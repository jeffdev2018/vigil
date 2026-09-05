package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/triage"
)

func TestTriageAcceptAppliesOverrides(t *testing.T) {
	itemID := newPendingTriageItem(t, "accept as "+uuid.NewString())
	projectID := dbfx.Insert(t, "project", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"title":        "Triage overrides " + uuid.NewString()[:8],
		"description":  "",
		"status":       "planned",
	})

	var out struct {
		Issue struct {
			ID string `json:"id"`
		} `json:"issue"`
	}
	testutil.Call(t, testHandler.AcceptTriageItem, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/triage/items/"+itemID+"/accept", map[string]any{
			"assignee_type": "member",
			"assignee_id":   testUserID,
			"project_id":    projectID,
			"priority":      "urgent",
		}),
		"id", itemID,
	)).Want(http.StatusOK).JSON(&out)
	if out.Issue.ID == "" {
		t.Fatal("accept returned no issue")
	}
	dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, out.Issue.ID)

	var assigneeType, assigneeID, gotProject, priority string
	if err := testPool.QueryRow(context.Background(), `
		SELECT COALESCE(assignee_type, ''), COALESCE(assignee_id::text, ''),
		       COALESCE(project_id::text, ''), priority
		FROM issue WHERE id = $1
	`, out.Issue.ID).Scan(&assigneeType, &assigneeID, &gotProject, &priority); err != nil {
		t.Fatalf("load accepted issue: %v", err)
	}
	if assigneeType != "member" || assigneeID != testUserID {
		t.Fatalf("assignee = %s/%s, want the chosen member %s", assigneeType, assigneeID, testUserID)
	}
	if gotProject != projectID || priority != "urgent" {
		t.Fatalf("project/priority = %s/%s, want %s/urgent", gotProject, priority, projectID)
	}
}

func TestTriageAcceptRejectsInvalidOverrides(t *testing.T) {
	itemID := newPendingTriageItem(t, "accept bad "+uuid.NewString())

	for name, body := range map[string]map[string]any{
		"priority":      {"priority": "critical"},
		"assignee_type": {"assignee_type": "squad", "assignee_id": testUserID},
		"assignee_id":   {"assignee_type": "member", "assignee_id": "not-a-uuid"},
		"orphan_id":     {"assignee_id": testUserID},
	} {
		t.Run(name, func(t *testing.T) {
			testutil.Call(t, testHandler.AcceptTriageItem, testutil.WithURLParams(
				newRequest(http.MethodPost, "/api/triage/items/"+itemID+"/accept", body),
				"id", itemID,
			)).Want(http.StatusBadRequest)
		})
	}

	// A rejected accept must leave the item in the queue.
	var state string
	if err := testPool.QueryRow(context.Background(),
		`SELECT state FROM triage_item WHERE id = $1`, itemID).Scan(&state); err != nil {
		t.Fatalf("load item: %v", err)
	}
	if state != triage.StatePending {
		t.Fatalf("item = %s after rejected accepts, want pending", state)
	}
}
