package handler

import (
	"math/big"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The canonical matrix for the burndown's past / today / future rules. It runs
// without a database on purpose: the rules are arithmetic over dates, and the
// handler test beside it only needs to prove the endpoint wires them up.

func day(s string) pgtype.Date {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return pgtype.Date{Time: t, Valid: true}
}

func bigInt(v int64) *big.Int { return big.NewInt(v) }

func num(v int64) pgtype.Numeric {
	return pgtype.Numeric{Int: bigInt(v), Valid: true}
}

func snapshot(date string, total, done int64) db.CycleSnapshot {
	return db.CycleSnapshot{
		SnapshotDate: day(date),
		TotalCount:   total,
		DoneCount:    done,
		TotalLoad:    num(total),
		DoneLoad:     num(done),
	}
}

func remaining(t *testing.T, d CycleBurndownDay) int64 {
	t.Helper()
	if d.RemainingCount == nil {
		t.Fatalf("day %s has no remaining_count", d.Date)
	}
	return *d.RemainingCount
}

func TestBuildCycleBurndownFillsEveryDay(t *testing.T) {
	cycle := db.Cycle{StartDate: day("2026-03-02"), EndDate: day("2026-03-06")}
	today := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)
	live := db.GetCycleStatsRow{TotalCount: 10, DoneCount: 6, TotalLoad: num(10), DoneLoad: num(6)}

	out := buildCycleBurndown(cycle, []db.CycleSnapshot{
		snapshot("2026-03-02", 10, 0),
		// 03-03 deliberately missing: it must carry 03-02 forward.
	}, live, today)

	wantDates := []string{"2026-03-02", "2026-03-03", "2026-03-04", "2026-03-05", "2026-03-06"}
	if len(out.Days) != len(wantDates) {
		t.Fatalf("days = %d, want %d", len(out.Days), len(wantDates))
	}
	for i, want := range wantDates {
		if out.Days[i].Date != want {
			t.Fatalf("day %d = %s, want %s", i, out.Days[i].Date, want)
		}
	}
	if got := remaining(t, out.Days[0]); got != 10 {
		t.Fatalf("snapshot day = %d, want 10", got)
	}
	if got := remaining(t, out.Days[1]); got != 10 {
		t.Fatalf("gap day = %d, want the carried 10 — a zero would read as 'all done'", got)
	}
	if got := remaining(t, out.Days[2]); got != 4 {
		t.Fatalf("today = %d, want the live 4", got)
	}
	for _, i := range []int{3, 4} {
		if out.Days[i].RemainingCount != nil {
			t.Fatalf("future day %s has a remaining value; nothing has happened yet", out.Days[i].Date)
		}
	}
	if out.ApproximateBefore != nil {
		t.Fatalf("approximate_before = %v, want absent: history starts on day one", *out.ApproximateBefore)
	}
}

func TestBuildCycleBurndownIdealRunsFullToZero(t *testing.T) {
	cycle := db.Cycle{StartDate: day("2026-03-02"), EndDate: day("2026-03-06")}
	out := buildCycleBurndown(cycle, nil,
		db.GetCycleStatsRow{TotalCount: 8, TotalLoad: num(8)},
		time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC))

	if out.Days[0].IdealCount != 8 {
		t.Fatalf("ideal day one = %v, want the full 8", out.Days[0].IdealCount)
	}
	if out.Days[len(out.Days)-1].IdealCount != 0 {
		t.Fatalf("ideal last day = %v, want 0", out.Days[len(out.Days)-1].IdealCount)
	}
	for i := 1; i < len(out.Days); i++ {
		if out.Days[i].IdealCount > out.Days[i-1].IdealCount {
			t.Fatalf("ideal line rose at %s", out.Days[i].Date)
		}
	}
}

func TestBuildCycleBurndownFlagsDaysBeforeItsFirstSnapshot(t *testing.T) {
	cycle := db.Cycle{StartDate: day("2026-03-01"), EndDate: day("2026-03-05")}
	today := time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)

	// History starts on 03-03: the two days before it are flat-filled from it.
	out := buildCycleBurndown(cycle, []db.CycleSnapshot{snapshot("2026-03-03", 6, 2)},
		db.GetCycleStatsRow{TotalCount: 6, DoneCount: 4, TotalLoad: num(6), DoneLoad: num(4)}, today)

	if out.ApproximateBefore == nil || *out.ApproximateBefore != "2026-03-03" {
		t.Fatalf("approximate_before = %v, want 2026-03-03", out.ApproximateBefore)
	}
	for _, i := range []int{0, 1, 2} {
		if got := remaining(t, out.Days[i]); got != 4 {
			t.Fatalf("day %s = %d, want the first snapshot's 4", out.Days[i].Date, got)
		}
	}

	// With no snapshots at all, every past day is approximate from today.
	none := buildCycleBurndown(cycle, nil,
		db.GetCycleStatsRow{TotalCount: 6, DoneCount: 1, TotalLoad: num(6), DoneLoad: num(1)}, today)
	if none.ApproximateBefore == nil || *none.ApproximateBefore != "2026-03-04" {
		t.Fatalf("approximate_before without history = %v, want today", none.ApproximateBefore)
	}
	if got := remaining(t, none.Days[0]); got != 5 {
		t.Fatalf("flat-filled day = %d, want the live 5", got)
	}
}

func TestBuildCycleBurndownHandlesASingleDayCycle(t *testing.T) {
	cycle := db.Cycle{StartDate: day("2026-03-02"), EndDate: day("2026-03-02")}
	out := buildCycleBurndown(cycle, nil,
		db.GetCycleStatsRow{TotalCount: 3, TotalLoad: num(3)},
		time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC))
	if len(out.Days) != 1 {
		t.Fatalf("days = %d, want 1", len(out.Days))
	}
	if out.Days[0].IdealCount != 0 {
		t.Fatalf("ideal = %v, want 0: the only day is also the last one", out.Days[0].IdealCount)
	}
	if got := remaining(t, out.Days[0]); got != 3 {
		t.Fatalf("remaining = %d, want 3", got)
	}
}

func TestNumericToFloatHandlesScaleAndNull(t *testing.T) {
	cases := []struct {
		name string
		in   pgtype.Numeric
		want float64
	}{
		{"unset", pgtype.Numeric{}, 0},
		{"nan", pgtype.Numeric{NaN: true, Valid: true}, 0},
		{"integer", num(12), 12},
		{"scaled", pgtype.Numeric{Int: bigInt(125), Exp: -1, Valid: true}, 12.5},
		{"positive exponent", pgtype.Numeric{Int: bigInt(12), Exp: 2, Valid: true}, 1200},
	}
	for _, tc := range cases {
		if got := numericToFloat(tc.in); got != tc.want {
			t.Errorf("%s: numericToFloat = %v, want %v", tc.name, got, tc.want)
		}
	}
}
