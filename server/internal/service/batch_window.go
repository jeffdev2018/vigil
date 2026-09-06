package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Off-peak batch lane (K45).
//
// Some autopilot work is not urgent: a nightly dependency sweep, a weekly
// report. This declares one window per workspace during which such work is
// stamped with the batch dispatch lane instead of the sync one, and claim
// ordering then serves it only after every synchronous task of the same
// agent/runtime. The trade the user accepts is "cheaper, arrives later".
//
// The whole configuration lives under workspace.settings.batch_window, so
// there is no table for it and it is purged with the workspace.
//
// Nothing here calls a provider batch API. The lane is a scheduling decision
// this platform enforces itself, plus an environment variable
// (MULTICA_DISPATCH_LANE) exported to the agent process so a runtime or CLI
// wrapper that owns a cheaper path can honour it.

// Dispatch lanes, mirroring agent_task_queue.dispatch_lane.
const (
	DispatchLaneSync  = "sync"
	DispatchLaneBatch = "batch"
)

// BatchWindowDefaultTimezone is what a workspace that never configured this
// reads as; the times stay empty so a disabled window is unambiguous.
const BatchWindowDefaultTimezone = "UTC"

// BatchWindow is the workspace's off-peak window. Start and End are local
// wall-clock "HH:MM" in Timezone; Start is inclusive, End is exclusive, and a
// window whose End is before its Start crosses midnight (22:00 → 06:00).
type BatchWindow struct {
	Enabled bool `json:"enabled"`
	// Start is the inclusive local start time, "HH:MM".
	Start string `json:"start_local_time"`
	// End is the exclusive local end time, "HH:MM".
	End string `json:"end_local_time"`
	// Timezone is an IANA name the window's wall clock is read in.
	Timezone string `json:"timezone"`
}

// BatchWindowDefaults is the disabled window: no off-peak lane, no constraint.
func BatchWindowDefaults() BatchWindow {
	return BatchWindow{Timezone: BatchWindowDefaultTimezone}
}

// BatchWindowFromSettings reads the window off a workspace settings blob.
// Anything missing or unparseable is the disabled window — in particular
// Enabled stays false, so a corrupt blob never starts deferring work.
func BatchWindowFromSettings(settings []byte) BatchWindow {
	out := BatchWindowDefaults()
	var s struct {
		Window *BatchWindow `json:"batch_window"`
	}
	if len(settings) == 0 || json.Unmarshal(settings, &s) != nil || s.Window == nil {
		return out
	}
	got := NormalizeBatchWindow(*s.Window)
	// A stored window that no longer validates (hand-edited blob, a timezone
	// dropped from the tzdata this binary carries) is read as disabled rather
	// than as a window covering all of time.
	if ValidateBatchWindow(got) != nil {
		return out
	}
	return got
}

// NormalizeBatchWindow trims the times and fills in the default timezone, so
// storage and comparison see one spelling.
func NormalizeBatchWindow(w BatchWindow) BatchWindow {
	w.Start = strings.TrimSpace(w.Start)
	w.End = strings.TrimSpace(w.End)
	w.Timezone = strings.TrimSpace(w.Timezone)
	if w.Timezone == "" {
		w.Timezone = BatchWindowDefaultTimezone
	}
	return w
}

// ValidateBatchWindow reports what is wrong with a submitted window, or nil.
//
// A disabled window is always storable, whatever its times: turning the
// feature off must never be blocked by the fields it stops reading.
func ValidateBatchWindow(w BatchWindow) error {
	if err := ValidateTimezone(w.Timezone); err != nil {
		return err
	}
	if !w.Enabled {
		return nil
	}
	start, err := parseWallClock(w.Start)
	if err != nil {
		return fmt.Errorf("start_local_time: %w", err)
	}
	end, err := parseWallClock(w.End)
	if err != nil {
		return fmt.Errorf("end_local_time: %w", err)
	}
	// Equal bounds are the one genuinely ambiguous case: read as an empty
	// window it silently disables the feature, read as a full day it defers
	// everything. Refuse instead of picking.
	if start == end {
		return fmt.Errorf("start_local_time and end_local_time must differ")
	}
	return nil
}

// InBatchWindow answers "is now inside the off-peak window?", reading now's
// wall clock in the window's timezone. A disabled or invalid window is never
// inside, so a workspace that configured nothing pays nothing.
func InBatchWindow(w BatchWindow, now time.Time) bool {
	if !w.Enabled {
		return false
	}
	if ValidateBatchWindow(w) != nil {
		return false
	}
	loc, err := time.LoadLocation(w.Timezone)
	if err != nil {
		return false
	}
	start, _ := parseWallClock(w.Start)
	end, _ := parseWallClock(w.End)
	local := now.In(loc)
	cur := local.Hour()*60 + local.Minute()
	if start < end {
		// Same-day window: [start, end).
		return cur >= start && cur < end
	}
	// Crosses midnight: [start, 24:00) ∪ [00:00, end).
	return cur >= start || cur < end
}

// parseWallClock reads "HH:MM" into minutes since local midnight. Deliberately
// strict — no seconds, no single-digit hour — so the stored form is the one
// an <input type="time"> produces and the one the UI renders back.
func parseWallClock(s string) (int, error) {
	h, m, ok := strings.Cut(s, ":")
	if !ok || len(h) != 2 || len(m) != 2 {
		return 0, fmt.Errorf("expected HH:MM, got %q", s)
	}
	hour, err := strconv.Atoi(h)
	if err != nil || hour < 0 || hour > 23 {
		return 0, fmt.Errorf("expected HH:MM, got %q", s)
	}
	minute, err := strconv.Atoi(m)
	if err != nil || minute < 0 || minute > 59 {
		return 0, fmt.Errorf("expected HH:MM, got %q", s)
	}
	return hour*60 + minute, nil
}
