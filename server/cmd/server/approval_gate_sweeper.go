package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/handler"
)

// approvalGateSweepInterval bounds how long an expired gate can look pending
// to someone who is not reading it: the run's own long-poll expires it on
// read, but an inbox row or a timeline card only changes when this runs.
const approvalGateSweepInterval = time.Minute

func runApprovalGateSweeper(ctx context.Context, h *handler.Handler) {
	ticker := time.NewTicker(approvalGateSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			if n := h.ExpireOverdueGates(sweepCtx); n > 0 {
				slog.Info("approval gate sweeper settled expired gates", "count", n)
			}
			cancel()
		}
	}
}
