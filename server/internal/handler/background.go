package handler

import (
	"log/slog"
	"runtime/debug"
)

// goBackground runs fn on its own goroutine behind a recover. Work that
// leaves the request (a transcription, a model call, a best-effort backlink)
// used to run inside the handler, where chi's middleware caught a panic; on
// a bare goroutine the same panic takes the whole process down.
func goBackground(what string, fn func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error(what+" panicked", "panic", r, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}
