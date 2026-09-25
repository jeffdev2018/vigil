package daemon

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func lifecycleTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// fakeScript writes an executable the test owns. Default tests never resolve or
// execute a user-installed CLI; everything run here was created by the test.
func fakeScript(t *testing.T, name, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake script is a POSIX shell script")
	}
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatalf("write fake script: %v", err)
	}
	return path
}

func TestLifecycleScriptSucceedsAndLogs(t *testing.T) {
	script := fakeScript(t, "setup.sh", `echo "installed deps"; touch "$PWD/marker"`)
	workDir := t.TempDir()
	envRoot := t.TempDir()

	if err := runLifecycleScript(context.Background(), "setup", []string{script}, workDir, envRoot, nil, lifecycleTestLogger()); err != nil {
		t.Fatalf("runLifecycleScript: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, "marker")); err != nil {
		t.Fatalf("the script did not run in the worktree: %v", err)
	}
	log, err := os.ReadFile(filepath.Join(envRoot, "logs", "lifecycle-setup.log"))
	if err != nil {
		t.Fatalf("read the lifecycle log: %v", err)
	}
	if !strings.Contains(string(log), "installed deps") {
		t.Fatalf("the log does not carry the script's output: %q", log)
	}
}

// The failure message IS the run's failure reason, so it has to name the script
// and carry what it printed — "npm ci failed: no such package" is the answer.
func TestLifecycleScriptFailureCarriesItsOutput(t *testing.T) {
	script := fakeScript(t, "setup.sh", `echo "ENOENT: no such package"; exit 3`)

	err := runLifecycleScript(context.Background(), "setup", []string{script}, t.TempDir(), t.TempDir(), nil, lifecycleTestLogger())
	if err == nil {
		t.Fatal("expected a failing script to fail the run")
	}
	if !strings.Contains(err.Error(), "ENOENT: no such package") {
		t.Fatalf("the failure does not carry the script's output: %v", err)
	}
	if !strings.Contains(err.Error(), "setup") {
		t.Fatalf("the failure does not name which script failed: %v", err)
	}
}

// Nothing configured must not be an error: every caller would otherwise have to
// branch first.
func TestLifecycleScriptNoopWhenUnset(t *testing.T) {
	for _, argv := range [][]string{nil, {}, {"   "}} {
		if err := runLifecycleScript(context.Background(), "archive", argv, t.TempDir(), t.TempDir(), nil, lifecycleTestLogger()); err != nil {
			t.Fatalf("runLifecycleScript(%v) = %v, want nil", argv, err)
		}
	}
}

// argv, never a shell: a value containing shell metacharacters is one literal
// argument to the program, not a second command.
func TestLifecycleScriptDoesNotInvokeAShell(t *testing.T) {
	workDir := t.TempDir()
	script := fakeScript(t, "echo-args.sh", `printf '%s' "$1" > "$PWD/arg"`)
	injected := `; touch pwned`

	if err := runLifecycleScript(context.Background(), "setup", []string{script, injected}, workDir, t.TempDir(), nil, lifecycleTestLogger()); err != nil {
		t.Fatalf("runLifecycleScript: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(workDir, "arg"))
	if err != nil {
		t.Fatalf("read the captured argument: %v", err)
	}
	if string(got) != injected {
		t.Fatalf("argument = %q, want the literal %q", got, injected)
	}
	if _, err := os.Stat(filepath.Join(workDir, "pwned")); err == nil {
		t.Fatal("the argument was interpreted by a shell")
	}
}

// Only a worktree run gets lifecycle scripts. Running setup in an in-place
// directory would be a side effect on files the user did not hand over.
func TestLifecycleScriptsOnlyApplyToWorktreeMode(t *testing.T) {
	lc := &localDirectoryLifecycle{Setup: []string{"/bin/true"}, Archive: []string{"/bin/true"}}

	inPlace := &localDirectoryAssignment{Ref: localDirectoryRef{ExecutionMode: localDirectoryModeInPlace, Lifecycle: lc}}
	if got := inPlace.SetupScript(); got != nil {
		t.Fatalf("in_place SetupScript() = %v, want nil", got)
	}
	if got := inPlace.ArchiveScript(); got != nil {
		t.Fatalf("in_place ArchiveScript() = %v, want nil", got)
	}

	worktree := &localDirectoryAssignment{Ref: localDirectoryRef{ExecutionMode: localDirectoryModeWorktree, Lifecycle: lc}}
	if got := worktree.SetupScript(); len(got) != 1 {
		t.Fatalf("worktree SetupScript() = %v, want the configured argv", got)
	}
	if got := worktree.ArchiveScript(); len(got) != 1 {
		t.Fatalf("worktree ArchiveScript() = %v, want the configured argv", got)
	}
}

// A byte-offset tail cut can land mid multi-byte rune; the output must stay
// valid UTF-8 instead of emitting a mangled partial rune. "é" (2 bytes) sits
// at byte 0 and the rest is exactly lifecycleOutputLimit-1 ASCII bytes, so
// the kept tail (the last lifecycleOutputLimit bytes) starts one byte into
// "é" — its lone continuation byte, an invalid UTF-8 start.
func TestFormatLifecycleOutputCutsOnARuneBoundary(t *testing.T) {
	out := []byte("é" + strings.Repeat("a", lifecycleOutputLimit-1))
	got := formatLifecycleOutput(out)
	if !strings.HasPrefix(got, "\n\n…") {
		t.Fatalf("formatLifecycleOutput prefix = %q", got[:10])
	}
	body := strings.TrimPrefix(got, "\n\n…")
	if !utf8.ValidString(body) {
		t.Fatalf("formatLifecycleOutput produced invalid UTF-8: %q", body)
	}
	if !strings.HasSuffix(body, strings.Repeat("a", lifecycleOutputLimit-1)) {
		t.Fatalf("formatLifecycleOutput dropped content beyond the split rune")
	}
}
