//go:build agentintegration

package sandboxrun

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestSandboxNetworkHasNoDirectEgress runs a real container on the
// no-masquerade bridge and proves it cannot reach the internet directly while
// the host gateway stays reachable. Explicitly authorized only:
//
//	MULTICA_RUN_REAL_AGENT_SMOKE=1 go test -tags=agentintegration ./internal/daemon/sandboxrun -run TestSandboxNetworkHasNoDirectEgress -count=1 -v
func TestSandboxNetworkHasNoDirectEgress(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to allow a real docker run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not installed")
	}
	if out, err := exec.CommandContext(ctx, "docker", "network", "inspect", NetworkName).CombinedOutput(); err != nil {
		if out, err := exec.CommandContext(ctx, "docker", "network", "create", "-o", "com.docker.network.bridge.enable_ip_masquerade=false", NetworkName).CombinedOutput(); err != nil {
			t.Fatalf("create network: %v\n%s", err, out)
		}
		_ = out
	}
	script := `echo inside-sandbox; ` +
		`if wget -q -T 5 -O /dev/null http://1.1.1.1/ 2>/dev/null; then echo EGRESS=yes; else echo EGRESS=no; fi; ` +
		`getent hosts ` + AdvertiseHost + ` >/dev/null && echo GATEWAY=resolved || echo GATEWAY=unresolved`
	out, err := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", NetworkName,
		"--add-host", AdvertiseHost+":host-gateway", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"alpine", "sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v\n%s", err, out)
	}
	t.Logf("container output:\n%s", out)
	if !strings.Contains(string(out), "inside-sandbox") || !strings.Contains(string(out), "EGRESS=no") {
		t.Fatalf("container must run and have no direct egress:\n%s", out)
	}
	if !strings.Contains(string(out), "GATEWAY=resolved") {
		t.Fatalf("host gateway must resolve inside the container:\n%s", out)
	}
}
