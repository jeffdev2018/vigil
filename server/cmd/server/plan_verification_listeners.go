package main

import (
	"context"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// registerPlanVerificationListeners queues a verification run when a run
// completes on an issue with an active plan (F17), and mirrors the
// verification run's own lifecycle onto its plan_verification row.
func registerPlanVerificationListeners(bus *events.Bus, svc *service.TaskService) {
	ctx := context.Background()
	taskIDOf := func(e events.Event) string {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return ""
		}
		id, _ := payload["task_id"].(string)
		return id
	}
	bus.Subscribe(protocol.EventTaskCompleted, func(e events.Event) {
		id := taskIDOf(e)
		if id == "" {
			return
		}
		// A verification run that completes without having reported failed
		// its one job; a reported row is untouched by this (state guard).
		svc.SyncPlanVerificationState(ctx, parseUUID(id), "failed")
		if err := svc.MaybeEnqueuePlanVerification(ctx, parseUUID(id)); err != nil {
			slog.Warn("plan verification: enqueue after completion failed", "task_id", id, "error", err)
		}
	})
	bus.Subscribe(protocol.EventTaskRunning, func(e events.Event) {
		if id := taskIDOf(e); id != "" {
			svc.SyncPlanVerificationState(ctx, parseUUID(id), "running")
		}
	})
	for _, terminal := range []string{protocol.EventTaskFailed, protocol.EventTaskCancelled} {
		bus.Subscribe(terminal, func(e events.Event) {
			if id := taskIDOf(e); id != "" {
				svc.SyncPlanVerificationState(ctx, parseUUID(id), "failed")
			}
		})
	}
}
