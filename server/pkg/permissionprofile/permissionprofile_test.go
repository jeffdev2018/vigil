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
