package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const (
	dryRunAutopilotID = "11111111-1111-1111-1111-111111111111"
	dryRunTriggerID   = "22222222-2222-2222-2222-222222222222"
)

func newTriggerDryRunTestCmd(output string) *cobra.Command {
	cmd := &cobra.Command{Use: "trigger-dry-run"}
	cmd.Flags().String("payload-file", "", "")
	cmd.Flags().StringArray("header", nil, "")
	cmd.Flags().String("output", output, "")
	return cmd
}

// dryRunTestServer answers the autopilot detail (for id resolution) and the
// dry-run path, recording what the dry-run request carried.
func dryRunTestServer(t *testing.T, respond func(w http.ResponseWriter, r *http.Request), seen *map[string]any, method *string) *httptest.Server {
	t.Helper()
	dryRunPath := "/api/autopilots/" + dryRunAutopilotID + "/triggers/" + dryRunTriggerID + "/dry-run"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/autopilots/" + dryRunAutopilotID:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"autopilot": map[string]any{"id": dryRunAutopilotID},
				"triggers":  []map[string]any{{"id": dryRunTriggerID, "kind": "webhook"}},
			})
		case dryRunPath:
			*method = r.Method
			body := map[string]any{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			*seen = body
			respond(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	return srv
}

// --payload-file means "webhook": the sample event is POSTed and the verdict
// printed. Without it the same path is a GET schedule preview — one command,
// two verbs, chosen by whether the caller has an event to send.
func TestTriggerDryRunPostsTheSampleEvent(t *testing.T) {
	var seen map[string]any
	var method string
	dryRunTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"would_run":       false,
			"reason_code":     "criteria_not_matched",
			"explanation":     "staging, and it succeeded",
			"matched_filters": []map[string]any{{"event": "deploy"}},
			"event":           "deploy.finished",
		})
	}, &seen, &method)

	payload := filepath.Join(t.TempDir(), "event.json")
	if err := os.WriteFile(payload, []byte(`{"environment":"staging"}`), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	cmd := newTriggerDryRunTestCmd("table")
	_ = cmd.Flags().Set("payload-file", payload)
	_ = cmd.Flags().Set("header", "X-GitHub-Event=deployment")

	out, err := captureStdout(t, func() error {
		return runAutopilotTriggerDryRun(cmd, []string{dryRunAutopilotID, dryRunTriggerID})
	})
	if err != nil {
		t.Fatalf("runAutopilotTriggerDryRun: %v", err)
	}
	if method != http.MethodPost {
		t.Fatalf("method = %s, want POST for a payload dry-run", method)
	}
	if got, _ := seen["payload"].(map[string]any); got["environment"] != "staging" {
		t.Fatalf("payload = %#v, want the file's contents sent verbatim", seen["payload"])
	}
	headers, _ := seen["headers"].(map[string]any)
	if headers["X-GitHub-Event"] != "deployment" {
		t.Fatalf("headers = %#v, want the --header pair", seen["headers"])
	}
	for _, want := range []string{"would NOT run", "deploy.finished", "criteria_not_matched", "staging, and it succeeded"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestTriggerDryRunPreviewsASchedule(t *testing.T) {
	var seen map[string]any
	var method string
	dryRunTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"next_runs":      []string{"2126-07-14T08:30:00Z", "2126-07-15T08:10:00Z"},
			"would_run":      false,
			"reason_code":    "autopilot_paused",
			"window_minutes": 120,
		})
	}, &seen, &method)

	out, err := captureStdout(t, func() error {
		return runAutopilotTriggerDryRun(newTriggerDryRunTestCmd("table"), []string{dryRunAutopilotID, dryRunTriggerID})
	})
	if err != nil {
		t.Fatalf("runAutopilotTriggerDryRun: %v", err)
	}
	if method != http.MethodGet {
		t.Fatalf("method = %s, want GET when no payload file is given", method)
	}
	for _, want := range []string{"autopilot_paused", "2126-07-14T08:30:00Z", "2126-07-15T08:10:00Z"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// A typo in the payload file is reported against the file, not relayed as a
// 400 the caller then has to correlate back to it.
func TestTriggerDryRunRejectsUnparseablePayloadLocally(t *testing.T) {
	var seen map[string]any
	var method string
	dryRunTestServer(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("server must not be called for an unparseable payload file")
	}, &seen, &method)

	payload := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(payload, []byte("{oops"), 0o600); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	cmd := newTriggerDryRunTestCmd("json")
	_ = cmd.Flags().Set("payload-file", payload)

	err := runAutopilotTriggerDryRun(cmd, []string{dryRunAutopilotID, dryRunTriggerID})
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Fatalf("error = %v, want a local JSON complaint naming the file", err)
	}
}

func TestTriggerDryRunRejectsHeadersWithoutAPayload(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:1")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
	cmd := newTriggerDryRunTestCmd("json")
	_ = cmd.Flags().Set("header", "X-Event-Type=deploy")

	err := runAutopilotTriggerDryRun(cmd, []string{dryRunAutopilotID, dryRunTriggerID})
	if err == nil || !strings.Contains(err.Error(), "--header is only meaningful") {
		t.Fatalf("error = %v, want the header/payload pairing complaint", err)
	}
}

func newTriggerAddTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "trigger-add"}
	cmd.Flags().String("kind", "schedule", "")
	cmd.Flags().String("cron", "", "")
	cmd.Flags().String("timezone", "", "")
	cmd.Flags().String("label", "", "")
	cmd.Flags().Int("window-minutes", 0, "")
	cmd.Flags().String("event-match-criteria", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

func newTriggerUpdateTestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "trigger-update"}
	cmd.Flags().Bool("enabled", true, "")
	cmd.Flags().String("cron", "", "")
	cmd.Flags().String("timezone", "", "")
	cmd.Flags().String("label", "", "")
	cmd.Flags().Int("window-minutes", 0, "")
	cmd.Flags().String("event-match-criteria", "", "")
	cmd.Flags().String("output", "json", "")
	return cmd
}

// triggerWriteTestServer answers the detail lookup and records the body of the
// trigger create/update request.
func triggerWriteTestServer(t *testing.T, seen *map[string]any) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/autopilots/"+dryRunAutopilotID && r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"autopilot": map[string]any{"id": dryRunAutopilotID},
				"triggers":  []map[string]any{{"id": dryRunTriggerID, "kind": "schedule"}},
			})
			return
		}
		body := map[string]any{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		*seen = body
		_ = json.NewEncoder(w).Encode(map[string]any{"id": dryRunTriggerID, "kind": "schedule"})
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MULTICA_SERVER_URL", srv.URL)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "mat_test-token")
}

// The firing band and the natural-language routing rule were UI-only: a
// trigger created from the CLI could not have either.
func TestTriggerAddSendsWindowAndCriteria(t *testing.T) {
	var seen map[string]any
	triggerWriteTestServer(t, &seen)

	cmd := newTriggerAddTestCmd()
	_ = cmd.Flags().Set("cron", "0 8 * * *")
	_ = cmd.Flags().Set("window-minutes", "120")
	if _, err := captureStdout(t, func() error {
		return runAutopilotTriggerAdd(cmd, []string{dryRunAutopilotID})
	}); err != nil {
		t.Fatalf("runAutopilotTriggerAdd: %v", err)
	}
	if seen["window_minutes"] != float64(120) {
		t.Fatalf("window_minutes = %#v, want 120", seen["window_minutes"])
	}

	seen = nil
	webhook := newTriggerAddTestCmd()
	_ = webhook.Flags().Set("kind", "webhook")
	_ = webhook.Flags().Set("event-match-criteria", "only production incidents")
	if _, err := captureStdout(t, func() error {
		return runAutopilotTriggerAdd(webhook, []string{dryRunAutopilotID})
	}); err != nil {
		t.Fatalf("runAutopilotTriggerAdd (webhook): %v", err)
	}
	if seen["event_match_criteria"] != "only production incidents" {
		t.Fatalf("event_match_criteria = %#v", seen["event_match_criteria"])
	}
}

// Each flag belongs to one trigger kind; sending it to the other kind is a
// 400 from the server, and the CLI can say so without the round-trip.
func TestTriggerAddRejectsFlagsForTheWrongKind(t *testing.T) {
	var seen map[string]any
	triggerWriteTestServer(t, &seen)

	webhook := newTriggerAddTestCmd()
	_ = webhook.Flags().Set("kind", "webhook")
	_ = webhook.Flags().Set("window-minutes", "30")
	if err := runAutopilotTriggerAdd(webhook, []string{dryRunAutopilotID}); err == nil ||
		!strings.Contains(err.Error(), "--window-minutes is only valid") {
		t.Fatalf("error = %v, want the window/kind complaint", err)
	}

	schedule := newTriggerAddTestCmd()
	_ = schedule.Flags().Set("cron", "0 8 * * *")
	_ = schedule.Flags().Set("event-match-criteria", "anything")
	if err := runAutopilotTriggerAdd(schedule, []string{dryRunAutopilotID}); err == nil ||
		!strings.Contains(err.Error(), "--event-match-criteria is only valid") {
		t.Fatalf("error = %v, want the criteria/kind complaint", err)
	}
}

// Presence, not emptiness, decides what is sent: an empty
// --event-match-criteria is how the rule is cleared.
func TestTriggerUpdateSendsOnlyChangedFields(t *testing.T) {
	var seen map[string]any
	triggerWriteTestServer(t, &seen)

	cmd := newTriggerUpdateTestCmd()
	_ = cmd.Flags().Set("window-minutes", "60")
	_ = cmd.Flags().Set("event-match-criteria", "")
	if _, err := captureStdout(t, func() error {
		return runAutopilotTriggerUpdate(cmd, []string{dryRunAutopilotID, dryRunTriggerID})
	}); err != nil {
		t.Fatalf("runAutopilotTriggerUpdate: %v", err)
	}
	if seen["window_minutes"] != float64(60) {
		t.Fatalf("window_minutes = %#v, want 60", seen["window_minutes"])
	}
	if v, ok := seen["event_match_criteria"]; !ok || v != "" {
		t.Fatalf("event_match_criteria = %#v (present=%v), want an explicit empty string", v, ok)
	}
	if _, ok := seen["label"]; ok {
		t.Fatalf("label must not be sent when the flag was never set: %#v", seen)
	}
}
