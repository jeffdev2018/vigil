package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSandboxRunExecsPassthroughCommand drives the hidden shim end to end in
// mode none: the spec is honoured, `--` protects the wrapped argv, and the
// child inherits the shim's environment. The child is a test-created script,
// never a user-installed CLI.
func TestSandboxRunExecsPassthroughCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	// The shim execs, so the cobra command runs in a subprocess of this test
	// binary; the parent hands it every path through the environment.
	if os.Getenv("SANDBOX_RUN_CHILD") == "1" {
		rootCmd.SetArgs([]string{"sandbox-run", "--spec", os.Getenv("SPEC"), "--", os.Getenv("SCRIPT"), "-p"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatalf("execute: %v", err)
		}
		return
	}
	dir := t.TempDir()
	spec := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(spec, []byte(`{"mode":"none","task_id":"t","provider":"claude"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "fake-cli")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s|%s' \"$1\" \"$SANDBOX_RUN_MARK\" > \"$OUT\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	cmd := exec.Command(os.Args[0], "-test.run=^TestSandboxRunExecsPassthroughCommand$")
	cmd.Env = append(os.Environ(), "SANDBOX_RUN_CHILD=1", "SPEC="+spec, "SCRIPT="+script, "OUT="+out, "SANDBOX_RUN_MARK=task-42")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child failed: %v\n%s", err, b)
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "-p|task-42" {
		t.Fatalf("child saw %q", got)
	}
}
