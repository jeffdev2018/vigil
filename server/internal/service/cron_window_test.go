package service

import (
	"testing"
	"time"
)

func TestWindowOffsetIsDeterministicAndBounded(t *testing.T) {
	occ := time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)
	window := 2 * time.Hour
	a := WindowOffset("trig-1", occ, window)
	b := WindowOffset("trig-1", occ, window)
	if a != b {
		t.Fatalf("offset not deterministic: %v vs %v", a, b)
	}
	if a < 0 || a >= window {
		t.Fatalf("offset %v outside [0, %v)", a, window)
	}
	if WindowOffset("trig-2", occ, window) == a && WindowOffset("trig-1", occ.Add(24*time.Hour), window) == a {
		t.Fatal("offset should vary with trigger and day (both equal is vanishingly unlikely)")
	}
	if WindowOffset("trig-1", occ, 0) != 0 {
		t.Fatal("zero window must not shift")
	}
}

func TestNextWindowedOccurrencesShiftInsideTheBand(t *testing.T) {
	// Daily at 08:00 UTC, spread over two hours.
	after := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	until := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	occs, err := NextWindowedOccurrencesUTC("0 8 * * *", "UTC", 2*time.Hour, "trig-1", after, until)
	if err != nil {
		t.Fatalf("occurrences: %v", err)
	}
	if len(occs) != 2 {
		t.Fatalf("got %d occurrences, want 2: %v", len(occs), occs)
	}
	for _, o := range occs {
		bandStart := time.Date(o.Year(), o.Month(), o.Day(), 8, 0, 0, 0, time.UTC)
		if o.Before(bandStart) || !o.Before(bandStart.Add(2*time.Hour)) {
			t.Fatalf("occurrence %v outside its 08:00–10:00 band", o)
		}
	}
	// The next-after helper agrees with the enumeration.
	next, err := NextWindowedOccurrenceAfterUTC("0 8 * * *", "UTC", 2*time.Hour, "trig-1", after)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if !next.Equal(occs[0]) {
		t.Fatalf("next-after %v != first enumerated %v", next, occs[0])
	}
	// A band already started but whose slot is still ahead is not skipped.
	late := occs[0].Add(-time.Minute)
	got, err := NextWindowedOccurrenceAfterUTC("0 8 * * *", "UTC", 2*time.Hour, "trig-1", late)
	if err != nil || !got.Equal(occs[0]) {
		t.Fatalf("next-after inside band = %v (%v), want %v", got, err, occs[0])
	}
}
