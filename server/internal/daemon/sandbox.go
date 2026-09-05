package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon/sandboxrun"
	"github.com/multica-ai/multica/server/internal/selfexec"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// K10 sandbox mode per daemon runtime.
//
// The server asks for a confinement mode per run; this file decides what the
// machine can honour, reports it, and prepares the launch: a spec file the
// `multica sandbox-run` shim reads, a proxy credential for container egress,
// and a seeded home so the provider CLI finds its credentials inside the
// container. The wrapping itself happens in pkg/agent's launch boundary.

const (
	sandboxProbeTimeout    = 5 * time.Second
	sandboxProbeInterval   = 10 * time.Minute
	sandboxDockerRmTimeout = 30 * time.Second
)

// resolveSandboxMode degrades the requested mode to what the machine can run.
// It never picks silently: reason is non-empty whenever mode != requested.
func resolveSandboxMode(spec *SandboxSpec, caps SandboxCapabilities) (mode, reason string) {
	requested := sandboxrun.ModeNone
	if spec != nil && spec.Mode != "" {
		requested = spec.Mode
	}
	switch requested {
	case sandboxrun.ModeNone:
		return sandboxrun.ModeNone, ""
	case sandboxrun.ModeContainer:
		if caps.Docker {
			return sandboxrun.ModeContainer, ""
		}
		if caps.Bwrap {
			return sandboxrun.ModeSandbox, "docker is not available on this machine; using bubblewrap sandbox"
		}
		return sandboxrun.ModeNone, "docker is not available on this machine"
	case sandboxrun.ModeSandbox:
		if caps.Bwrap {
			return sandboxrun.ModeSandbox, ""
		}
		if caps.OS != "linux" {
			return sandboxrun.ModeNone, "bubblewrap sandbox requires a Linux host (this machine runs " + caps.OS + ")"
		}
		return sandboxrun.ModeNone, "bwrap is not installed on this machine"
	default:
		return sandboxrun.ModeNone, fmt.Sprintf("unknown sandbox mode %q", requested)
	}
}

// sandboxAdvertiseHost is the host the run's local listeners advertise: a
// container reaches the daemon through the Docker host gateway, everything
// else through loopback.
func sandboxAdvertiseHost(mode string) string {
	if mode == sandboxrun.ModeContainer {
		return sandboxrun.AdvertiseHost
	}
	return ""
}

// listenForRun binds a per-run listener. With an advertise host it listens
// on all interfaces (the container cannot reach loopback) and returns the
// host:port to put in the CLI's config; otherwise loopback, as before.
func listenForRun(advertiseHost string) (net.Listener, string, error) {
	addr := "127.0.0.1:0"
	if advertiseHost != "" {
		addr = "0.0.0.0:0"
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, "", err
	}
	advertised := listener.Addr().String()
	if advertiseHost != "" {
		advertised = net.JoinHostPort(advertiseHost, strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	}
	return listener, advertised, nil
}

// sandboxCommandRunner runs a probe command and returns its stdout. Tests
// inject a fake; production uses execSandboxCommand with the real binaries.
type sandboxCommandRunner func(ctx context.Context, name string, args ...string) ([]byte, error)

func execSandboxCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, sandboxProbeTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

// probeSandboxCapabilities asks the machine what it can confine with.
func probeSandboxCapabilities(ctx context.Context, goos string, lookPath func(string) (string, error), run sandboxCommandRunner) SandboxCapabilities {
	caps := SandboxCapabilities{OS: goos, Modes: []string{sandboxrun.ModeNone}}
	if _, err := lookPath("docker"); err == nil {
		if out, err := run(ctx, "docker", "version", "--format", "{{.Server.Version}}"); err == nil {
			if v := strings.TrimSpace(string(out)); v != "" {
				caps.Docker, caps.DockerVersion = true, v
				caps.Modes = append(caps.Modes, sandboxrun.ModeContainer)
			}
		}
	}
	if goos == "linux" {
		if _, err := lookPath("bwrap"); err == nil {
			caps.Bwrap = true
			caps.Modes = append(caps.Modes, sandboxrun.ModeSandbox)
		}
	}
	return caps
}

// ensureSandboxNetwork creates the no-masquerade bridge once (inspect first).
func ensureSandboxNetwork(ctx context.Context, run sandboxCommandRunner) error {
	if _, err := run(ctx, "docker", "network", "inspect", sandboxrun.NetworkName); err == nil {
		return nil
	}
	_, err := run(ctx, "docker", "network", "create", "-o", "com.docker.network.bridge.enable_ip_masquerade=false", sandboxrun.NetworkName)
	return err
}

// sandboxCapabilities is the last probe result; a daemon that never probed
// (tests) reports host-only.
func (d *Daemon) sandboxCapabilities() SandboxCapabilities {
	if caps := d.sandboxCaps.Load(); caps != nil {
		return *caps
	}
	return SandboxCapabilities{OS: runtime.GOOS, Modes: []string{sandboxrun.ModeNone}}
}

// refreshSandboxCapabilities probes with the real binaries. Container mode is
// only advertised once the sandbox network exists: without it every
// `docker run` would fail, so the capability fails closed instead.
func (d *Daemon) refreshSandboxCapabilities(ctx context.Context) {
	caps := probeSandboxCapabilities(ctx, runtime.GOOS, exec.LookPath, execSandboxCommand)
	if caps.Docker {
		if err := ensureSandboxNetwork(ctx, execSandboxCommand); err != nil {
			d.logger.Warn("sandbox: docker network unavailable; container mode disabled", "network", sandboxrun.NetworkName, "error", err)
			caps.Docker, caps.DockerVersion = false, ""
			caps.Modes = withoutString(caps.Modes, sandboxrun.ModeContainer)
		}
	}
	prev := d.sandboxCaps.Swap(&caps)
	if prev == nil || sandboxCapabilitiesFingerprint(*prev) != sandboxCapabilitiesFingerprint(caps) {
		d.logger.Info("sandbox capabilities", "os", caps.OS, "docker", caps.Docker, "docker_version", caps.DockerVersion, "bwrap", caps.Bwrap, "modes", caps.Modes)
	}
}

func withoutString(list []string, drop string) []string {
	out := list[:0]
	for _, s := range list {
		if s != drop {
			out = append(out, s)
		}
	}
	return out
}

// startSandboxSupport runs the first probe and the egress proxy, then keeps
// the probe fresh every sandboxProbeInterval.
func (d *Daemon) startSandboxSupport(ctx context.Context) {
	d.refreshSandboxCapabilities(ctx)
	if d.sandboxCapabilities().Docker {
		proxy, err := sandboxrun.StartProxy("0.0.0.0:0", d.logger)
		if err != nil {
			d.logger.Warn("sandbox: egress proxy failed to start; container mode disabled", "error", err)
			caps := d.sandboxCapabilities()
			caps.Docker, caps.DockerVersion = false, ""
			caps.Modes = withoutString(caps.Modes, sandboxrun.ModeContainer)
			d.sandboxCaps.Store(&caps)
		} else {
			d.sandboxProxy = proxy
			d.logger.Info("sandbox: egress proxy listening", "port", proxy.Port())
		}
	}
	go func() {
		ticker := time.NewTicker(sandboxProbeInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				if d.sandboxProxy != nil {
					_ = d.sandboxProxy.Close()
				}
				return
			case <-ticker.C:
				d.refreshSandboxCapabilities(ctx)
			}
		}
	}()
}

