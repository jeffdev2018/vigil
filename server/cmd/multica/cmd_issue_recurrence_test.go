package main

// `multica issue recurrence` (JEF-375) — the three things the CLI owns: the
// preset flags that become a cron, the body it PUTs, and the two refusals it
// has to turn into a sentence (403 a run may not write, 404 no rule).

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestBuildRecurrenceCron_PresetsBecomeCron(t *testing.T) {
	cases := []struct {
		name                             string
		daily, weekdays, weekly, monthly string
		want                             string
	}{
		{name: "no preset leaves the cron to --cron"},
		{name: "daily", daily: "09:00", want: "0 9 * * *"},
		{name: "daily keeps the minutes", daily: "18:45", want: "45 18 * * *"},
		{name: "weekdays", weekdays: "09:00", want: "0 9 * * 1-5"},
		{name: "weekly monday", weekly: "MON 09:00", want: "0 9 * * 1"},
		{name: "weekly is case and name insensitive", weekly: "sunday 07:30", want: "30 7 * * 0"},
		{name: "monthly first", monthly: "1 09:00", want: "0 9 1 * *"},
		{name: "monthly accepts a comma", monthly: "15, 23:59", want: "59 23 15 * *"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := buildRecurrenceCron(c.daily, c.weekdays, c.weekly, c.monthly)
			if err != nil {
				t.Fatalf("buildRecurrenceCron: %v", err)
			}
			if got != c.want {
				t.Errorf("cron = %q, want %q", got, c.want)
			}
		})
	}
}

func TestBuildRecurrenceCron_RefusesAmbiguousOrMalformedPresets(t *testing.T) {
	cases := []struct {
		name                             string
		daily, weekdays, weekly, monthly string
		wantIn                           string
	}{
		{name: "two presets", daily: "09:00", weekdays: "09:00", wantIn: "--daily and --weekdays"},
		{name: "hour out of range", daily: "25:00", wantIn: "between 00 and 23"},
		{name: "not a time", daily: "9h", wantIn: "HH:MM"},
		{name: "not a weekday", weekly: "MONDAYISH 09:00", wantIn: "not a weekday"},
		{name: "weekly missing the clock", weekly: "MON", wantIn: "two values"},
		{name: "day of month out of range", monthly: "32 09:00", wantIn: "between 1 and 31"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := buildRecurrenceCron(c.daily, c.weekdays, c.weekly, c.monthly)
			if err == nil {
				t.Fatalf("expected a refusal")
			}
			if !strings.Contains(err.Error(), c.wantIn) {
				t.Errorf("error %q must name %q", err.Error(), c.wantIn)
			}
		})
	}
}

func newRecurrenceTestCmd(use string) *cobra.Command {
	cmd := &cobra.Command{Use: use}
	cmd.Flags().String("cron", "", "")
	cmd.Flags().String("daily", "", "")
	cmd.Flags().String("weekdays", "", "")
	cmd.Flags().String("weekly", "", "")
	cmd.Flags().String("monthly", "", "")
	cmd.Flags().String("timezone", "", "")
	cmd.Flags().String("mode", "", "")
	cmd.Flags().Bool("disabled", false, "")
	cmd.Flags().String("output", "table", "")
	cmd.Flags().Bool("full-id", false, "")
	return cmd
}

