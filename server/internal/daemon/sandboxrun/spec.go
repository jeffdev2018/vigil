// Package sandboxrun implements the K10 confinement shim: `multica sandbox-run
// --spec <file> -- <cmd> <args…>` wraps the runtime CLI in `docker run`
// (mode container) or `bwrap` (mode sandbox) and execs it. The argv builders
// are pure so the daemon package can test them without Docker or bubblewrap.
package sandboxrun

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

const (
	ModeNone      = "none"
	ModeSandbox   = "sandbox"
	ModeContainer = "container"

	// DefaultImage is used when the run's spec carries no image. Only the
	// providers in installSnippets can be bootstrapped into it.
	DefaultImage = "node:22-bookworm-slim"
	// NetworkName is the Docker bridge the daemon creates without IP
	// masquerade: a container on it reaches the host (the egress proxy and
	// MCP listeners) but has no route out.
	NetworkName = "multica-sandbox"
	// AdvertiseHost is how a container reaches the host.
	AdvertiseHost = "host.docker.internal"
	// ContainerHome is the sandbox home mounted into the container. The
	// daemon seeds it from the provider's host config dir so credentials work.
	ContainerHome = "/root"
	// ContainerCLIPath is where a Linux daemon mounts its own multica binary
	// so the agent's `multica …` commands keep working inside the container.
	ContainerCLIPath = "/usr/local/bin/multica"
)

// installSnippets bootstrap a provider CLI into DefaultImage when missing.
// Providers absent here need a custom image that already ships their CLI.
var installSnippets = map[string]string{
	"claude": "command -v claude >/dev/null || npm i -g @anthropic-ai/claude-code >/dev/null",
	"codex":  "command -v codex >/dev/null || npm i -g @openai/codex >/dev/null",
}

// containerEnvPrefixes selects which inherited variables enter the container:
// the daemon's MULTICA_* contract plus provider credentials/config.
var containerEnvPrefixes = []string{"MULTICA_", "ANTHROPIC_", "CLAUDE_", "OPENAI_", "CODEX_", "TMPDIR=", "TMP=", "TEMP="}

// Spec is the JSON file the daemon writes for one run and the shim reads.
type Spec struct {
	Mode          string   `json:"mode"`
	Image         string   `json:"image,omitempty"`
	AllowedHosts  []string `json:"allowed_hosts,omitempty"`
	TaskID        string   `json:"task_id"`
	Provider      string   `json:"provider"`
	ContainerName string   `json:"container_name,omitempty"`
	// ProxyURL carries the egress proxy credentials: http://multica:<token>@host:port.
	ProxyURL      string `json:"proxy_url,omitempty"`
	AdvertiseHost string `json:"advertise_host,omitempty"`
	// Home is the host directory mounted as the container's home.
	Home string `json:"home,omitempty"`
	// CLIPath is a host multica binary to expose at ContainerCLIPath (Linux only).
	CLIPath string `json:"cli_path,omitempty"`
	// Mounts are host paths bind-mounted read-write at the same path: the
	// task temp dir, the task config root, provider state dirs. The working
	// directory is not listed; the shim reads it from its own cwd.
	Mounts []string `json:"mounts,omitempty"`
}

// Argv builds the process to exec for spec: argv0 (a PATH name for docker and
// bwrap), its arguments and the environment. Mode none passes through.
func Argv(spec Spec, cwd string, env []string, path string, args []string) (string, []string, []string, error) {
	switch spec.Mode {
	case "", ModeNone:
		return path, args, env, nil
	case ModeSandbox:
		argv := []string{"--ro-bind", "/", "/", "--bind", cwd, cwd}
		for _, m := range uniquePaths(append(mountsFromEnv(env), spec.Mounts...)) {
			argv = append(argv, "--bind", m, m)
		}
		argv = append(argv, "--dev", "/dev", "--proc", "/proc", "--tmpfs", "/run", "--unshare-pid", "--die-with-parent", "--", path)
		argv = append(argv, args...)
		return "bwrap", argv, env, nil
	case ModeContainer:
		argv, err := containerArgv(spec, cwd, env, path, args)
		return "docker", argv, env, err
	default:
		return "", nil, nil, fmt.Errorf("sandbox-run: unknown mode %q", spec.Mode)
	}
}

