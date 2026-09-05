package handler

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// agentRequest stamps the server-set actor headers the auth middleware writes
// for a mat_ task token, which is how resolveActor classifies an agent.
func agentRequest(t *testing.T, agentID string, method, path string, body any) *http.Request {
	t.Helper()
	req := newRequest(method, path, body)
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Agent-ID", agentID)
	return req
}

func TestTriageVerdictIsAgentOnly(t *testing.T) {
	itemID := newPendingTriageItem(t, "verdict "+uuid.NewString())
	agentID := createWebhookTestAgent(t, "Triage Verdict Agent")

	// A human calling the agent-only endpoint is refused, verdict untouched.
	testutil.Call(t, testHandler.SetTriageVerdict, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/triage/items/"+itemID+"/verdict",
			map[string]any{"verdict": "accept", "reason": "looks real"}),
		"id", itemID,
	)).Want(http.StatusForbidden)

	var out struct {
		Verdict         string `json:"verdict"`
		VerdictReason   string `json:"verdict_reason"`
		VerdictAgentID  string `json:"verdict_agent_id"`
		VerdictRevision int64  `json:"verdict_revision"`
	}
	testutil.Call(t, testHandler.SetTriageVerdict, testutil.WithURLParams(
		agentRequest(t, agentID, http.MethodPost, "/api/triage/items/"+itemID+"/verdict",
			map[string]any{"verdict": "dismiss", "reason": "duplicate alert noise"}),
		"id", itemID,
	)).Want(http.StatusOK).JSON(&out)
	if out.Verdict != "dismiss" || out.VerdictAgentID != agentID || out.VerdictRevision != 1 {
		t.Fatalf("verdict response = %+v, want dismiss by %s at revision 1", out, agentID)
	}

	// An unknown verdict is refused rather than stored.
	testutil.Call(t, testHandler.SetTriageVerdict, testutil.WithURLParams(
		agentRequest(t, agentID, http.MethodPost, "/api/triage/items/"+itemID+"/verdict",
			map[string]any{"verdict": "maybe"}),
		"id", itemID,
	)).Want(http.StatusBadRequest)

	// A suggestion is advisory: the item is still pending, and the queue
	// listing carries the suggestion so the UI can show it.
	var listed struct {
		Items []TriageItemResponse `json:"items"`
	}
	testutil.Call(t, testHandler.ListTriageItems,
		newRequest(http.MethodGet, "/api/triage/items?state=pending&limit=100", nil),
	).Want(http.StatusOK).JSON(&listed)
	for _, item := range listed.Items {
		if item.ID != itemID {
			continue
		}
		if item.Verdict != "dismiss" || item.VerdictReason != "duplicate alert noise" {
			t.Fatalf("listed verdict = %q/%q, want dismiss/duplicate alert noise", item.Verdict, item.VerdictReason)
		}
		if item.VerdictAgentID != agentID || item.VerdictAt == nil {
			t.Fatalf("listed verdict attribution = %q/%v, want %s with a timestamp", item.VerdictAgentID, item.VerdictAt, agentID)
		}
		return
	}
	t.Fatalf("item %s left the pending queue after an agent verdict", itemID)
}
