package service

import (
	"strings"
	"testing"
	"time"
)

func TestParseFollowupWhenBounds(t *testing.T) {
	now := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	if at, err := ParseFollowupWhen("+90", now); err != nil || !at.Equal(now.Add(90*time.Minute)) {
		t.Errorf("+90 = %v, %v", at, err)
	}
	if at, err := ParseFollowupWhen("2026-09-12T09:00:00+02:00", now); err != nil || at.UTC().Hour() != 7 {
		t.Errorf("rfc3339 = %v, %v", at, err)
	}
	for _, bad := range []string{"", "+0", "+abc", "tomorrow", now.Add(30 * time.Second).Format(time.RFC3339), now.Add(31 * 24 * time.Hour).Format(time.RFC3339)} {
		if _, err := ParseFollowupWhen(bad, now); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestNormalizeFollowupNoteAndSettings(t *testing.T) {
	if got := NormalizeFollowupNote("  "); got != FollowupDefaultNote {
		t.Errorf("empty note = %q", got)
	}
	if got := NormalizeFollowupNote(strings.Repeat("é", 600)); len([]rune(got)) != FollowupNoteMaxRunes {
		t.Errorf("long note kept %d runes", len([]rune(got)))
	}
	if got := FollowupNoteFromSummary(FollowupSummaryPrefix + "call back"); got != "call back" {
		t.Errorf("summary round trip = %q", got)
	}
	def := FollowupSettingsFrom(nil)
	if def.MaxPerAgentPerDay != 20 || def.MaxPerWorkspacePerDay != 200 {
		t.Errorf("defaults = %+v", def)
	}
	got := FollowupSettingsFrom([]byte(`{"followups":{"max_per_agent_per_day":5,"max_per_workspace_per_day":5000}}`))
	if got.MaxPerAgentPerDay != 5 || got.MaxPerWorkspacePerDay != 200 {
		t.Errorf("configured = %+v, want 5 and the default for the out-of-range value", got)
	}
}
