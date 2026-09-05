package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// Both list endpoints used to answer `total: len(page)`, so a 20-row first
// page reported that 20 was everything there was — the number a reader uses to
// decide whether to page at all.
func TestAutopilotListTotalsCountEverything(t *testing.T) {
	agentID := createWebhookTestAgent(t, "Totals Agent")
	apID := createWebhookTestAutopilot(t, agentID, "active", "run_only")
	cleanupTriageRowsForAutopilot(t, apID)
	trig := createWebhookTriggerViaHandler(t, apID)

	const rows = 3
	for i := 0; i < rows; i++ {
		dbfx.Insert(t, "autopilot_run", testutil.Cols{
			"autopilot_id": apID,
			"source":       "manual",
			"status":       "completed",
		})
		dbfx.Insert(t, "webhook_delivery", testutil.Cols{
			"workspace_id":     testWorkspaceID,
			"autopilot_id":     apID,
			"trigger_id":       trig.ID,
			"provider":         "generic",
			"event":            fmt.Sprintf("test.event.%d", i),
			"signature_status": sigStatusNotRequired,
			"status":           deliveryStatusDispatched,
			"raw_body":         []byte(`{}`),
		})
	}

	t.Run("runs", func(t *testing.T) {
		req := withURLParam(newRequest("GET", "/api/autopilots/"+apID+"/runs?limit=2", nil), "id", apID)
		out := testutil.Call(t, testHandler.ListAutopilotRuns, req).Want(http.StatusOK).Map()
		list, _ := out["runs"].([]any)
		if len(list) != 2 {
			t.Fatalf("returned %d runs, want the requested page of 2", len(list))
		}
		if out["total"] != float64(rows) {
			t.Fatalf("total = %v, want %d — the page length is not the total", out["total"], rows)
		}
	})

	t.Run("deliveries", func(t *testing.T) {
		req := withURLParam(newRequest("GET", "/api/autopilots/"+apID+"/deliveries?limit=2", nil), "id", apID)
		out := testutil.Call(t, testHandler.ListAutopilotDeliveries, req).Want(http.StatusOK).Map()
		list, _ := out["deliveries"].([]any)
		if len(list) != 2 {
			t.Fatalf("returned %d deliveries, want the requested page of 2", len(list))
		}
		if out["total"] != float64(rows) {
			t.Fatalf("total = %v, want %d — the page length is not the total", out["total"], rows)
		}
	})

	t.Run("second page", func(t *testing.T) {
		req := withURLParam(newRequest("GET", "/api/autopilots/"+apID+"/runs?limit=2&offset=2", nil), "id", apID)
		out := testutil.Call(t, testHandler.ListAutopilotRuns, req).Want(http.StatusOK).Map()
		list, _ := out["runs"].([]any)
		if len(list) != 1 {
			t.Fatalf("second page returned %d runs, want the 1 row left", len(list))
		}
		if out["total"] != float64(rows) {
			t.Fatalf("total = %v on page 2, want the same %d", out["total"], rows)
		}
	})
}
