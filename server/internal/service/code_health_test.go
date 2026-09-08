package service

import (
	"testing"
	"time"
)

// Canonical coverage of the code health settings (K22): what a workspace reads
// when it never configured this, what a submitted blob is allowed to say, and
// when the cron is due. The handler suite covers the endpoints and the scan.

func TestCodeHealthFromSettingsFallsBackAndClamps(t *testing.T) {
	cases := []struct {
		name string
		blob string
		want CodeHealthSettings
	}{
		{"empty blob", "", CodeHealthDefaults()},
		{"no key", `{"workflow_limits":{"max_legs":3}}`, CodeHealthDefaults()},
		{"unparseable", `not json`, CodeHealthDefaults()},
		{"out of range values fall back", `{"code_health":{"max_issues_per_scan":9000,"min_confidence":-5}}`, CodeHealthDefaults()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CodeHealthFromSettings([]byte(tc.blob))
			if got.Enabled != tc.want.Enabled || got.Cron != tc.want.Cron || got.Timezone != tc.want.Timezone ||
				got.MaxIssuesPerScan != tc.want.MaxIssuesPerScan || got.MinConfidence != tc.want.MinConfidence {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}

	got := CodeHealthFromSettings([]byte(`{"code_health":{"enabled":true,"cron":"0 6 * * *","timezone":"Europe/Paris","agent_id":"a","project_id":"p","max_issues_per_scan":12,"min_confidence":0}}`))
	if !got.Enabled || got.Cron != "0 6 * * *" || got.Timezone != "Europe/Paris" || got.AgentID != "a" ||
		got.ProjectID != "p" || got.MaxIssuesPerScan != 12 || got.MinConfidence != 0 {
		t.Fatalf("a configured workspace reads back what it wrote: %+v", got)
	}
}

func TestValidateCodeHealthSettings(t *testing.T) {
	ok := CodeHealthDefaults()
	ok.Enabled = true
	ok.AgentID = "agent"
	if err := ValidateCodeHealthSettings(ok); err != nil {
		t.Fatalf("the defaults plus an agent are valid: %v", err)
	}
	// Disabled needs no agent: turning the autopilot off must always be
	// possible, whatever else the form holds.
	off := CodeHealthDefaults()
	if err := ValidateCodeHealthSettings(off); err != nil {
		t.Fatalf("disabled is valid without an agent: %v", err)
	}

	for _, tc := range []struct {
		name  string
		mutar func(*CodeHealthSettings)
	}{
		{"no agent while enabled", func(s *CodeHealthSettings) { s.Enabled = true; s.AgentID = "" }},
		{"cron", func(s *CodeHealthSettings) { s.Cron = "every monday please" }},
		{"timezone", func(s *CodeHealthSettings) { s.Timezone = "Mars/Olympus" }},
		{"max issues too low", func(s *CodeHealthSettings) { s.MaxIssuesPerScan = 0 }},
		{"max issues too high", func(s *CodeHealthSettings) { s.MaxIssuesPerScan = CodeHealthMaxMaxIssues + 1 }},
		{"confidence", func(s *CodeHealthSettings) { s.MinConfidence = 101 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := ok
			tc.mutar(&s)
			if err := ValidateCodeHealthSettings(s); err == nil {
				t.Fatalf("%s must be refused: %+v", tc.name, s)
			}
		})
	}
}

func TestCodeHealthDue(t *testing.T) {
	// Monday 03:00 UTC, weekly.
	cfg := CodeHealthDefaults()
	cfg.Enabled = true
	cfg.AgentID = "agent"
	anchor := time.Date(2026, 1, 5, 4, 0, 0, 0, time.UTC) // Monday, just after the slot

	if CodeHealthDue(cfg, anchor, anchor.Add(24*time.Hour)) {
		t.Fatal("a day later is not a week later")
	}
	if !CodeHealthDue(cfg, anchor, anchor.Add(8*24*time.Hour)) {
		t.Fatal("the next Monday is due")
	}

	off := cfg
	off.Enabled = false
	if CodeHealthDue(off, anchor, anchor.Add(365*24*time.Hour)) {
		t.Fatal("a disabled autopilot is never due")
	}
	// No anchor: nothing to measure the schedule from, so it waits.
	if CodeHealthDue(cfg, time.Time{}, anchor.Add(365*24*time.Hour)) {
		t.Fatal("an unanchored schedule is not due")
	}
	// Fails closed on an expression the settings endpoint would have refused.
	broken := cfg
	broken.Cron = "@@@"
	if CodeHealthDue(broken, anchor, anchor.Add(365*24*time.Hour)) {
		t.Fatal("an unparseable cron never fires")
	}
}
