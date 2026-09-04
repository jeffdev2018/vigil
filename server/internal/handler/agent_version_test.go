package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Agent versions (K23): a snapshot after every change, a rollback that is
// a new version, and the version a run was created under.

func agentCall(t *testing.T, h http.HandlerFunc, method, path string, body map[string]any, params ...string) *testutil.Response {
	t.Helper()
	req := testutil.WithHeaders(newRequest(method, path, body), "X-Workspace-ID", testWorkspaceID)
	return testutil.Call(t, h, testutil.WithURLParams(req, params...))
}

type versionsEnvelope struct {
	Versions []AgentVersionResponse `json:"versions"`
}

func TestAgentVersionsSnapshotDiffAndRollback(t *testing.T) {
	agent := dbfx.Agent(t, "versioned agent", handlerTestRuntimeID(t), testutil.Cols{"instructions": "Be brief.", "model": "m-1", "owner_id": testUserID})
	t.Cleanup(func() { testPool.Exec(t.Context(), `DELETE FROM agent_version WHERE agent_id = $1`, agent) })
	skill := dbfx.Insert(t, "skill", testutil.Cols{"workspace_id": testWorkspaceID, "name": "skill-" + uuid.NewString()[:6], "description": "", "content": "# skill", "created_by": testUserID})

	// The first read seeds v1 from the current state.
	var list versionsEnvelope
	agentCall(t, testHandler.ListAgentVersions, http.MethodGet, "/api/agents/"+agent+"/versions", nil, "id", agent).Want(http.StatusOK).JSON(&list)
	if len(list.Versions) != 1 || list.Versions[0].VersionNumber != 1 || list.Versions[0].Instructions != "Be brief." || !list.Versions[0].Active {
		t.Fatalf("baseline = %+v", list.Versions)
	}
	v1 := list.Versions[0]

	// A config change records v2; an identical save records nothing.
	agentCall(t, testHandler.UpdateAgent, http.MethodPut, "/api/agents/"+agent, map[string]any{"instructions": "Be thorough."}, "id", agent).Want(http.StatusOK)
	agentCall(t, testHandler.UpdateAgent, http.MethodPut, "/api/agents/"+agent, map[string]any{"instructions": "Be thorough."}, "id", agent).Want(http.StatusOK)
	agentCall(t, testHandler.ListAgentVersions, http.MethodGet, "/api/agents/"+agent+"/versions", nil, "id", agent).Want(http.StatusOK).JSON(&list)
	if len(list.Versions) != 2 || list.Versions[0].VersionNumber != 2 || list.Versions[0].Instructions != "Be thorough." || list.Versions[1].Active {
		t.Fatalf("after update = %+v", list.Versions)
	}
	v2 := list.Versions[0]

	// Skills are part of the snapshot.
	agentCall(t, testHandler.SetAgentSkills, http.MethodPut, "/api/agents/"+agent+"/skills", map[string]any{"skill_ids": []string{skill}}, "id", agent).Want(http.StatusOK)
	agentCall(t, testHandler.ListAgentVersions, http.MethodGet, "/api/agents/"+agent+"/versions", nil, "id", agent).Want(http.StatusOK).JSON(&list)
	if len(list.Versions) != 3 || len(list.Versions[0].SkillIDs) != 1 || list.Versions[0].SkillIDs[0] != skill {
		t.Fatalf("after skills = %+v", list.Versions)
	}
	v3 := list.Versions[0]

	// Diff names the changed fields.
	var diff AgentVersionDiff
	agentCall(t, testHandler.GetAgentVersionDiff, http.MethodGet, "/api/agents/"+agent+"/versions/"+v3.ID+"/diff?against="+v1.ID, nil, "id", agent, "versionId", v3.ID).Want(http.StatusOK).JSON(&diff)
	if len(diff.ChangedFields) != 2 || diff.ChangedFields[0] != "instructions" || diff.ChangedFields[1] != "skills" || !diff.To.Active || diff.From.Active {
		t.Fatalf("diff = %+v", diff)
	}
	agentCall(t, testHandler.GetAgentVersionDiff, http.MethodGet, "/api/agents/"+agent+"/versions/"+v3.ID+"/diff?against="+uuid.NewString(), nil, "id", agent, "versionId", v3.ID).Want(http.StatusNotFound)

	// Rolling back to the active state is refused; to v1 writes the agent
	// back and records v4, identical to v1.
	agentCall(t, testHandler.RollbackAgentVersion, http.MethodPost, "/api/agents/"+agent+"/versions/"+v3.ID+"/rollback", map[string]any{}, "id", agent, "versionId", v3.ID).Want(http.StatusConflict)
	var rolled struct{ Version AgentVersionResponse }
	agentCall(t, testHandler.RollbackAgentVersion, http.MethodPost, "/api/agents/"+agent+"/versions/"+v1.ID+"/rollback", map[string]any{}, "id", agent, "versionId", v1.ID).Want(http.StatusOK).JSON(&rolled)
	if rolled.Version.VersionNumber != 4 || rolled.Version.Instructions != "Be brief." || len(rolled.Version.SkillIDs) != 0 || rolled.Version.Note != "Rollback to v1" || !rolled.Version.Active {
		t.Fatalf("rollback = %+v", rolled.Version)
	}
	var instructions string
	dbfx.QueryRow(t, `SELECT instructions FROM agent WHERE id = $1`, agent).Scan(&instructions)
	if instructions != "Be brief." {
		t.Fatalf("agent instructions after rollback = %q", instructions)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_skill WHERE agent_id = $1`, agent); n != 0 {
		t.Fatalf("skills after rollback = %d, want 0", n)
	}
	_ = v2

	// A run created now belongs to v4; one created before v2 existed to v1.
	if got := testHandler.agentVersionNumberAt(t.Context(), parseUUID(agent), time.Now()); got != 4 {
		t.Fatalf("version at now = %d, want 4", got)
	}
	var v1At time.Time
	dbfx.QueryRow(t, `SELECT created_at FROM agent_version WHERE id = $1`, v1.ID).Scan(&v1At)
	if got := testHandler.agentVersionNumberAt(t.Context(), parseUUID(agent), v1At); got != 1 {
		t.Fatalf("version at v1's creation = %d, want 1", got)
	}

	// Purge with the workspace.
	ws := dbfx.Workspace(t, "Version purge", "version-purge-"+uuid.NewString())
	dbfx.Insert(t, "agent_version", testutil.Cols{"workspace_id": ws, "agent_id": agent, "version_number": 99})
	if err := testHandler.Queries.DeleteWorkspaceLeafData(t.Context(), parseUUID(ws)); err != nil {
		t.Fatal(err)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM agent_version WHERE workspace_id = $1`, ws); n != 0 {
		t.Fatalf("versions after purge = %d", n)
	}
}
