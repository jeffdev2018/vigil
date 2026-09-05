package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon/sandboxrun"
)

func TestResolveSandboxModeMatrix(t *testing.T) {
	t.Parallel()
	docker := SandboxCapabilities{OS: "linux", Docker: true}
	bwrap := SandboxCapabilities{OS: "linux", Bwrap: true}
	both := SandboxCapabilities{OS: "linux", Docker: true, Bwrap: true}
	bare := SandboxCapabilities{OS: "darwin"}
	cases := []struct {
		name       string
		spec       *SandboxSpec
		caps       SandboxCapabilities
		wantMode   string
		wantReason bool
	}{
		{"nil spec", nil, bare, "none", false},
		{"none", &SandboxSpec{Mode: "none"}, both, "none", false},
		{"container with docker", &SandboxSpec{Mode: "container"}, docker, "container", false},
		{"container degrades to bwrap", &SandboxSpec{Mode: "container"}, bwrap, "sandbox", true},
		{"container degrades to none", &SandboxSpec{Mode: "container"}, bare, "none", true},
		{"sandbox with bwrap", &SandboxSpec{Mode: "sandbox"}, bwrap, "sandbox", false},
		{"sandbox without bwrap on linux", &SandboxSpec{Mode: "sandbox"}, SandboxCapabilities{OS: "linux"}, "none", true},
		{"sandbox on macos", &SandboxSpec{Mode: "sandbox"}, bare, "none", true},
		{"unknown", &SandboxSpec{Mode: "vm"}, both, "none", true},
	}
	for _, tc := range cases {
		mode, reason := resolveSandboxMode(tc.spec, tc.caps)
		if mode != tc.wantMode || (reason != "") != tc.wantReason {
			t.Errorf("%s: got (%q, %q), want mode %q reason=%v", tc.name, mode, reason, tc.wantMode, tc.wantReason)
		}
	}
	if _, reason := resolveSandboxMode(&SandboxSpec{Mode: "container"}, bare); reason != "docker is not available on this machine" {
		t.Fatalf("reason = %q", reason)
	}
}

// fakeToolDir writes fake docker/bwrap executables and returns a lookPath and
// a runner bound to that directory only, so no real binary is ever probed.
func fakeToolDir(t *testing.T, tools map[string]string) (func(string) (string, error), sandboxCommandRunner) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range tools {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	lookPath := func(name string) (string, error) {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			return "", exec.ErrNotFound
		}
		return p, nil
	}
	run := func(ctx context.Context, name string, args ...string) ([]byte, error) {
		p, err := lookPath(name)
		if err != nil {
			return nil, err
		}
		return exec.CommandContext(ctx, p, args...).Output()
	}
	return lookPath, run
}

func TestProbeSandboxCapabilities(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixtures")
	}
	t.Parallel()
	ctx := context.Background()

	lookPath, run := fakeToolDir(t, map[string]string{"docker": "echo 27.1.1", "bwrap": "exit 0"})
	caps := probeSandboxCapabilities(ctx, "linux", lookPath, run)
	if !caps.Docker || caps.DockerVersion != "27.1.1" || !caps.Bwrap || strings.Join(caps.Modes, ",") != "none,container,sandbox" {
		t.Fatalf("linux caps = %+v", caps)
	}
	caps = probeSandboxCapabilities(ctx, "darwin", lookPath, run)
	if caps.Bwrap || strings.Join(caps.Modes, ",") != "none,container" {
		t.Fatalf("darwin must never advertise bwrap: %+v", caps)
	}

	lookPath, run = fakeToolDir(t, map[string]string{"docker": "echo 'Cannot connect to the Docker daemon' >&2; exit 1"})
	caps = probeSandboxCapabilities(ctx, "linux", lookPath, run)
	if caps.Docker || caps.DockerVersion != "" || strings.Join(caps.Modes, ",") != "none" {
		t.Fatalf("a docker CLI without a daemon must not count: %+v", caps)
	}

	lookPath, run = fakeToolDir(t, nil)
	if caps = probeSandboxCapabilities(ctx, "linux", lookPath, run); caps.Docker || caps.Bwrap || len(caps.Modes) != 1 {
		t.Fatalf("missing binaries: %+v", caps)
	}
}

