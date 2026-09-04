package triage

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DefaultRetention is how long an unresolved item survives before it may be
// expired. The purge caller owns timing; expires_at is stamped at capture so
// the data is ready when the sweeper lands.
const DefaultRetention = 14 * 24 * time.Hour

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

// Capture upserts the item's source and records the item, folding it into the
// source's existing pending item for the same normalized title when one
// exists. Callers outside this package own dispatch: an error here must be
// logged and swallowed, never returned up a path that creates issues.
func Capture(ctx context.Context, q *db.Queries, p CaptureParams) (db.TriageItem, error) {
	source, err := q.UpsertTriageSource(ctx, db.UpsertTriageSourceParams{
		WorkspaceID: p.WorkspaceID,
		Kind:        p.SourceKind,
		RefID:       p.SourceRefID,
		Name:        p.SourceName,
		CreatedByID: p.SourceCreatedBy,
	})
	if err != nil {
		return db.TriageItem{}, fmt.Errorf("upsert triage source: %w", err)
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
		State:           p.State,
		DropReason:      pgtype.Text{String: p.DropReason, Valid: p.DropReason != ""},
		Shadow:          p.Shadow,
		ExpiresAt:       pgtype.Timestamptz{Time: time.Now().Add(DefaultRetention), Valid: true},
	})
	if err != nil {
		return db.TriageItem{}, fmt.Errorf("upsert triage item: %w", err)
	}
	return item, nil
}
