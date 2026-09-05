package agent

import (
	"sort"
	"strings"
	"testing"
)

// The table is the contract two packages read (the daemon runs the commands,
// the API decides whether to offer the buttons), so it is pinned here rather
// than in either of them.
func TestCLIAuthTableOnlyClaimsDocumentedCommands(t *testing.T) {
	got := CLIAuthProviders()
	sort.Strings(got)
	want := []string{"claude", "codex", "copilot", "cursor-agent"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("providers = %v, want %v", got, want)
	}
	// Interactive-only CLIs are deliberately absent: `opencode auth login`
	// prompts for a provider to pick, so under the daemon (no terminal) it
	// would hang until the timeout rather than sign anyone in.
	for _, provider := range []string{"opencode", "gemini", "qwen", "aider", ""} {
		if CLIAuthSupported(provider) {
			t.Fatalf("%q must not be offered a sign-in button", provider)
		}
		if _, ok := CLIAuthAction(provider, "login"); ok {
			t.Fatalf("%q must have no login command", provider)
		}
	}
}

func TestCLIAuthActionRefusesAnUndocumentedAction(t *testing.T) {
	if _, ok := CLIAuthAction("copilot", "login"); !ok {
		t.Fatal("copilot documents an OAuth device-flow login")
	}
	// Signing out of Copilot CLI is an in-session slash command, not a shell
	// subcommand. Absent must not degrade into "run the binary with no args".
	if args, ok := CLIAuthAction("copilot", "logout"); ok || len(args) != 0 {
		t.Fatalf("copilot logout = %v, %v; want absent", args, ok)
	}
	if args, ok := CLIAuthAction("claude", "refresh"); ok || len(args) != 0 {
		t.Fatalf("unknown action = %v, %v; want absent", args, ok)
	}
}

// A status probe is trusted by exit code alone, so only providers documenting
// that contract may have one.
func TestCLIAuthStatusOnlyWhereTheExitCodeIsDocumented(t *testing.T) {
	for provider, want := range map[string]string{
		"claude": "auth status",
		"codex":  "login status",
	} {
		args, ok := CLIAuthStatus(provider)
		if !ok || strings.Join(args, " ") != want {
			t.Fatalf("%s status = %v, %v; want %q", provider, args, ok, want)
		}
	}
	for _, provider := range []string{"cursor-agent", "copilot", "opencode"} {
		if args, ok := CLIAuthStatus(provider); ok {
			t.Fatalf("%s status = %v; want none", provider, args)
		}
	}
}