// recurrenceCLIServer answers the issue resolver and the recurrence endpoint.
// status drives what the recurrence call replies.
func recurrenceCLIServer(t *testing.T, captured *map[string]any, capturedPath, capturedMethod *string, status int, errBody string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/recurrence"):
			*capturedPath = r.URL.Path
			*capturedMethod = r.Method
			if r.Method == http.MethodPut {
				if err := json.NewDecoder(r.Body).Decode(captured); err != nil {
					t.Errorf("decode recurrence body: %v", err)
				}
			}
			if status != http.StatusOK && status != http.StatusNoContent {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(errBody))
				return
			}
			if r.Method == http.MethodDelete {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"recurrence": map[string]any{
					"id": "rec-1", "issue_id": "issue-1", "cron_expression": "0 9 * * 1", "timezone": "Europe/Paris",
					"mode": "schedule", "enabled": true, "next_run_at": "2026-09-14T07:00:00Z",
					"last_occurrence_id": nil, "occurrence_count": 2,
				},
				"source": map[string]any{"id": "issue-1", "identifier": "MUL-1", "title": "Weekly review"},
				"occurrences": []map[string]any{
					{"id": "issue-2", "identifier": "MUL-7", "title": "Weekly review", "status": "todo", "created_at": "2026-09-07T07:00:00Z", "due_date": "2026-09-08"},
					{"id": "issue-1", "identifier": "MUL-1", "title": "Weekly review", "status": "done", "created_at": "2026-08-31T07:00:00Z", "due_date": nil},
				},
				"next_runs": []string{"2026-09-14T07:00:00Z", "2026-09-21T07:00:00Z", "2026-09-28T07:00:00Z"},
			})
		case strings.HasPrefix(r.URL.Path, "/api/issues/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "issue-1", "identifier": "MUL-1", "title": "Weekly review", "status": "todo", "priority": "none",
			})
		default:
			http.NotFound(w, r)
		}
	}))
}

func recurrenceTestEnv(t *testing.T, url string) {
	t.Helper()
	t.Setenv("MULTICA_SERVER_URL", url)
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")
}

func TestRunIssueRecurrenceSet_SendsCronTimezoneModeAndEnabled(t *testing.T) {
	var body map[string]any
	var path, method string
	srv := recurrenceCLIServer(t, &body, &path, &method, http.StatusOK, "")
	defer srv.Close()
	recurrenceTestEnv(t, srv.URL)

	cmd := newRecurrenceTestCmd("set")
	_ = cmd.Flags().Set("cron", "0 9 * * 1")
	_ = cmd.Flags().Set("timezone", "Europe/Paris")
	if err := runIssueRecurrenceSet(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueRecurrenceSet: %v", err)
	}
	if method != http.MethodPut || path != "/api/issues/issue-1/recurrence" {
		t.Errorf("%s %s, want PUT /api/issues/issue-1/recurrence", method, path)
	}
	if got := body["cron_expression"]; got != "0 9 * * 1" {
		t.Errorf("cron_expression = %#v", got)
	}
	if got := body["timezone"]; got != "Europe/Paris" {
		t.Errorf("timezone = %#v", got)
	}
	if got := body["enabled"]; got != true {
		t.Errorf("enabled = %#v, want true: a set without --disabled arms the rule", got)
	}
	// The server defaults mode and timezone; sending an empty mode would be
	// claiming a value the caller never chose.
	if _, ok := body["mode"]; ok {
		t.Errorf("mode was sent without --mode: %#v", body)
	}
}

func TestRunIssueRecurrenceSet_PresetBuildsTheCronAndCronWins(t *testing.T) {
	var body map[string]any
	var path, method string
	srv := recurrenceCLIServer(t, &body, &path, &method, http.StatusOK, "")
	defer srv.Close()
	recurrenceTestEnv(t, srv.URL)

	cmd := newRecurrenceTestCmd("set")
	_ = cmd.Flags().Set("weekdays", "09:00")
	if err := runIssueRecurrenceSet(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueRecurrenceSet: %v", err)
	}
	if got := body["cron_expression"]; got != "0 9 * * 1-5" {
		t.Errorf("cron_expression = %#v, want the preset's cron", got)
	}

	withBoth := newRecurrenceTestCmd("set")
	_ = withBoth.Flags().Set("daily", "09:00")
	_ = withBoth.Flags().Set("cron", "*/5 * * * *")
	if err := runIssueRecurrenceSet(withBoth, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueRecurrenceSet: %v", err)
	}
	if got := body["cron_expression"]; got != "*/5 * * * *" {
		t.Errorf("cron_expression = %#v, want the explicit --cron to win over the preset", got)
	}
}