func TestEnsureSandboxNetworkIsIdempotent(t *testing.T) {
	t.Parallel()
	var calls []string
	exists := false
	run := func(_ context.Context, name string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(append([]string{name}, args...), " "))
		if args[1] == "inspect" && !exists {
			return nil, errors.New("no such network")
		}
		exists = true
		return nil, nil
	}
	for i := 0; i < 2; i++ {
		if err := ensureSandboxNetwork(context.Background(), run); err != nil {
			t.Fatal(err)
		}
	}
	want := []string{
		"docker network inspect multica-sandbox",
		"docker network create -o com.docker.network.bridge.enable_ip_masquerade=false multica-sandbox",
		"docker network inspect multica-sandbox",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(calls, "\n"))
	}
}

func TestHeartbeatCarriesSandboxCapabilitiesOnlyWhenChanged(t *testing.T) {
	t.Parallel()
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		_, _ = io.WriteString(w, `{}`)
	}))
	defer srv.Close()

	d := &Daemon{client: NewClient(srv.URL), logger: slog.New(slog.DiscardHandler)}
	caps := SandboxCapabilities{OS: "linux", Docker: true, DockerVersion: "27.1.1", Modes: []string{"none", "container"}}
	d.sandboxCaps.Store(&caps)

	beat := func() {
		pending, fp, changed := d.pendingSandboxCapabilities("rt-1")
		if _, err := d.client.SendHeartbeat(context.Background(), "rt-1", nil, nil, pending); err != nil {
			t.Fatal(err)
		}
		if changed {
			d.markSandboxCapabilitiesSent("rt-1", fp)
		}
	}
	beat()
	beat()
	got, ok := bodies[0]["sandbox_capabilities"].(map[string]any)
	if !ok || got["os"] != "linux" || got["docker"] != true || got["docker_version"] != "27.1.1" || got["bwrap"] != false {
		t.Fatalf("first beat body = %v", bodies[0])
	}
	if modes, _ := got["modes"].([]any); len(modes) != 2 || modes[1] != "container" {
		t.Fatalf("modes = %v", got["modes"])
	}
	if _, present := bodies[1]["sandbox_capabilities"]; present {
		t.Fatalf("unchanged capabilities must not be re-sent: %v", bodies[1])
	}
	caps2 := caps
	caps2.Docker = false
	d.sandboxCaps.Store(&caps2)
	beat()
	if got := bodies[2]["sandbox_capabilities"].(map[string]any); got["docker"] != false {
		t.Fatalf("changed capabilities must be re-sent: %v", bodies[2])
	}
}

func TestStartTaskReportsSandboxDecision(t *testing.T) {
	t.Parallel()
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/tasks/t-1/start" {
			t.Errorf("path %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
	}))
	defer srv.Close()
	if err := NewClient(srv.URL).StartTask(context.Background(), "t-1", "container", "sandbox", "docker is not available on this machine; using bubblewrap sandbox"); err != nil {
		t.Fatal(err)
	}
	if body["sandbox_requested"] != "container" || body["sandbox_mode"] != "sandbox" || body["sandbox_reason"] != "docker is not available on this machine; using bubblewrap sandbox" {
		t.Fatalf("body = %v", body)
	}
	if err := NewClient(srv.URL).StartTask(context.Background(), "t-1", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if body["sandbox_requested"] != "none" || body["sandbox_mode"] != "none" || body["sandbox_reason"] != "" {
		t.Fatalf("empty decision must read as none: %v", body)
	}
}

func TestListenForRunAdvertisesHostGateway(t *testing.T) {
	t.Parallel()
	ln, advertised, err := listenForRun("")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if !strings.HasPrefix(advertised, "127.0.0.1:") || !strings.HasPrefix(ln.Addr().String(), "127.0.0.1:") {
		t.Fatalf("default must stay on loopback: %s / %s", advertised, ln.Addr())
	}
	ln2, advertised2, err := listenForRun("host.docker.internal")
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()
	port := strings.TrimPrefix(ln2.Addr().String(), "[::]:")
	if advertised2 != "host.docker.internal:"+port || strings.HasPrefix(ln2.Addr().String(), "127.0.0.1:") {
		t.Fatalf("container runs must listen on all interfaces and advertise the gateway: %s / %s", advertised2, ln2.Addr())
	}
}

