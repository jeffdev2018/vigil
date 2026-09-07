package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The hook is the only place a command allowlist can be enforced: it receives
// the final command string, so chaining and wrapping are visible to it. The
// matrix of what that string may contain is canonical in
// pkg/permissionprofile/shellcommand_test.go; this covers the event contract.
func TestPreToolUseDecision(t *testing.T) {
	allow := []string{"git status", "make test"}
	event := func(tool, command string) []byte {
		return []byte(`{"tool_name":"` + tool + `","tool_input":{"command":"` + command + `"}}`)
	}

	if block, _ := preToolUseDecision(event("Bash", "rm -rf /"), nil, "narrow"); block {
		t.Error("a run with no declared allowlist is not command-restricted and must not be blocked")
	}
	if block, _ := preToolUseDecision(event("Bash", "git status"), allow, "narrow"); block {
		t.Error("an allowed command must run")
	}
	if block, _ := preToolUseDecision(event("Edit", "rm -rf /"), allow, "narrow"); block {
		t.Error("the allowlist governs shell commands; another tool is not its business")
	}

	block, reason := preToolUseDecision(event("Bash", "git status \\u0026\\u0026 rm -rf /"), allow, "narrow")
	if !block {
		t.Fatal("a chained command must be blocked on its second segment")
	}
	// The model has to be able to act on the refusal, so it names both the
	// offending segment and what it could have run instead.
	if !strings.Contains(reason, "rm") || !strings.Contains(reason, "git status, make test") {
		t.Errorf("reason = %q, want the refused segment and the allowed list", reason)
	}
	if !strings.Contains(reason, "narrow") {
		t.Errorf("reason = %q, want the profile named so the operator knows what to change", reason)
	}

	// An event it cannot read is an event it cannot judge.
	if block, _ := preToolUseDecision([]byte("not json"), allow, "narrow"); !block {
		t.Error("an unreadable tool call must be refused, never waved through")
	}
	if block, _ := preToolUseDecision(nil, allow, "narrow"); !block {
		t.Error("an empty tool call must be refused too")
	}
	// …but only when there is a policy to apply. With no allowlist there is
	// nothing to fail closed about, and blocking every run of every unrelated
	// workspace would be the wrong kind of safe.
	if block, _ := preToolUseDecision([]byte("not json"), nil, ""); block {
		t.Error("no allowlist, no decision to make")
	}
}

// The marker is how the daemon tells "this run ran no shell command" from "the
// hook never fired". Without it, a CLI whose hooks are disabled looks exactly
// like a quiet run, and the control would be reported as holding when it did
// not run at all.
func TestMarkHookObserved(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "observed")

	t.Setenv(envHookObservedMarker, "")
	markHookObserved()
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("no marker path configured must write no marker")
	}

	t.Setenv(envHookObservedMarker, marker)
	markHookObserved()
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker not written: %v", err)
	}
	markHookObserved() // idempotent: a run makes many tool calls
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("marker lost on the second call: %v", err)
	}

	t.Setenv(envHookObservedMarker, filepath.Join(dir, "missing-dir", "observed"))
	markHookObserved() // an unwritable path must not crash the hook
}

func TestSplitHookList(t *testing.T) {
	got := splitHookList("git status\n\n  make test  \n")
	if len(got) != 2 || got[0] != "git status" || got[1] != "make test" {
		t.Fatalf("splitHookList = %q", got)
	}
	if got := splitHookList("   "); got != nil {
		t.Fatalf("blank list = %q, want none", got)
	}
}
