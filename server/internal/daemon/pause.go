package daemon

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Pause at a safe boundary (K19). The status poll learns a human asked
// for a pause; the transcript drain marks the boundary (a tool result or
// a finished text turn); only then is the agent interrupted. A run that
// stays silent is interrupted after pauseBoundaryGrace so a pause is never
// ignored forever.

const pauseBoundaryGrace = 2 * time.Minute

type pauseControl struct {
	requested atomic.Bool
	once      sync.Once
	boundary  chan struct{}
}

func newPauseControl() *pauseControl {
	return &pauseControl{boundary: make(chan struct{})}
}

// request records the human's ask and arms the grace timer.
func (p *pauseControl) request(ctx context.Context) {
	if p == nil || p.requested.Swap(true) {
		return
	}
	go func() {
		select {
		case <-ctx.Done():
		case <-time.After(pauseBoundaryGrace):
			p.reach()
		}
	}()
}

// reach signals the safe boundary; harmless before a request.
func (p *pauseControl) reach() {
	if p == nil {
		return
	}
	p.once.Do(func() { close(p.boundary) })
}

// atBoundary is called by the transcript drain after each tool result or
// text turn: it fires the interrupt only once a pause was requested.
func (p *pauseControl) atBoundary() {
	if p != nil && p.requested.Load() {
		p.reach()
	}
}

// paused reports whether the boundary fired (a pause took effect).
func (p *pauseControl) paused() bool {
	if p == nil {
		return false
	}
	select {
	case <-p.boundary:
		return true
	default:
		return false
	}
}

// pauseControlFor returns the control of a running task, creating it once.
func (d *Daemon) pauseControlFor(taskID string) *pauseControl {
	if v, ok := d.pauseControls.Load(taskID); ok {
		return v.(*pauseControl)
	}
	v, _ := d.pauseControls.LoadOrStore(taskID, newPauseControl())
	return v.(*pauseControl)
}
