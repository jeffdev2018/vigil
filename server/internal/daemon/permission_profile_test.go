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
	applyPermissionProfile(agent, "claude", nil)
	if _, ok := agent.CustomEnv["PROD_DB"]; ok || agent.CustomEnv["API_KEY"] != "k" {
		t.Fatalf("env = %v, want PROD_DB withheld", agent.CustomEnv)
	}
	if len(agent.CustomArgs) != 3 || agent.CustomArgs[1] != "--settings" || !strings.Contains(agent.CustomArgs[2], `Edit(.env*)`) {
		t.Fatalf("args = %v", agent.CustomArgs)
	}
	codex := &AgentData{PermissionProfile: &permissionprofile.Profile{Name: "read_only", ReadOnly: true}}
	applyPermissionProfile(codex, "codex", nil)
	if strings.Join(codex.CustomArgs, " ") != "--sandbox read-only" {
		t.Fatalf("codex args = %v", codex.CustomArgs)
	}
	none := &AgentData{CustomEnv: map[string]string{"PROD_DB": "p"}}
	applyPermissionProfile(none, "claude", nil)
	if none.CustomEnv["PROD_DB"] != "p" || none.CustomArgs != nil {
		t.Fatal("no profile must change nothing")
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
