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
	// A PUT that omits max_escalations keeps the cascade default (JEF-272) —
	// older clients must not silently turn it off.
	if got.MaxEscalations != service.DefaultConfidenceReview.MaxEscalations {
		t.Fatalf("max_escalations = %d, want default %d", got.MaxEscalations, service.DefaultConfidenceReview.MaxEscalations)
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

// max_escalations (JEF-272) is bounded to 0-3; 0 round-trips as a real value
// (cascade off), not as "unset".
func TestConfidenceReviewSettingsMaxEscalationsBounds(t *testing.T) {
	rememberSettings(t)
	testutil.Call(t, testHandler.PutConfidenceReviewSettings, newRequest(http.MethodPut, "/api/confidence-review-settings", map[string]any{"enabled": true, "threshold": 0.5, "max_escalations": 4})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.PutConfidenceReviewSettings, newRequest(http.MethodPut, "/api/confidence-review-settings", map[string]any{"enabled": true, "threshold": 0.5, "max_escalations": -1})).Want(http.StatusBadRequest)

	testutil.Call(t, testHandler.PutConfidenceReviewSettings, newRequest(http.MethodPut, "/api/confidence-review-settings", map[string]any{"enabled": true, "threshold": 0.5, "max_escalations": 0})).Want(http.StatusOK)
	var got service.ConfidenceReview
	testutil.Call(t, testHandler.GetConfidenceReviewSettings, newRequest(http.MethodGet, "/api/confidence-review-settings", nil)).Want(http.StatusOK).JSON(&got)
	if got.MaxEscalations != 0 {
		t.Fatalf("max_escalations = %d, want explicit 0", got.MaxEscalations)
	}

	testutil.Call(t, testHandler.PutConfidenceReviewSettings, newRequest(http.MethodPut, "/api/confidence-review-settings", map[string]any{"enabled": true, "threshold": 0.5, "max_escalations": 3})).Want(http.StatusOK)
	got = service.ConfidenceReview{}
	testutil.Call(t, testHandler.GetConfidenceReviewSettings, newRequest(http.MethodGet, "/api/confidence-review-settings", nil)).Want(http.StatusOK).JSON(&got)
	if got.MaxEscalations != 3 {
		t.Fatalf("max_escalations = %d, want 3", got.MaxEscalations)
	}
}

func TestTaskToResponseExposesEscalation(t *testing.T) {
	contextJSON := []byte(`{"head_sha":"abc123","escalation":{"from_task_id":"8b2f1c00-0000-7000-8000-000000000001","reason":"below_threshold","attempt":1,"from_runtime_id":"8b2f1c00-0000-7000-8000-000000000002"}}`)
	with := taskToResponse(db.AgentTaskQueue{Context: contextJSON}, "ws")
	var decoded struct {
		Attempt       int    `json:"attempt"`
		Reason        string `json:"reason"`
		FromRuntimeID string `json:"from_runtime_id"`
	}
	if err := json.Unmarshal(with.Escalation, &decoded); err != nil {
		t.Fatalf("escalation not exposed: %v", err)
	}
	if decoded.Attempt != 1 || decoded.Reason != "below_threshold" || decoded.FromRuntimeID == "" {
		t.Errorf("escalation = %+v", decoded)
	}

	// Context without an escalation block, and no context at all, expose
	// nothing; omitempty keeps the key off the wire.
	for name, row := range map[string]db.AgentTaskQueue{
		"no escalation key": {Context: []byte(`{"head_sha":"abc123"}`)},
		"no context":        {},
	} {
		resp := taskToResponse(row, "ws")
		if len(resp.Escalation) != 0 {
			t.Errorf("%s: escalation = %s, want empty", name, resp.Escalation)
		}
		wire, err := json.Marshal(resp)
		if err != nil {
			t.Fatalf("%s: marshal response: %v", name, err)
		}
		var asMap map[string]any
		if err := json.Unmarshal(wire, &asMap); err != nil {
			t.Fatalf("%s: unmarshal response: %v", name, err)
		}
		if _, present := asMap["escalation"]; present {
			t.Errorf("%s: escalation key present on a non-escalated task", name)
		}
	}
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
