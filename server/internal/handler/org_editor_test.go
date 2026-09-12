package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestOrgRevisionRestore(t *testing.T) {
	project := dbfx.Project(t, "revision restore "+uuid.NewString()[:8])
	s := orgCreate(t, map[string]any{"project_id": project, "model": "hierarchy", "definition": OrgDefinition{Units: []OrgUnit{orgUnit("lead", "Original", testUserID)}}, "budget_usd_ticks": 100}, http.StatusCreated)
	t.Cleanup(func() {
		dbfx.Exec(t, "DELETE FROM org_revision WHERE structure_id = $1", s.ID)
		dbfx.Exec(t, "DELETE FROM org_structure WHERE id = $1", s.ID)
	})
	initial := *s.RevisionID
	update := func(body map[string]any, status int) OrgStructureResponse {
		var response OrgStructureResponse
		res := testutil.Call(t, testHandler.UpdateOrgStructure, testutil.WithURLParams(newRequest(http.MethodPut, "/api/org/"+s.ID, body), "id", s.ID)).Want(status)
		if status == http.StatusOK {
			res.JSON(&response)
		}
		return response
	}
	changed := update(map[string]any{"expected_revision": 1, "definition": OrgDefinition{Units: []OrgUnit{orgUnit("lead", "Changed", testUserID)}}, "budget_usd_ticks": 0}, http.StatusOK)
	if changed.Revision != 2 || changed.BudgetUsdTicks != 0 {
		t.Fatalf("save/clear budget: %+v", changed)
	}
	update(map[string]any{"expected_revision": 1, "restore_revision_id": initial}, http.StatusConflict)
	restored := update(map[string]any{"expected_revision": 2, "restore_revision_id": initial}, http.StatusOK)
	if restored.Revision != 3 || restored.Definition.Units[0].Name != "Original" || restored.Status != "draft" || restored.BudgetUsdTicks != 0 {
		t.Fatalf("restore must create revision and preserve settings: %+v", restored)
	}
	active := orgAction(t, s.ID, "activate", map[string]any{"eval_attestation": "fixture evaluation: 30 cases passed"}, http.StatusOK)
	restoredActive := update(map[string]any{"expected_revision": active.Revision, "restore_revision_id": initial}, http.StatusOK)
	if restoredActive.Status != "active" || restoredActive.Revision != 5 || restoredActive.Definition.Units[0].Name != "Original" {
		t.Fatalf("restore after activation: %+v", restoredActive)
	}

	foreign := dbfx.Insert(t, "org_revision", testutil.Cols{"id": uuid.NewString(), "workspace_id": testWorkspaceID, "structure_id": uuid.NewString(), "revision": 1, "model": "hierarchy", "status": "draft", "definition": "{}"})
	update(map[string]any{"expected_revision": 5, "restore_revision_id": foreign}, http.StatusNotFound)
	rows, err := testHandler.Queries.ListOrgRevisions(context.Background(), db.ListOrgRevisionsParams{StructureID: parseUUID(s.ID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil || len(rows) != 5 {
		t.Fatalf("revision history: %d %v", len(rows), err)
	}
	// Failure between updating the live row and committing its revision is atomic.
	original := testHandler.TxStarter
	testHandler.TxStarter = rollbackOnCommitTxStarter{pool: testPool}
	t.Cleanup(func() { testHandler.TxStarter = original })
	update(map[string]any{"expected_revision": 5, "name": "Must roll back"}, http.StatusInternalServerError)
	testHandler.TxStarter = original
	live, err := testHandler.Queries.GetOrgStructure(context.Background(), db.GetOrgStructureParams{ID: parseUUID(s.ID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil || live.Revision != 5 {
		t.Fatalf("failed save must roll back: %d %v", live.Revision, err)
	}
}

func TestOrgReportingCycle(t *testing.T) {
	def := OrgDefinition{Units: []OrgUnit{{ID: "root"}, {ID: "a"}, {ID: "b"}}, Edges: []OrgEdge{{From: "a", To: "b", Kind: "reports_to"}, {From: "b", To: "a", Kind: "reports_to"}}}
	if validateOrgReporting(def, OrgModelHierarchy) == nil {
		t.Fatal("disconnected reporting cycle accepted")
	}
}

func TestOrgTeamCatalogImport(t *testing.T) {
	ws := dbfx.Workspace(t, "catalog import", "catalog-"+uuid.NewString()[:8])
	dbfx.Member(t, ws, testUserID, "owner")
	t.Cleanup(func() {
		req := testutil.WithURLParams(newRequest(http.MethodDelete, "/api/workspaces/"+ws, nil), "id", ws)
		req.Header.Set("X-Workspace-ID", ws)
		testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)
	})
	for _, team := range orgTeamCatalog() {
		t.Run(team.ID, func(t *testing.T) {
			bundle := orgTeamBundle(team)
			var definition OrgDefinition
			if err := json.Unmarshal(bundle.Org[0].Definition, &definition); err != nil {
				t.Fatal(err)
			}
			if len(definition.Units) != 1 || len(definition.Units[0].Members) != 3 || len(definition.Units[0].Roles) != 3 || definition.Units[0].Members[0].Role != "lead" || len(definition.Edges) != 0 {
				t.Fatal("a catalog team must contain three members and roles, not three teams")
			}
			data, err := zipTransferBundle(bundle)
			if err != nil {
				t.Fatal(err)
			}
			var preview transferPreview
			testutil.Call(t, testHandler.PreviewWorkspaceImport, transferMultipart(t, ws, "/api/workspace-transfer/preview", data, nil)).Want(http.StatusOK).JSON(&preview)
			if preview.Manifest.Counts["agents"] != 3 {
				t.Fatal("preview must describe all roles")
			}
			var result struct {
				Report transferReport `json:"report"`
			}
			testutil.Call(t, testHandler.ImportWorkspace, transferMultipart(t, ws, "/api/workspace-transfer/import", data, map[string]string{"strategy": "rename"})).Want(http.StatusOK).JSON(&result)
			if result.Report.Created["agents"] != 3 || result.Report.Created["skills"] != 1 || result.Report.Created["org_structures"] != 1 || result.Report.Created["issues"] != 1 || result.Report.Created["projects"] != 1 || result.Report.Created["autopilots"] != 1 || len(result.Report.Warnings) > 0 {
				t.Fatalf("incomplete team: %+v", result.Report)
			}
		})
	}
	if dbfx.Count(t, "SELECT COUNT(*) FROM autopilot WHERE workspace_id=$1 AND status <> 'paused'", ws) != 0 {
		t.Fatal("import started recurring work")
	}
	if dbfx.Count(t, "SELECT COUNT(*) FROM agent WHERE workspace_id=$1 AND (runtime_id IS NOT NULL OR trust_mode <> 'propose')", ws) != 0 {
		t.Fatal("import granted execution or excess trust")
	}
}

// TestOrgTeamBundleSurvivesAFourthRole guards a real crash: orgTeamBundle
// used to index a hardcoded 3-element []string{"lead","delivery","review"}
// by the loop position over team.Roles with no bounds check. Every catalog
// entry today has exactly 3 roles so it never panicked in production, but a
// future catalog edit adding a 4th role would have. No DB needed — this is a
// pure function over an in-memory template.
func TestOrgTeamBundleSurvivesAFourthRole(t *testing.T) {
	team := orgTeamTemplate{
		ID:          "quartet",
		Name:        "Quartet team",
		Description: "four roles",
		Roles:       []string{"Lead", "Delivery", "Review", "Extra"},
		Procedure:   "do the work",
	}
	bundle := orgTeamBundle(team) // must not panic (index out of range)
	var definition OrgDefinition
	if err := json.Unmarshal(bundle.Org[0].Definition, &definition); err != nil {
		t.Fatal(err)
	}
	if len(definition.Units) != 1 || len(definition.Units[0].Roles) != 4 {
		t.Fatalf("expected 4 roles, got %+v", definition.Units[0].Roles)
	}
	ids := make([]string, len(definition.Units[0].Roles))
	for i, r := range definition.Units[0].Roles {
		ids[i] = r.ID
	}
	if ids[0] != "lead" || ids[1] != "delivery" || ids[2] != "review" || ids[3] == "" {
		t.Fatalf("role ids = %v, want the first three named and a non-empty fallback for the fourth", ids)
	}
}
