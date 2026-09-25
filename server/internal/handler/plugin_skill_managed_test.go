package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// A plugin's skill is a generated artifact of its installation: a manifest
// re-fetch or upgrade would silently discard an edit made through the skill
// API, so every direct-write endpoint must refuse rather than accept and lose
// it. Uninstalling (or upgrading) the plugin is the supported way to change
// it. This creates an ordinary skill through the normal API, then marks it
// plugin-owned directly in the database (the way an install would) to
// exercise the refusal on each endpoint.
func TestPluginManagedSkillRefusesDirectEdits(t *testing.T) {
	if testPool == nil {
		t.Skip("no database available")
	}

	req := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/skills", CreateSkillRequest{
		Name:    "test-skill-plugin-managed",
		Content: "# SKILL.md content",
		Files:   []CreateSkillFileRequest{{Path: "README.md", Content: "readme"}},
	})
	rec := httptest.NewRecorder()
	testHandler.CreateSkill(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create skill: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created SkillWithFilesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	fileID := created.Files[0].ID

	installationID := dbfx.Insert(t, "plugin_installation", testutil.Cols{
		"workspace_id":       testWorkspaceID,
		"plugin_key":         "com.example.skill-managed-test",
		"version":            "1.0.0",
		"manifest":           testutil.Raw(`'{"manifest_version":1}'::jsonb`),
		"package_version_id": testutil.Raw("gen_random_uuid()"),
	})
	dbfx.Exec(t, "UPDATE skill SET plugin_installation_id = $1 WHERE id = $2", installationID, created.ID)

	// The detail response must surface which installation owns it, so a
	// client can grey out edit affordances instead of hitting these 409s.
	getReq := withURLParam(newRequest(http.MethodGet, "/api/skills/"+created.ID, nil), "id", created.ID)
	getRec := httptest.NewRecorder()
	testHandler.GetSkill(getRec, getReq)
	var getResp SkillResponse
	if err := json.Unmarshal(getRec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("unmarshal get response: %v", err)
	}
	if getResp.PluginInstallationID == nil || *getResp.PluginInstallationID != installationID {
		t.Fatalf("plugin_installation_id = %v, want %q", getResp.PluginInstallationID, installationID)
	}

	updateReq := withURLParam(newRequest(http.MethodPut, "/api/skills/"+created.ID, UpdateSkillRequest{
		Name: strPtr("renamed"),
	}), "id", created.ID)
	updateRec := httptest.NewRecorder()
	testHandler.UpdateSkill(updateRec, updateReq)
	if updateRec.Code != http.StatusConflict {
		t.Fatalf("UpdateSkill on a plugin-managed skill: status=%d body=%s, want 409", updateRec.Code, updateRec.Body.String())
	}

	upsertReq := withURLParam(newRequest(http.MethodPost, "/api/skills/"+created.ID+"/files", CreateSkillFileRequest{
		Path: "new.md", Content: "x",
	}), "id", created.ID)
	upsertRec := httptest.NewRecorder()
	testHandler.UpsertSkillFile(upsertRec, upsertReq)
	if upsertRec.Code != http.StatusConflict {
		t.Fatalf("UpsertSkillFile on a plugin-managed skill: status=%d body=%s, want 409", upsertRec.Code, upsertRec.Body.String())
	}

	deleteFileReq := withURLParams(newRequest(http.MethodDelete, "/api/skills/"+created.ID+"/files/"+fileID, nil),
		"id", created.ID, "fileId", fileID)
	deleteFileRec := httptest.NewRecorder()
	testHandler.DeleteSkillFile(deleteFileRec, deleteFileReq)
	if deleteFileRec.Code != http.StatusConflict {
		t.Fatalf("DeleteSkillFile on a plugin-managed skill: status=%d body=%s, want 409", deleteFileRec.Code, deleteFileRec.Body.String())
	}

	deleteReq := withURLParam(newRequest(http.MethodDelete, "/api/skills/"+created.ID, nil), "id", created.ID)
	deleteRec := httptest.NewRecorder()
	testHandler.DeleteSkill(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusConflict {
		t.Fatalf("DeleteSkill on a plugin-managed skill: status=%d body=%s, want 409", deleteRec.Code, deleteRec.Body.String())
	}

	// Cleanup: this skill was created outside dbfx's tracked tables, so
	// remove it and its file explicitly now that the refusal is proven.
	dbfx.Exec(t, "DELETE FROM skill_file WHERE skill_id = $1", created.ID)
	dbfx.Exec(t, "DELETE FROM skill WHERE id = $1", created.ID)
}
