package daemon

import (
	"context"
	"testing"
	"time"
)

// Pause at a safe boundary (K19): a boundary before the request does
// nothing; the request alone does nothing until the next boundary; then the
// interrupt fires exactly once.

func TestPauseControlWaitsForTheBoundary(t *testing.T) {
	t.Parallel()
	p := newPauseControl()
	p.atBoundary()
	if p.paused() {
		t.Fatal("a boundary before any request must not pause")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.request(ctx)
	if p.paused() {
		t.Fatal("a request must wait for the next boundary")
	}
	p.atBoundary()
	select {
	case <-p.boundary:
	case <-time.After(time.Second):
		t.Fatal("the boundary after a request must fire the pause")
	}
	p.atBoundary()
	p.reach()
	if !p.paused() {
		t.Fatal("paused must stay true")
	}
	var d Daemon
	if d.pauseControlFor("t1") != d.pauseControlFor("t1") || d.pauseControlFor("t1") == d.pauseControlFor("t2") {
		t.Fatal("one control per task")
	}
}

// A pause requested while the task waits for a local directory lock arms its
// grace timer on the wait's context. That context ends with the wait, and the
// run-phase request that follows must re-arm the timer, or a silent run is
// never paused.
func TestPauseGraceSurvivesTheRequestingContext(t *testing.T) {
	t.Parallel()
	p := newPauseControl()
	p.grace = 200 * time.Millisecond

	waitCtx, waitCancel := context.WithCancel(context.Background())
	p.request(waitCtx)
	waitCancel()

	// The run-phase watcher repeats the request on every status poll.
	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()
	deadline := time.After(3 * time.Second)
	for {
		p.request(runCtx)
		select {
		case <-p.boundary:
			return
		case <-deadline:
			t.Fatal("the grace timer died with the wait context; the pause never fires")
		case <-time.After(50 * time.Millisecond):
		}
	}
}