func sandboxCapabilitiesFingerprint(caps SandboxCapabilities) string {
	raw, _ := json.Marshal(caps)
	return string(raw)
}

// pendingSandboxCapabilities mirrors pendingSkippedAgents: the set to send on
// this beat, or nil when the server already holds it.
func (d *Daemon) pendingSandboxCapabilities(runtimeID string) (*SandboxCapabilities, string, bool) {
	caps := d.sandboxCapabilities()
	fingerprint := sandboxCapabilitiesFingerprint(caps)
	d.sandboxCapsSentMu.Lock()
	sent, known := d.sandboxCapsSent[runtimeID]
	d.sandboxCapsSentMu.Unlock()
	if known && sent == fingerprint {
		return nil, fingerprint, false
	}
	return &caps, fingerprint, true
}

func (d *Daemon) markSandboxCapabilitiesSent(runtimeID, fingerprint string) {
	d.sandboxCapsSentMu.Lock()
	defer d.sandboxCapsSentMu.Unlock()
	if d.sandboxCapsSent == nil {
		d.sandboxCapsSent = make(map[string]string)
	}
	d.sandboxCapsSent[runtimeID] = fingerprint
}

// prepareSandboxLaunch writes the shim spec for one run and returns the
// launch config plus a cleanup that revokes the proxy credential and removes
// a container the shim's process-group kill may have orphaned (killing the
// docker client does not stop the container). mounts are host paths the CLI
// must see read-write besides the working directory.
func (d *Daemon) prepareSandboxLaunch(task Task, provider, mode, taskTempDir string, mounts []string, log *slog.Logger) (*agent.SandboxLaunch, func(), error) {
	noop := func() {}
	if mode == "" || mode == sandboxrun.ModeNone {
		return nil, noop, nil
	}
	shim, err := selfexec.Resolve()
	if err != nil {
		return nil, noop, fmt.Errorf("resolve sandbox shim: %w", err)
	}
	spec := sandboxrun.Spec{Mode: mode, TaskID: task.ID, Provider: provider, Mounts: mounts}
	if task.Sandbox != nil {
		spec.Image, spec.AllowedHosts = task.Sandbox.Image, task.Sandbox.AllowedHosts
	}
	cleanup := noop
	switch mode {
	case sandboxrun.ModeSandbox:
		// bwrap keeps the real HOME read-only; the provider's state dir must
		// stay writable for sessions and credential refreshes.
		if dir := providerHostConfigDir(provider); dir != "" {
			spec.Mounts = append(spec.Mounts, dir)
		}
	case sandboxrun.ModeContainer:
		if d.sandboxProxy == nil {
			return nil, noop, errors.New("container mode requested but the egress proxy is not running")
		}
		home, err := seedSandboxHome(provider)
		if err != nil {
			return nil, noop, fmt.Errorf("seed sandbox home: %w", err)
		}
		spec.Home = home
		spec.AdvertiseHost = sandboxrun.AdvertiseHost
		if runtime.GOOS == "linux" {
			// Same OS as the image: the daemon's own binary serves as the
			// container's `multica` CLI. Elsewhere the image must ship it.
			spec.CLIPath = shim
		}
		suffix := make([]byte, 3)
		if _, err := rand.Read(suffix); err != nil {
			return nil, noop, err
		}
		spec.ContainerName = "multica-run-" + strings.ReplaceAll(task.ID, "-", "")[:min(8, len(task.ID))] + "-" + hex.EncodeToString(suffix)
		allowed := append([]string(nil), sandboxrun.DefaultAllowedHosts...)
		if u, err := url.Parse(d.cfg.ServerBaseURL); err == nil && u.Hostname() != "" {
			allowed = append(allowed, u.Hostname())
		}
		allowed = append(allowed, spec.AllowedHosts...)
		proxyURL, token, err := d.sandboxProxy.Register(allowed, sandboxrun.AdvertiseHost)
		if err != nil {
			return nil, noop, err
		}
		spec.ProxyURL = proxyURL
		name := spec.ContainerName
		cleanup = func() {
			d.sandboxProxy.Unregister(token)
			rmCtx, cancel := context.WithTimeout(context.Background(), sandboxDockerRmTimeout)
			defer cancel()
			if out, err := exec.CommandContext(rmCtx, "docker", "rm", "-f", name).CombinedOutput(); err != nil && !strings.Contains(string(out), "No such container") {
				log.Warn("sandbox: container cleanup failed", "container", name, "error", err, "output", strings.TrimSpace(string(out)))
			}
		}
	}
	specPath := filepath.Join(taskTempDir, "sandbox-spec.json")
	raw, err := json.Marshal(spec)
	if err != nil {
		cleanup()
		return nil, noop, err
	}
	// The spec carries the proxy credential: owner-only.
	if err := os.WriteFile(specPath, raw, 0o600); err != nil {
		cleanup()
		return nil, noop, fmt.Errorf("write sandbox spec: %w", err)
	}
	return &agent.SandboxLaunch{Mode: mode, Image: spec.Image, AllowedHosts: spec.AllowedHosts, TaskID: task.ID, SpecPath: specPath, ShimPath: shim}, cleanup, nil
}

