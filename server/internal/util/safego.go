package util

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"
)

// Supervised loops wait this long after a first panic before restarting, doubling
// up to superviseMaxBackoff. Variables so tests can shorten them.
var (
	superviseMinBackoff = time.Second
	superviseMaxBackoff = time.Minute
)

// GoBackground runs fn on its own goroutine behind a recover. Work that leaves
// a request (a transcription, a model call, a best-effort email) used to run
// inside the handler, where chi's middleware caught a panic; on a bare
// goroutine the same panic takes the whole process down.
func GoBackground(what string, fn func()) {
	go func() {
		runRecovered(what, fn)
	}()
}

// Supervise runs a long-lived loop until it returns on its own or ctx is done.
// A panic inside fn is logged and the loop is restarted after a backoff, so a
// bug in one background service neither crashes the process nor silently
// stops that service. fn must be safe to call again after it panicked.
func Supervise(ctx context.Context, what string, fn func(context.Context)) {
	backoff := superviseMinBackoff
	for {
		started := time.Now()
		if !runRecovered(what, func() { fn(ctx) }) {
			return
		}
		// A loop that ran healthy for a long stretch starts its backoff over.
		if time.Since(started) > superviseMaxBackoff {
			backoff = superviseMinBackoff
		}
		slog.Warn(what+" restarting after panic", "backoff", backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		backoff = min(backoff*2, superviseMaxBackoff)
	}
}

// runRecovered calls fn and reports whether it panicked.
func runRecovered(what string, fn func()) (panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
			slog.Error(what+" panicked", "panic", r, "stack", string(debug.Stack()))
		}
	}()
	fn()
	return false
}
