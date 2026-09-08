package handler

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// BYOK (K48) delivers the key that pays for the run, after the permission
// profile has filtered the agent's own secrets. That order is deliberate and
// stays: a profile whose HiddenSecrets is "*" means "withhold the workspace's
// secrets", and withholding the run's own credential too would not make the
// run restrictive, it would stop it.
//
// What changes is the narrower case. A profile that NAMES the vendor variable
// is saying something about that variable, and putting the key back was
// overriding an instruction rather than completing one — silently, so an
// operator who withheld the key to force a run onto a gateway credential
// believed it had worked.
func TestClaimModelKeyRespectsANamedHiddenSecret(t *testing.T) {
	prevBox := testHandler.ModelKeySecretBox
	t.Cleanup(func() { testHandler.ModelKeySecretBox = prevBox })
	box, err := secretbox.New(bytes.Repeat([]byte("p"), secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	testHandler.ModelKeySecretBox = box

	testutil.Call(t, testHandler.CreateModelKey, testutil.WithURLParams(newRequest(http.MethodPost, "/x", map[string]any{
		"scope": "workspace", "provider": "anthropic", "key": "sk-ant-api03-profile-case-00000000abcd", "label": "ws",
	}), "id", testWorkspaceID)).Want(http.StatusCreated)
	t.Cleanup(func() { dbfx.Exec(t, `DELETE FROM workspace_model_key WHERE workspace_id = $1`, testWorkspaceID) })

	claimEnv := func(t *testing.T, hidden string) map[string]string {
		t.Helper()
		runtimeID := dbfx.Runtime(t, "byok profile runtime "+uuid.NewString()[:6], testutil.Cols{"provider": "claude"})
		agentID := dbfx.Agent(t, "byok profile agent "+uuid.NewString()[:6], runtimeID)
		if hidden != "" {
			profile := dbfx.Insert(t, "agent_permission_profile", testutil.Cols{
				"workspace_id":   testWorkspaceID,
				"name":           "gateway " + uuid.NewString()[:6],
				"hidden_secrets": `["` + hidden + `"]`,
			})
			dbfx.Exec(t, `UPDATE agent SET permission_profile_id = $2 WHERE id = $1`, agentID, profile)
		}
		issueID := dbfx.Issue(t, "byok profile issue "+uuid.NewString()[:6], testutil.Cols{"assignee_type": "agent", "assignee_id": agentID})
		dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID, "status": "queued"})
		var claim struct {
			Task *struct {
				Agent *struct {
					CustomEnv map[string]string `json:"custom_env"`
				} `json:"agent"`
			} `json:"task"`
		}
		testutil.Call(t, testHandler.ClaimTaskByRuntime, withURLParam(
			newDaemonTokenRequest(http.MethodPost, "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "byok-profile-daemon"),
			"runtimeId", runtimeID)).Want(http.StatusOK).JSON(&claim)
		if claim.Task == nil || claim.Task.Agent == nil {
			t.Fatal("claim returned no agent")
		}
		return claim.Task.Agent.CustomEnv
	}

	if got := claimEnv(t, "")["ANTHROPIC_API_KEY"]; got == "" {
		t.Error("a run with no profile must still receive the key that pays for it")
	}
	if got := claimEnv(t, "*")["ANTHROPIC_API_KEY"]; got == "" {
		t.Error(`a blanket "*" withholds the workspace's secrets, not the run's own credential`)
	}
	if got := claimEnv(t, "ANTHROPIC_API_KEY")["ANTHROPIC_API_KEY"]; got != "" {
		t.Errorf("a profile that names the variable must be obeyed, got %q", got)
	}
	if got := claimEnv(t, "*_API_KEY")["ANTHROPIC_API_KEY"]; got != "" {
		t.Errorf("a pattern that says something about API keys is deliberate too, got %q", got)
	}
}
