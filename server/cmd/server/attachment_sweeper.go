package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/handler"
)

// attachmentSweepInterval paces the orphaned-attachment sweep (JEF-292).
// Nothing here is latency-critical: a newly-unreferenced attachment only
// becomes eligible for deletion after a 7-day grace period, so a slower tick
// than that has no visible effect beyond delaying storage reclamation.
const attachmentSweepInterval = time.Minute

func runAttachmentSweeper(ctx context.Context, h *handler.Handler) {
	ticker := time.NewTicker(attachmentSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			marked, unmarked, deleted, err := h.SweepUnreferencedAttachments(sweepCtx)
			cancel()
			if err != nil {
				slog.Warn("attachment sweeper round had errors", "error", err)
			}
			if marked > 0 || unmarked > 0 || deleted > 0 {
				slog.Info("attachment sweeper round completed", "marked", marked, "unmarked", unmarked, "deleted", deleted)
			}
		}
	}
}
