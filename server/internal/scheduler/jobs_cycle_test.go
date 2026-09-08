package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"
)

func nowForTest() time.Time { return time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC) }

// The two cycle jobs (F29) are thin wrappers, so what is worth pinning is the
// contract the scheduler reads off them: a global scope, a name that cannot
// drift (it is the audit key), and a handler that reports rows rather than
// swallowing its callee's error.

func TestCycleJobsAreGlobalAndReportTheirWork(t *testing.T) {
	cases := []struct {
		name string
		spec JobSpec
	}{
		{JobNameCycleSnapshot, CycleSnapshotJob(func(context.Context) (int, error) { return 7, nil })},
		{JobNameCycleRollover, CycleRolloverJob(func(context.Context) (int, error) { return 7, nil })},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.spec.Name != tc.name {
				t.Fatalf("name = %q, want %q — the name is the audit key", tc.spec.Name, tc.name)
			}
			scopes, err := tc.spec.Scopes(context.Background(), nowForTest())
			if err != nil || len(scopes) != 1 || scopes[0] != ScopeGlobal {
				t.Fatalf("scopes = %v (%v), want the single global scope: both jobs sweep every workspace", scopes, err)
			}
			res, err := tc.spec.Handler(context.Background(), HandlerInput{})
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			if res.RowsAffected != 7 {
				t.Fatalf("rows = %d, want the 7 the sweep reported", res.RowsAffected)
			}
		})
	}
}

func TestCycleJobsSurfaceTheirSweepError(t *testing.T) {
	boom := errors.New("boom")
	for _, spec := range []JobSpec{
		CycleSnapshotJob(func(context.Context) (int, error) { return 0, boom }),
		CycleRolloverJob(func(context.Context) (int, error) { return 0, boom }),
	} {
		if _, err := spec.Handler(context.Background(), HandlerInput{}); !errors.Is(err, boom) {
			t.Fatalf("%s: err = %v, want the wrapped sweep error — a swallowed one retries nothing", spec.Name, err)
		}
	}
}
