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
