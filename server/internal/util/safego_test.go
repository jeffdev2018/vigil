package util

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// A panic in background work must be swallowed by the recover, not crash the
// test binary (which stands in for the server process here).
func TestGoBackgroundRecoversFromPanic(t *testing.T) {
	done := make(chan struct{})
	GoBackground("test job", func() {
		defer close(done)
		panic("boom")
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background job did not finish")
	}
}

func shortenSuperviseBackoff(t *testing.T) {
	minB, maxB := superviseMinBackoff, superviseMaxBackoff
	superviseMinBackoff, superviseMaxBackoff = time.Millisecond, 4*time.Millisecond
	t.Cleanup(func() { superviseMinBackoff, superviseMaxBackoff = minB, maxB })
}

// A worker loop that panics is restarted instead of dying silently, and
// Supervise returns once the loop exits normally.
func TestSuperviseRestartsLoopAfterPanic(t *testing.T) {
	shortenSuperviseBackoff(t)
	var calls atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		Supervise(context.Background(), "test loop", func(context.Context) {
			if calls.Add(1) < 3 {
				panic("boom")
			}
		})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("supervised loop was not restarted to completion")
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("loop ran %d times, want 3 (two panics, one clean exit)", got)
	}
}

// Cancelling ctx during the restart backoff stops the supervisor.
func TestSuperviseStopsWhenContextEnds(t *testing.T) {
	minB, maxB := superviseMinBackoff, superviseMaxBackoff
	superviseMinBackoff, superviseMaxBackoff = time.Hour, time.Hour
	t.Cleanup(func() { superviseMinBackoff, superviseMaxBackoff = minB, maxB })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		Supervise(ctx, "test loop", func(context.Context) { panic("boom") })
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Supervise did not return after ctx was cancelled")
	}
}