func TestPrepareSandboxLaunchWritesSpecAndSeedsHome(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix home layout")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Host credentials the seeded home must carry, and the subtree it must not.
	for _, f := range []string{".claude/settings.json", ".claude/projects/p/x.jsonl", ".claude.json"} {
		p := filepath.Join(home, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(f), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	proxy, err := sandboxrun.StartProxy("127.0.0.1:0", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer proxy.Close()
	d := &Daemon{cfg: Config{ServerBaseURL: "http://127.0.0.1:8080", WorkspacesRoot: filepath.Join(home, "ws")}, logger: slog.New(slog.DiscardHandler)}
	d.sandboxProxy = proxy
	tempDir := t.TempDir()
	task := Task{ID: "0123456789abcdef-task", Sandbox: &SandboxSpec{Mode: "container", Image: "", AllowedHosts: []string{"pypi.org"}}}

	launch, cleanup, err := d.prepareSandboxLaunch(task, "claude", "container", tempDir, []string{"/tmp/env-root"}, d.logger)
	if err != nil {
		t.Fatal(err)
	}
	if launch.Mode != "container" || launch.ShimPath == "" || launch.TaskID != task.ID {
		t.Fatalf("launch = %+v", launch)
	}
	raw, err := os.ReadFile(launch.SpecPath)
	if err != nil {
		t.Fatal(err)
	}
	var spec sandboxrun.Spec
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Mode != "container" || spec.Provider != "claude" || spec.TaskID != task.ID || spec.AdvertiseHost != "host.docker.internal" ||
		!strings.HasPrefix(spec.ContainerName, "multica-run-01234567-") || !strings.HasPrefix(spec.ProxyURL, "http://multica:") ||
		!strings.Contains(spec.ProxyURL, "@host.docker.internal:") || spec.Home != filepath.Join(home, ".multica", "sandbox", "home", "claude") ||
		strings.Join(spec.Mounts, ",") != "/tmp/env-root" || strings.Join(spec.AllowedHosts, ",") != "pypi.org" {
		t.Fatalf("spec = %s", raw)
	}
	if (spec.CLIPath != "") != (runtime.GOOS == "linux") {
		t.Fatalf("cli_path is Linux-only: %q", spec.CLIPath)
	}
	if _, err := os.Stat(filepath.Join(spec.Home, ".claude", "settings.json")); err != nil {
		t.Fatalf("seeded home lacks credentials: %v", err)
	}
	if _, err := os.Stat(filepath.Join(spec.Home, ".claude.json")); err != nil {
		t.Fatalf("seeded home lacks ~/.claude.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(spec.Home, ".claude", "projects")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("projects/ must not be copied into the sandbox home")
	}
	info, _ := os.Stat(launch.SpecPath)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("spec holds the proxy credential and must be owner-only, got %v", info.Mode().Perm())
	}
	// Cleanup revokes the credential (docker rm -f is best-effort and logged).
	t.Setenv("PATH", t.TempDir())
	cleanup()
	pu := spec.ProxyURL
	token := pu[strings.Index(pu, "multica:")+len("multica:") : strings.Index(pu, "@")]
	req, _ := http.NewRequest(http.MethodConnect, "http://x", nil)
	req.Host = "pypi.org:443"
	req.SetBasicAuth("multica", token)
	req.Header.Set("Proxy-Authorization", req.Header.Get("Authorization"))
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)
	if rec.Code != http.StatusProxyAuthRequired {
		t.Fatalf("revoked credential must be rejected, got %d", rec.Code)
	}

	// Sandbox mode: no proxy, the provider state dir is added to the mounts.
	launch, cleanup, err = d.prepareSandboxLaunch(task, "claude", "sandbox", tempDir, nil, d.logger)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	raw, _ = os.ReadFile(launch.SpecPath)
	spec = sandboxrun.Spec{}
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Mode != "sandbox" || spec.ProxyURL != "" || strings.Join(spec.Mounts, ",") != filepath.Join(home, ".claude") {
		t.Fatalf("sandbox spec = %s", raw)
	}
	if l, _, err := d.prepareSandboxLaunch(task, "claude", "none", tempDir, nil, d.logger); err != nil || l != nil {
		t.Fatalf("mode none must not produce a launch: %v %v", l, err)
	}
}
