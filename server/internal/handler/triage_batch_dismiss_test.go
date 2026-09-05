package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/triage"
)

func TestTriageBatchDismissRecordsSharedReason(t *testing.T) {
	first := newPendingTriageItem(t, "batch dismiss a "+uuid.NewString())
	second := newPendingTriageItem(t, "batch dismiss b "+uuid.NewString())
	missing := uuid.NewString()

	var out struct {
		Items []BatchDismissTriageItem `json:"items"`
	}
	testutil.Call(t, testHandler.BatchDismissTriageItems,
		newRequest(http.MethodPost, "/api/triage/items/batch-dismiss", map[string]any{
			"item_ids": []string{first, second, missing, "not-a-uuid"},
			"reason":   "alert storm",
		}),
	).Want(http.StatusOK).JSON(&out)

	got := make(map[string]string, len(out.Items))
	for _, item := range out.Items {
		got[item.ID] = item.Outcome
	}
	if got[first] != "dismissed" || got[second] != "dismissed" {
		t.Fatalf("outcomes = %+v, want both items dismissed", got)
	}
	if got[missing] != "not_pending" || got["not-a-uuid"] != "not_found" {
		t.Fatalf("outcomes = %+v, want not_pending / not_found for the unusable ids", got)
	}

	var state, reason string
	if err := testPool.QueryRow(context.Background(),
		`SELECT state, COALESCE(resolution_reason, '') FROM triage_item WHERE id = $1`, first,
	).Scan(&state, &reason); err != nil {
		t.Fatalf("load dismissed item: %v", err)
	}
	if state != triage.StateDismissed || reason != "alert storm" {
		t.Fatalf("item = %s (%q), want dismissed (alert storm)", state, reason)
	}
}

func TestTriageBatchDismissRejectsEmptyAndOversizedBatches(t *testing.T) {
	testutil.Call(t, testHandler.BatchDismissTriageItems,
		newRequest(http.MethodPost, "/api/triage/items/batch-dismiss", map[string]any{"item_ids": []string{}}),
	).Want(http.StatusBadRequest)

	tooMany := make([]string, triageMaxBatchAccept+1)
	for i := range tooMany {
		tooMany[i] = uuid.NewString()
	}
	testutil.Call(t, testHandler.BatchDismissTriageItems,
		newRequest(http.MethodPost, "/api/triage/items/batch-dismiss", map[string]any{"item_ids": tooMany}),
	).Want(http.StatusBadRequest)
}
