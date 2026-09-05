package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Run confidence scoring (JEF-240): the workspace settings endpoints and the
// confidence block exposed on task responses.

func TestConfidenceReviewSettingsDefaultsAndRoundTrip(t *testing.T) {
	rememberSettings(t)

	var got service.ConfidenceReview
	testutil.Call(t, testHandler.GetConfidenceReviewSettings, newRequest(http.MethodGet, "/api/confidence-review-settings", nil)).Want(http.StatusOK).JSON(&got)
	if got != service.DefaultConfidenceReview {
		t.Fatalf("defaults = %+v, want %+v", got, service.DefaultConfidenceReview)
	}

	testutil.Call(t, testHandler.PutConfidenceReviewSettings, newRequest(http.MethodPut, "/api/confidence-review-settings", map[string]any{"enabled": false, "threshold": 0.7})).Want(http.StatusOK)
	got = service.ConfidenceReview{}
	testutil.Call(t, testHandler.GetConfidenceReviewSettings, newRequest(http.MethodGet, "/api/confidence-review-settings", nil)).Want(http.StatusOK).JSON(&got)
	if got.Enabled || got.Threshold != 0.7 {
		t.Fatalf("after PUT = %+v, want {false, 0.7}", got)
	}
	// The write must merge into workspace.settings, not clobber other keys.
	var settings map[string]any
	dbfx.QueryRow(t, `SELECT settings FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&settings)
	if _, ok := settings["confidence_review"]; !ok {
		t.Fatalf("confidence_review missing from workspace settings: %v", settings)
	}
}

func TestConfidenceReviewSettingsRejectsBadThreshold(t *testing.T) {
	rememberSettings(t)
	testutil.Call(t, testHandler.PutConfidenceReviewSettings, newRequest(http.MethodPut, "/api/confidence-review-settings", map[string]any{"enabled": true, "threshold": 0})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.PutConfidenceReviewSettings, newRequest(http.MethodPut, "/api/confidence-review-settings", map[string]any{"enabled": true, "threshold": 1.5})).Want(http.StatusBadRequest)
}

func TestTaskToResponseExposesConfidence(t *testing.T) {
	stored := []byte(`{"score":0.85,"rationale":"verified build","model":"gpt-5.6-luna","threshold":0.5,"below_threshold":false}`)
	with := taskToResponse(db.AgentTaskQueue{Confidence: stored}, "ws")
	var decoded struct {
		Score          float64 `json:"score"`
		BelowThreshold bool    `json:"below_threshold"`
	}
	if err := json.Unmarshal(with.Confidence, &decoded); err != nil {
		t.Fatalf("confidence not exposed: %v", err)
	}
	if decoded.Score != 0.85 || decoded.BelowThreshold {
		t.Errorf("confidence = %+v", decoded)
	}

	without := taskToResponse(db.AgentTaskQueue{}, "ws")
	if len(without.Confidence) != 0 {
		t.Errorf("unscored task exposes confidence = %s, want empty", without.Confidence)
	}
	// omitempty: the key must be absent from the wire form.
	wire, err := json.Marshal(without)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var asMap map[string]any
	if err := json.Unmarshal(wire, &asMap); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if _, present := asMap["confidence"]; present {
		t.Error("confidence key present on an unscored task")
	}
}
