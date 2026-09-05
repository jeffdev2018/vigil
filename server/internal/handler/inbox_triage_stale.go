package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Aging triage queue digest. The queue reports oldest_pending_age_seconds to
// anyone who opens it, which is exactly the person who is already looking;
// nothing tells the workspace when the queue quietly stops being read. This
// files one inbox item per admin/owner per day for a workspace whose oldest
// real pending item has been waiting longer than the threshold.
//
// Notified daily and no more: an ignored queue is ignored every day, and one
// reminder a day is a nudge while one an hour is a reason to mute the inbox.

const (
	// InboxTypeTriageStale is the inbox item type for this digest.
	InboxTypeTriageStale = "triage_stale"
	// triageStaleAfter is how long the oldest pending item may wait before
	// the queue counts as stalling. Two days, so a queue left over a normal
	// weekend does not page anyone on Monday morning for Friday's items.
	triageStaleAfter = 48 * time.Hour
	// triageStalePath is where the recipient should go, relative to their
	// workspace. Stored in details so the client does not have to know which
	// surface a given inbox type belongs to.
	triageStalePath = "/triage"
)

// RunTriageStaleDigest is the scheduler's entry point. Returns how many inbox
// items it filed. Errors on one workspace are logged and skipped: a digest is
// per workspace, and one broken one must not silence every other.
func (h *Handler) RunTriageStaleDigest(ctx context.Context, now time.Time) (int, error) {
	cutoff := pgtype.Timestamptz{Time: now.Add(-triageStaleAfter), Valid: true}
	stale, err := h.Queries.ListWorkspacesWithStalePendingTriage(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("list stale triage: %w", err)
	}
	if len(stale) == 0 {
		return 0, nil
	}
	// The timezone decides which calendar day the digest is for, and the day
	// is what dedups it. Read once for the whole install rather than per
	// workspace with a backlog.
	workspaces, err := h.Queries.ListWorkspacesForBriefing(ctx)
	if err != nil {
		return 0, fmt.Errorf("list workspaces: %w", err)
	}
	settings := make(map[string][]byte, len(workspaces))
	for _, ws := range workspaces {
		settings[uuidToString(ws.ID)] = ws.Settings
	}

	filed := 0
	for _, row := range stale {
		wsID := uuidToString(row.WorkspaceID)
		if _, known := settings[wsID]; !known {
			// A queue whose workspace is gone; the retention sweep owns it.
			continue
		}
		loc := briefingLocation(service.WorkspaceTimezone(settings[wsID]))
		n, err := h.fileTriageStaleDigest(ctx, row, now.In(loc).Format("2006-01-02"), now)
		filed += n
		if err != nil {
			slog.Warn("triage stale digest failed", "error", err, "workspace_id", wsID)
		}
	}
	return filed, nil
}

func (h *Handler) fileTriageStaleDigest(ctx context.Context, row db.ListWorkspacesWithStalePendingTriageRow, day string, now time.Time) (int, error) {
	recipients, err := service.ListWorkspaceManagerNotificationRecipients(ctx, h.Queries, row.WorkspaceID)
	if err != nil {
		return 0, fmt.Errorf("list managers: %w", err)
	}
	oldestHours := 0
	if row.OldestFirstSeenAt.Valid {
		oldestHours = int(now.Sub(row.OldestFirstSeenAt.Time).Hours())
	}
	title := fmt.Sprintf("%d triage items waiting, the oldest for %dh", row.PendingCount, oldestHours)
	body := "Nobody has decided on these yet. Accept, dismiss or merge them, or turn the source off."
	details, _ := json.Marshal(map[string]any{
		"day":          day,
		"count":        row.PendingCount,
		"oldest_hours": oldestHours,
		"path":         triageStalePath,
	})

	filed := 0
	for _, rcpt := range recipients {
		if rcpt.Type != "member" {
			continue
		}
		already, err := h.Queries.CountInboxItemsForDay(ctx, db.CountInboxItemsForDayParams{
			WorkspaceID: row.WorkspaceID, RecipientID: rcpt.ID, Type: InboxTypeTriageStale, Day: day,
		})
		if err != nil {
			return filed, fmt.Errorf("dedup check: %w", err)
		}
		if already > 0 {
			continue
		}
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: row.WorkspaceID,
			RecipientType: "member", RecipientID: rcpt.ID,
			Type: InboxTypeTriageStale, Severity: "attention",
			Title:     title,
			Body:      pgtype.Text{String: body, Valid: true},
			ActorType: pgtype.Text{String: "system", Valid: true},
			Details:   details,
		})
		if err != nil {
			return filed, fmt.Errorf("create inbox item: %w", err)
		}
		h.publish(protocol.EventInboxNew, uuidToString(row.WorkspaceID), "system", "", map[string]any{"item": inboxToResponse(item)})
		filed++
	}
	return filed, nil
}

// countPendingTriage is the number of real pending items in one workspace,
// for the morning briefing's counter row. A read error is reported as zero:
// the briefing must still go out.
func (h *Handler) countPendingTriage(ctx context.Context, wsID pgtype.UUID) int {
	rows, err := h.Queries.CountTriageItemsByState(ctx, wsID)
	if err != nil {
		slog.Warn("briefing: count triage failed", "error", err, "workspace_id", uuidToString(wsID))
		return 0
	}
	total := 0
	for _, row := range rows {
		if row.State == "pending" && !row.Shadow {
			total += int(row.N)
		}
	}
	return total
}
