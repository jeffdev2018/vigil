package agent

import (
	"context"
	"reflect"
	"testing"
)

// TestSandboxLaunchRewritesArgvToShim pins the K10 launch contract: a
// Config.Sandbox routes the process through `multica sandbox-run --spec … --
// <original argv>` while the Dir and Env a backend assigns afterwards survive.
func TestSandboxLaunchRewritesArgvToShim(t *testing.T) {
	t.Parallel()

	sb := &SandboxLaunch{Mode: "container", TaskID: "task-1", SpecPath: "/tmp/spec.json", ShimPath: "/opt/multica"}
	cfg := Config{ExecutablePath: "/usr/bin/claude", LaunchPrefix: []string{"start", "q36"}, Sandbox: sb}
	cmd := cfg.commandAt("/usr/bin/claude").exec(context.Background(), "-p", "hello")

	if cmd.Path != "/opt/multica" {
		t.Fatalf("argv0 = %q, want the shim", cmd.Path)
	}
	want := []string{"/opt/multica", "sandbox-run", "--spec", "/tmp/spec.json", "--", "/usr/bin/claude", "start", "q36", "-p", "hello"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %q, want %q", cmd.Args, want)
	}

	// Backends assign these after construction; the shim inherits both.
	cmd.Dir = "/work/dir"
	cmd.Env = []string{"MULTICA_TASK_ID=task-1"}
	if cmd.Dir != "/work/dir" || len(cmd.Env) != 1 {
		t.Fatal("Dir/Env assigned after construction must be preserved")
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("the shim process must still get the runtime process-lifecycle defaults")
	}
}

func TestSandboxLaunchNilLeavesArgvAlone(t *testing.T) {
	t.Parallel()
	cmd := Config{ExecutablePath: "/usr/bin/claude"}.commandAt("/usr/bin/claude").exec(context.Background(), "-p", "x")
	if !reflect.DeepEqual(cmd.Args, []string{"/usr/bin/claude", "-p", "x"}) {
		t.Fatalf("args = %q", cmd.Args)
	}
	if NewCommand("/bin/true", nil).sandbox != nil {
		t.Fatal("bare probes must never be sandboxed")
	}
}
