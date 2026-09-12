package service

import (
	"testing"
	"time"
)

func TestResetChecklistUnticksEveryBox(t *testing.T) {
	in := "Steps:\n- [x] export\n  * [X] nested\n- [ ] untouched\n+ [x] plus\nnot - [x] a box"
	want := "Steps:\n- [ ] export\n  * [ ] nested\n- [ ] untouched\n+ [ ] plus\nnot - [x] a box"
	if got := ResetChecklist(in); got != want {
		t.Errorf("ResetChecklist =\n%s\nwant\n%s", got, want)
	}
}

func TestNextRecurrenceRun(t *testing.T) {
	after := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	next, err := NextRecurrenceRun(RecurrenceModeSchedule, "0 9 * * 1", "Europe/Paris", after)
	if err != nil || !next.Valid || next.Time.UTC() != time.Date(2026, 9, 14, 7, 0, 0, 0, time.UTC) {
		t.Errorf("weekly monday 9 paris = %v %v", next.Time, err)
	}
	if next, err := NextRecurrenceRun(RecurrenceModeOnClose, "", "", after); err != nil || next.Valid {
		t.Errorf("on_close must have no next run: %v %v", next, err)
	}
	if _, err := NextRecurrenceRun(RecurrenceModeSchedule, "every monday", "UTC", after); err == nil {
		t.Error("invalid cron accepted")
	}
	if err := ValidateRecurrenceMode("weekly"); err == nil {
		t.Error("invalid mode accepted")
	}
}