// providerHostConfigDir is where the provider CLI keeps its state and
// credentials on the host. Empty for providers this code does not know.
func providerHostConfigDir(provider string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch provider {
	case "claude":
		return filepath.Join(home, ".claude")
	case "codex":
		return filepath.Join(home, ".codex")
	}
	return ""
}

// sandboxHomeSkip lists per-provider subtrees left out of the seeded home:
// they are large and machine-specific, not credentials.
var sandboxHomeSkip = map[string][]string{"claude": {"projects"}, "codex": {"sessions"}}

// seedSandboxHome returns ~/.multica/sandbox/home/<provider>, creating it on
// first use by copying the provider's host config dir (and ~/.claude.json,
// which holds Claude Code's OAuth state) so the CLI in the container is
// already signed in. The copy is one-shot: a credential rotated on the host
// afterwards is not propagated; delete the directory to reseed.
func seedSandboxHome(provider string) (string, error) {
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	home := filepath.Join(userHome, ".multica", "sandbox", "home", provider)
	if _, err := os.Stat(home); err == nil {
		return home, nil
	}
	staging := home + ".seeding"
	_ = os.RemoveAll(staging)
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return "", err
	}
	if src := providerHostConfigDir(provider); src != "" {
		if err := copyTreeSkipping(src, filepath.Join(staging, filepath.Base(src)), sandboxHomeSkip[provider]); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if provider == "claude" {
		if err := copyFileMode(filepath.Join(userHome, ".claude.json"), filepath.Join(staging, ".claude.json")); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
	}
	if err := os.Rename(staging, home); err != nil {
		return "", err
	}
	return home, nil
}

func copyTreeSkipping(src, dst string, skip []string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		for _, s := range skip {
			if rel == s {
				return filepath.SkipDir
			}
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		return copyFileMode(path, target)
	})
}

func copyFileMode(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	info, err := in.Stat()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// sandboxCapsState is embedded in Daemon: the last probe and, per runtime,
// the fingerprint the server accepted.
type sandboxCapsState struct {
	sandboxCaps       atomic.Pointer[SandboxCapabilities]
	sandboxCapsSentMu sync.Mutex
	sandboxCapsSent   map[string]string
	sandboxProxy      *sandboxrun.Proxy
}