func TestRunIssueRecurrenceSet_OnCloseNeedsNoCron(t *testing.T) {
	var body map[string]any
	var path, method string
	srv := recurrenceCLIServer(t, &body, &path, &method, http.StatusOK, "")
	defer srv.Close()
	recurrenceTestEnv(t, srv.URL)

	cmd := newRecurrenceTestCmd("set")
	_ = cmd.Flags().Set("mode", "on_close")
	_ = cmd.Flags().Set("disabled", "true")
	if err := runIssueRecurrenceSet(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueRecurrenceSet: %v", err)
	}
	if got := body["mode"]; got != "on_close" {
		t.Errorf("mode = %#v", got)
	}
	if _, ok := body["cron_expression"]; ok {
		t.Errorf("cron_expression was sent for an on_close rule: %#v", body)
	}
	if got := body["enabled"]; got != false {
		t.Errorf("enabled = %#v, want false with --disabled", got)
	}
}

func TestRunIssueRecurrenceSet_RefusesAScheduleWithNoCronBeforeCallingTheServer(t *testing.T) {
	t.Setenv("MULTICA_SERVER_URL", "http://127.0.0.1:1")
	t.Setenv("MULTICA_WORKSPACE_ID", "ws-1")
	t.Setenv("MULTICA_TOKEN", "test-token")

	err := runIssueRecurrenceSet(newRecurrenceTestCmd("set"), []string{"MUL-1"})
	if err == nil || !strings.Contains(err.Error(), "--cron") {
		t.Fatalf("error = %v, want a refusal naming --cron and the presets", err)
	}
}

func TestRunIssueRecurrenceSet_PrintsTheMemberOnlyRefusalOn403(t *testing.T) {
	var body map[string]any
	var path, method string
	srv := recurrenceCLIServer(t, &body, &path, &method, http.StatusForbidden,
		`{"error":"only a member sets a recurrence"}`)
	defer srv.Close()
	recurrenceTestEnv(t, srv.URL)

	cmd := newRecurrenceTestCmd("set")
	_ = cmd.Flags().Set("daily", "09:00")
	err := runIssueRecurrenceSet(cmd, []string{"MUL-1"})
	if err == nil {
		t.Fatalf("expected the member-only refusal to surface as an error")
	}
	if !strings.Contains(err.Error(), "only a member sets a recurrence") {
		t.Errorf("error %q must carry the server's sentence", err.Error())
	}
	if !strings.Contains(err.Error(), "ask a member") {
		t.Errorf("error %q must tell a run what to do instead", err.Error())
	}
}

func TestRunIssueRecurrenceShow_PrintsTheRuleAndSaysWhenThereIsNone(t *testing.T) {
	var body map[string]any
	var path, method string
	srv := recurrenceCLIServer(t, &body, &path, &method, http.StatusOK, "")
	defer srv.Close()
	recurrenceTestEnv(t, srv.URL)

	cmd := newRecurrenceTestCmd("show")
	if err := runIssueRecurrenceShow(cmd, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueRecurrenceShow: %v", err)
	}
	if method != http.MethodGet || path != "/api/issues/issue-1/recurrence" {
		t.Errorf("%s %s, want GET /api/issues/issue-1/recurrence", method, path)
	}

	missing := recurrenceCLIServer(t, &body, &path, &method, http.StatusNotFound,
		`{"error":"this issue does not recur"}`)
	defer missing.Close()
	recurrenceTestEnv(t, missing.URL)
	err := runIssueRecurrenceShow(newRecurrenceTestCmd("show"), []string{"MUL-1"})
	if err == nil || !strings.Contains(err.Error(), "this issue does not recur") {
		t.Fatalf("error = %v, want the server's 404 sentence", err)
	}
}

func TestRunIssueRecurrenceClear_DeletesTheRule(t *testing.T) {
	var body map[string]any
	var path, method string
	srv := recurrenceCLIServer(t, &body, &path, &method, http.StatusNoContent, "")
	defer srv.Close()
	recurrenceTestEnv(t, srv.URL)

	if err := runIssueRecurrenceClear(&cobra.Command{Use: "clear"}, []string{"MUL-1"}); err != nil {
		t.Fatalf("runIssueRecurrenceClear: %v", err)
	}
	if method != http.MethodDelete || path != "/api/issues/issue-1/recurrence" {
		t.Errorf("%s %s, want DELETE /api/issues/issue-1/recurrence", method, path)
	}
}
