package triage

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DefaultRetention is how long an unresolved item survives before it may be
// expired, for a source that sets no expiry_days of its own. The purge caller
// owns timing; expires_at is stamped at capture so the data is ready when the
// sweeper runs.
const DefaultRetention = 14 * 24 * time.Hour

// DropReasonRateLimited marks an item the source's cap_per_hour refused. The
// row is kept (shadow) rather than discarded: a flood that never reached the
// queue is exactly the population triage exists to make visible.
const DropReasonRateLimited = "rate_limited"

// CaptureParams describes one inbound item. Shadow captures set Shadow=true
// and never change routing; drops set State=StateDropped with DropReason.
type CaptureParams struct {
	WorkspaceID     pgtype.UUID
	SourceKind      string
	SourceRefID     pgtype.UUID
	SourceName      string
	SourceCreatedBy pgtype.UUID
	OriginType      string
	OriginID        pgtype.UUID
	Title           string
	BodyMarkdown    string
	TriggerPayload  []byte
	State           string
	DropReason      string
	Shadow          bool
}

// AutoAcceptEnabled reports whether a source resolves its own captures without
// waiting for a human. The column is a JSONB policy object so it can grow
// (per-project defaults, assignee) without another migration; today only
// `{"enabled": true}` is read. Malformed JSON reads as disabled: auto-accept
// creates issues, so it must never be turned on by accident.
func AutoAcceptEnabled(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var cfg struct {
		Enabled bool `json:"enabled"`
	}
	return json.Unmarshal(raw, &cfg) == nil && cfg.Enabled
}

// retention resolves how long this source's items live. expiry_days <= 0 means
// "unset" (the column's own default is 14), so it falls back to DefaultRetention.
func retention(source db.TriageSource) time.Duration {
	if source.ExpiryDays > 0 {
		return time.Duration(source.ExpiryDays) * 24 * time.Hour
	}
	return DefaultRetention
}

// rateLimited reports whether this source already captured cap_per_hour queue
// rows in the last hour. Only real pending captures are metered: a shadow
// measurement row and a drop audit row cost nothing downstream. A counting
// error fails open — an anti-flood cap must never become a data-loss path.
func rateLimited(ctx context.Context, q *db.Queries, source db.TriageSource, p CaptureParams) bool {
	if source.CapPerHour <= 0 || p.State != StatePending || p.Shadow {
		return false
	}
	n, err := q.CountTriageItemsForSourceSince(ctx, db.CountTriageItemsForSourceSinceParams{
		WorkspaceID: p.WorkspaceID,
		SourceID:    source.ID,
		Since:       pgtype.Timestamptz{Time: time.Now().Add(-time.Hour), Valid: true},
	})
	if err != nil {
		return false
	}
	return n >= int64(source.CapPerHour)
}

// Capture upserts the item's source and records the item, folding it into the
// source's existing pending item for the same normalized title when one
// exists. It returns the source too, so the caller can apply the rest of the
// per-source policy (auto_accept) without a second lookup. Callers outside
// this package own dispatch: an error here must be logged and swallowed, never
// returned up a path that creates issues.
func Capture(ctx context.Context, q *db.Queries, p CaptureParams) (db.TriageItem, db.TriageSource, error) {
	source, err := q.UpsertTriageSource(ctx, db.UpsertTriageSourceParams{
		WorkspaceID: p.WorkspaceID,
		Kind:        p.SourceKind,
		RefID:       p.SourceRefID,
		Name:        p.SourceName,
		CreatedByID: p.SourceCreatedBy,
	})
	if err != nil {
		return db.TriageItem{}, db.TriageSource{}, fmt.Errorf("upsert triage source: %w", err)
	}

	state, dropReason, shadow := p.State, p.DropReason, p.Shadow
	if rateLimited(ctx, q, source, p) {
		state, dropReason, shadow = StateDropped, DropReasonRateLimited, true
	}

	item, err := q.UpsertTriageItem(ctx, db.UpsertTriageItemParams{
		WorkspaceID:     p.WorkspaceID,
		SourceID:        source.ID,
		OriginType:      p.OriginType,
		OriginID:        p.OriginID,
		DedupeKey:       pgtype.Text{Valid: true},
		ContentDigest:   pgtype.Text{String: ContentDigest(p.Title, p.TriggerPayload), Valid: true},
		Title:           pgtype.Text{String: p.Title, Valid: true},
		NormalizedTitle: pgtype.Text{String: NormalizeTitle(p.Title), Valid: true},
		BodyMarkdown:    pgtype.Text{String: p.BodyMarkdown, Valid: true},
		Payload:         BuildPayload(p.TriggerPayload),
		State:           state,
		DropReason:      pgtype.Text{String: dropReason, Valid: dropReason != ""},
		Shadow:          shadow,
		ExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(retention(source)), Valid: true},
	})
	if err != nil {
		return db.TriageItem{}, source, fmt.Errorf("upsert triage item: %w", err)
	}
	return item, source, nil
}