func containerArgv(spec Spec, cwd string, env []string, path string, args []string) ([]string, error) {
	image := spec.Image
	if image == "" {
		image = DefaultImage
	}
	install, known := installSnippets[spec.Provider]
	if !known && image == DefaultImage {
		return nil, fmt.Errorf("sandbox-run: provider %q needs a custom image that ships its CLI; the default image only bootstraps claude and codex", spec.Provider)
	}
	if known && image != DefaultImage {
		install = "" // a custom image is expected to ship the CLI
	}
	host := spec.AdvertiseHost
	if host == "" {
		host = AdvertiseHost
	}
	name := spec.ContainerName
	if name == "" {
		name = "multica-run-" + shortID(spec.TaskID)
	}
	argv := []string{"run", "--rm", "-i", "--init", "--name", name,
		"--network", NetworkName, "--add-host", host + ":host-gateway",
		"--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--pids-limit", "1024",
		"-w", cwd, "-v", cwd + ":" + cwd}
	for _, m := range uniquePaths(append(mountsFromEnv(env), spec.Mounts...)) {
		if m == cwd {
			continue
		}
		argv = append(argv, "-v", m+":"+m)
	}
	if spec.Home != "" {
		argv = append(argv, "-v", spec.Home+":"+ContainerHome)
	}
	if spec.CLIPath != "" {
		argv = append(argv, "-v", spec.CLIPath+":"+ContainerCLIPath+":ro")
	}
	argv = append(argv, "-e", "HOME="+ContainerHome, "-e", "IS_SANDBOX=1")
	if spec.ProxyURL != "" {
		argv = append(argv, "-e", "HTTPS_PROXY="+spec.ProxyURL, "-e", "HTTP_PROXY="+spec.ProxyURL,
			"-e", "NO_PROXY="+host+",localhost,127.0.0.1")
	}
	for _, kv := range ContainerEnv(env, host) {
		argv = append(argv, "-e", kv)
	}
	script := "exec " + shellQuote(filepath.Base(path)) + ` "$@"`
	if install != "" {
		script = install + "; " + script
	}
	argv = append(argv, image, "sh", "-c", script, "sh")
	argv = append(argv, args...)
	return argv, nil
}

// ContainerEnv filters the inherited environment down to what the container
// receives. A loopback MULTICA_SERVER_URL (local development) is re-pointed at
// the advertise host, since 127.0.0.1 inside the container is the container.
func ContainerEnv(env []string, advertiseHost string) []string {
	var out []string
	for _, kv := range env {
		keep := false
		for _, p := range containerEnvPrefixes {
			if strings.HasPrefix(kv, p) {
				keep = true
				break
			}
		}
		if !keep {
			continue
		}
		if strings.HasPrefix(kv, "MULTICA_SERVER_URL=") {
			kv = "MULTICA_SERVER_URL=" + rewriteLoopback(strings.TrimPrefix(kv, "MULTICA_SERVER_URL="), advertiseHost)
		}
		out = append(out, kv)
	}
	return out
}

func rewriteLoopback(raw, host string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		if port := u.Port(); port != "" {
			u.Host = host + ":" + port
		} else {
			u.Host = host
		}
		return u.String()
	}
	return raw
}

// mountsFromEnv returns the temp dir and task config root the daemon set in
// the inherited environment, so both the shim and the CLI see the same paths.
func mountsFromEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		for _, key := range []string{"TMPDIR=", "MULTICA_TASK_CONFIG_ROOT="} {
			if v := strings.TrimPrefix(kv, key); v != kv && v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

func uniquePaths(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		p = filepath.Clean(p)
		if p == "" || p == "." || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

func shortID(id string) string {
	id = strings.ReplaceAll(id, "-", "")
	if len(id) > 8 {
		id = id[:8]
	}
	return id
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
