package daemon

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/permissionprofile"
)

// Permission profiles (K06), daemon side. Glob and prompt logic:
// server/pkg/permissionprofile.

func TestApplyPermissionProfileFiltersSecretsAndAddsProviderFlags(t *testing.T) {
	t.Parallel()
	profile := &permissionprofile.Profile{Name: "code", DeniedPaths: []string{".env*"}, HiddenSecrets: []string{"*PROD*"}}
	agent := &AgentData{CustomEnv: map[string]string{"API_KEY": "k", "PROD_DB": "p"}, CustomArgs: []string{"--foo"}, PermissionProfile: profile}
	applyPermissionProfile(agent, "claude", false, nil)
	if _, ok := agent.CustomEnv["PROD_DB"]; ok || agent.CustomEnv["API_KEY"] != "k" {
		t.Fatalf("env = %v, want PROD_DB withheld", agent.CustomEnv)
	}
	if len(agent.CustomArgs) != 3 || agent.CustomArgs[1] != "--settings" || !strings.Contains(agent.CustomArgs[2], `Edit(.env*)`) {
		t.Fatalf("args = %v", agent.CustomArgs)
	}
	codex := &AgentData{PermissionProfile: &permissionprofile.Profile{Name: "read_only", ReadOnly: true}}
	applyPermissionProfile(codex, "codex", false, nil)
	if strings.Join(codex.CustomArgs, " ") != "--sandbox read-only" {
		t.Fatalf("codex args = %v", codex.CustomArgs)
	}
	none := &AgentData{CustomEnv: map[string]string{"PROD_DB": "p"}}
	applyPermissionProfile(none, "claude", false, nil)
	if none.CustomEnv["PROD_DB"] != "p" || none.CustomArgs != nil {
		t.Fatal("no profile must change nothing")
	}
}

// block_sensitive_files (JEF-256), daemon side: the .env read block lands in
// Claude's --settings deny rules — which hold even under bypassPermissions —
// with or without a permission profile, and stays advisory-only on Codex.
func TestApplyPermissionProfileBlocksSensitiveFiles(t *testing.T) {
	t.Parallel()

	// No profile at all: the sandbox policy alone emits the deny rules.
	agent := &AgentData{CustomEnv: map[string]string{"API_KEY": "k"}}
	applyPermissionProfile(agent, "claude", true, nil)
	if len(agent.CustomArgs) != 2 || agent.CustomArgs[0] != "--settings" {
		t.Fatalf("args = %v", agent.CustomArgs)
	}
	var settings struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal([]byte(agent.CustomArgs[1]), &settings); err != nil {
		t.Fatalf("settings JSON: %v", err)
	}
	for _, want := range []string{`Read(.env)`, `Read(.env.*)`, `Read(**/.env)`, `Read(**/.env.*)`} {
		found := false
		for _, rule := range settings.Permissions.Deny {
			if rule == want {
				found = true
			}
		}
		if !found {
			t.Errorf("deny rules %v miss %q", settings.Permissions.Deny, want)
		}
	}
	if agent.CustomEnv["API_KEY"] != "k" {
		t.Fatal("the sandbox block withholds no env keys on its own")
	}

	// With a profile the rules merge into one payload instead of a second
	// --settings flag.
	merged := &AgentData{PermissionProfile: &permissionprofile.Profile{Name: "code", DeniedPaths: []string{"infra/**"}}}
	applyPermissionProfile(merged, "claude", true, nil)
	if len(merged.CustomArgs) != 2 {
		t.Fatalf("one settings payload, args = %v", merged.CustomArgs)
	}
	if !strings.Contains(merged.CustomArgs[1], `Read(infra/**)`) || !strings.Contains(merged.CustomArgs[1], `Read(**/.env.*)`) {
		t.Fatalf("merged deny rules = %s", merged.CustomArgs[1])
	}

	// Codex has no CLI read deny: nothing is added, container mode and the
	// prompt note carry it.
	codex := &AgentData{}
	applyPermissionProfile(codex, "codex", true, nil)
	if codex.CustomArgs != nil {
		t.Fatalf("codex args = %v, want none", codex.CustomArgs)
	}
}

func TestPermissionProfileTravelsInTheClaimPayload(t *testing.T) {
	t.Parallel()
	var task Task
	if err := json.Unmarshal([]byte(`{"id":"t","agent":{"id":"a","name":"n","instructions":"","permission_profile":{"name":"ci","read_only":false,"denied_paths":["infra/**"],"allowed_commands":["*"],"hidden_secrets":[]}}}`), &task); err != nil {
		t.Fatal(err)
	}
	if task.Agent == nil || task.Agent.PermissionProfile == nil || !task.Agent.PermissionProfile.DeniesPath("infra/main.tf") {
		t.Fatalf("profile not decoded: %+v", task.Agent)
	}
}
