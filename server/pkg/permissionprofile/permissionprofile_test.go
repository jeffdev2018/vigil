package permissionprofile

import (
	"strings"
	"testing"
)

func byName(t *testing.T, name string) Profile {
	t.Helper()
	for _, p := range Defaults() {
		if p.Name == name {
			return p
		}
	}
	t.Fatalf("no default profile %q", name)
	return Profile{}
}

func TestDefaultsValidateAndDeny(t *testing.T) {
	for _, p := range Defaults() {
		if err := p.Validate(); err != nil {
			t.Fatalf("%s: %v", p.Name, err)
		}
	}
	code := byName(t, "code")
	for path, want := range map[string]bool{
		".env": true, ".env.local": true, "certs/server.pem": true, ".github/workflows/ci.yml": true, "infra/main.tf": true,
		"src/app.ts": false, "README.md": false, "envelope.go": false,
	} {
		if got := code.DeniesPath(path); got != want {
			t.Fatalf("code denies %q = %v, want %v", path, got, want)
		}
	}
	if got := code.DeniedAmong([]string{"a.go", "deploy/x.yaml", "b.go"}); len(got) != 1 || got[0] != "deploy/x.yaml" {
		t.Fatalf("DeniedAmong = %v", got)
	}
	if byName(t, "production").DeniesPath(".env") {
		t.Fatal("production denies nothing")
	}
	if err := (Profile{Name: "x", DeniedPaths: []string{"[a]"}}).Validate(); err == nil {
		t.Fatal("character classes must be refused")
	}
}

func TestSecretsAndPrompt(t *testing.T) {
	env := map[string]string{"ANTHROPIC_API_KEY": "sk", "prod_db_url": "pg", "DEPLOY_TOKEN": "d"}
	kept, hidden := byName(t, "code").FilterSecrets(env)
	if len(kept) != 1 || kept["ANTHROPIC_API_KEY"] != "sk" || strings.Join(hidden, ",") != "DEPLOY_TOKEN,prod_db_url" {
		t.Fatalf("kept=%v hidden=%v", kept, hidden)
	}
	kept, hidden = byName(t, "read_only").FilterSecrets(env)
	if len(kept) != 0 || len(hidden) != 3 {
		t.Fatalf("read_only must hide everything: kept=%v", kept)
	}
	kept, _ = byName(t, "production").FilterSecrets(env)
	if len(kept) != 3 {
		t.Fatal("production hides nothing")
	}
	ro := byName(t, "read_only")
	prompt := ro.PromptSection()
	if !strings.Contains(prompt, "read-only") || !strings.Contains(prompt, "Only run these commands: git status") {
		t.Fatalf("prompt = %q", prompt)
	}
	if byName(t, "production").PromptSection() == "" || strings.Contains(byName(t, "production").PromptSection(), "Only run") {
		t.Fatal("production prompt names the profile and allows any command")
	}
}

func TestProviderArgs(t *testing.T) {
	ro := byName(t, "read_only")
	if args := ro.ProviderArgs("codex"); strings.Join(args, " ") != "--sandbox read-only" {
		t.Fatalf("codex args = %v", args)
	}
	claude := ro.ProviderArgs("claude")
	if len(claude) != 2 || claude[0] != "--settings" || !strings.Contains(claude[1], `"Edit"`) || !strings.Contains(claude[1], `Bash(git push:*)`) {
		t.Fatalf("claude args = %v", claude)
	}
	code := byName(t, "code").ProviderArgs("claude")
	if !strings.Contains(code[1], `Edit(.env)`) || strings.Contains(code[1], `"Edit"`) {
		t.Fatalf("code claude settings = %v", code)
	}
	if byName(t, "production").ProviderArgs("claude") != nil || byName(t, "code").ProviderArgs("codex") != nil || ro.ProviderArgs("hermes") != nil {
		t.Fatal("nothing to append when the provider cannot enforce anything")
	}
}

// AllowedCommands is told to the model and nothing checks it. That is not an
// oversight to fix here: an allowlist has to say "only these", and the
// provider surfaces this package drives are deny lists with prefix matching.
// This pins the boundary so a later reader does not mistake the prompt
// paragraph for a control, and so a change that starts emitting deny rules
// from an allowlist has to say what it means.
func TestAllowedCommandsProducesNoProviderRule(t *testing.T) {
	restricted := Profile{Name: "narrow", AllowedCommands: []string{"git status", "make test"}}

	if got := restricted.ClaudeSettingsJSON(); got != "" {
		t.Errorf("ClaudeSettingsJSON = %q: an allowlist cannot be expressed as deny rules, and pretending otherwise would refuse the wrong things", got)
	}
	if got := restricted.CodexArgs(); got != nil {
		t.Errorf("CodexArgs = %v, want none: Codex exposes read-only, not a command allowlist", got)
	}
	if got := restricted.ProviderArgs("claude"); got != nil {
		t.Errorf("ProviderArgs(claude) = %v, want none", got)
	}
	if restricted.AllowsAnyCommand() {
		t.Error("a narrow list must still read as narrow: the prompt paragraph depends on it")
	}
	// The one place it does appear, so the model at least reads it.
	if section := restricted.PromptSection(); !strings.Contains(section, "git status, make test") {
		t.Errorf("prompt section must list the commands, got %q", section)
	}

	// And the fields that ARE enforced still produce their rules, so this test
	// fails if someone silences the whole payload rather than just the
	// allowlist.
	enforced := Profile{Name: "code", ReadOnly: true, DeniedPaths: []string{".env"}}
	if enforced.ClaudeSettingsJSON() == "" {
		t.Error("read_only and denied_paths must still reach Claude's deny rules")
	}
	if len(enforced.CodexArgs()) == 0 {
		t.Error("read_only must still reach Codex")
	}
}

// HidesSecretNamed separates "withhold the workspace's secrets" from "withhold
// this variable". Only the second may stop the run's own model credential from
// being delivered: a profile whose HiddenSecrets is "*" that also withheld the
// key paying for the run would not be restrictive, it would be broken.
func TestHidesSecretNamed(t *testing.T) {
	blanket := Profile{Name: "read_only", HiddenSecrets: []string{"*"}}
	if !blanket.HidesSecret("ANTHROPIC_API_KEY") {
		t.Fatal("the blanket glob still hides workspace secrets")
	}
	if blanket.HidesSecretNamed("ANTHROPIC_API_KEY") {
		t.Error(`"*" says nothing about this variable in particular`)
	}

	named := Profile{Name: "gateway", HiddenSecrets: []string{"ANTHROPIC_API_KEY"}}
	if !named.HidesSecretNamed("anthropic_api_key") {
		t.Error("naming the variable is an instruction about it, case aside")
	}

	pattern := Profile{Name: "no-keys", HiddenSecrets: []string{"*_API_KEY"}}
	if !pattern.HidesSecretNamed("OPENAI_API_KEY") {
		t.Error("a pattern that says something about API keys is deliberate too")
	}
	if pattern.HidesSecretNamed("DATABASE_URL") {
		t.Error("a pattern that does not match must not withhold")
	}

	mixed := Profile{Name: "mixed", HiddenSecrets: []string{"*", "*_TOKEN"}}
	if mixed.HidesSecretNamed("ANTHROPIC_API_KEY") {
		t.Error("the blanket is ignored and the other pattern does not match")
	}
	if !mixed.HidesSecretNamed("GITHUB_TOKEN") {
		t.Error("a deliberate pattern beside the blanket still counts")
	}

	if (Profile{Name: "open"}).HidesSecretNamed("ANTHROPIC_API_KEY") {
		t.Error("no hidden secrets, nothing named")
	}
}
