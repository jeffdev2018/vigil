package service

// Brain search (JEF-412): the one ranked note search behind the REST
// endpoint (palette, Brain page, CLI, mobile, MCP), capture merge candidates
// and the native agent tools. Passages are indexed lazily at search time, so
// none of the many writers of workspace_note can leave a note unfindable.

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	brainSearchPrefilter = 60
	// brainLazyIndexLimit bounds the synchronous catch-up one search does;
	// the backfill job indexes whatever is left.
	brainLazyIndexLimit = 200
)

// BrainSearchParams is one ranked note search.
type BrainSearchParams struct {
	WorkspaceID     pgtype.UUID
	Query, Tag      string
	IncludeArchived bool
	Limit           int32
	// Neighbours admits notes only the vector leg finds: capture merge
	// candidates a model judges. A search a person reads needs a lexical
	// match or a calibrated similarity.
	Neighbours bool
}

// BrainSearchHit is one ranked note with the passage that answered.
type BrainSearchHit struct {
	Note             db.WorkspaceNote
	Score            float64
	LexRank, VecRank *int64
	Snippet          string
	PassageHeading   string
}

// replaceNotePassages re-cuts one note into its passages, lexically only.
func replaceNotePassages(ctx context.Context, q *db.Queries, id, workspaceID pgtype.UUID, revision int64, title, content string) error {
	passages := SplitNotePassages(title, content)
	params := db.ReplaceNotePassagesParams{
		NoteID: id, WorkspaceID: workspaceID, SearchTitle: NormalizeSearchText(title),
		NoteRevision: revision, ChunkerVersion: BrainChunkerVersion,
		Ordinals:       make([]int32, len(passages)),
		Headings:       make([]string, len(passages)),
		Bodies:         make([]string, len(passages)),
		SearchHeadings: make([]string, len(passages)),
		SearchBodies:   make([]string, len(passages)),
		ContentHashes:  make([]string, len(passages)),
	}
	for i, p := range passages {
		params.Ordinals[i] = int32(p.Ordinal)
		params.Headings[i] = p.Heading
		params.Bodies[i] = p.Body
		params.SearchHeadings[i] = NormalizeSearchText(p.Heading)
		params.SearchBodies[i] = NormalizeSearchText(p.Body)
		params.ContentHashes[i] = NoteContentHash(title, p.Heading+"\n\n"+p.Body)
	}
	return q.ReplaceNotePassages(ctx, params)
}

// indexPendingNotePassages indexes up to limit notes whose passages are
// missing or stale, in one workspace or (invalid workspaceID) in all.
func indexPendingNotePassages(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, limit int32) (int, error) {
	rows, err := q.ListNotesNeedingPassageIndex(ctx, db.ListNotesNeedingPassageIndexParams{WorkspaceID: workspaceID, ChunkerVersion: BrainChunkerVersion, RowLimit: limit})
	if err != nil {
		return 0, err
	}
	for i, n := range rows {
		if err := replaceNotePassages(ctx, q, n.ID, n.WorkspaceID, n.Revision, n.Title, n.Content); err != nil {
			return i, err
		}
	}
	return len(rows), nil
}

// SearchBrainNotes ranks the workspace's notes for a typed query. emb may be
// nil or disabled: the search is then lexical.
func SearchBrainNotes(ctx context.Context, q *db.Queries, emb NoteEmbedder, p BrainSearchParams) ([]BrainSearchHit, error) {
	if _, err := indexPendingNotePassages(ctx, q, p.WorkspaceID, brainLazyIndexLimit); err != nil {
		slog.Warn("brain search: passage catch-up failed, searching the existing index", "workspace_id", util.UUIDToString(p.WorkspaceID), "error", err)
	}
	query := ParseBrainQuery(p.Query)
	if len(query.Items) == 0 && len(query.Excluded) == 0 {
		return []BrainSearchHit{}, nil
	}

	params := db.SearchBrainNotesParams{
		WorkspaceID:     p.WorkspaceID,
		Strict:          query.Strict,
		IncludeArchived: p.IncludeArchived,
		VectorOnlyHits:  p.Neighbours,
		Prefilter:       max(brainSearchPrefilter, p.Limit),
		TopK:            p.Limit,
		ItemTexts:       []string{},
		ItemKinds:       []string{},
		ExcludedTexts:   []string{},
		ExcludedKinds:   []string{},
	}
	for _, it := range query.Items {
		params.ItemTexts = append(params.ItemTexts, it.Text)
		params.ItemKinds = append(params.ItemKinds, it.Kind)
	}
	for _, it := range query.Excluded {
		params.ExcludedTexts = append(params.ExcludedTexts, it.Text)
		params.ExcludedKinds = append(params.ExcludedKinds, it.Kind)
	}
	if p.Tag != "" {
		params.Tag = pgtype.Text{String: p.Tag, Valid: true}
	}
	if emb != nil && emb.Enabled() {
		if literal, model, ok := emb.QueryEmbedding(ctx, p.Query); ok {
			params.QueryEmbedding = pgtype.Text{String: literal, Valid: true}
			params.EmbeddingModel = pgtype.Text{String: model, Valid: true}
			// Without a calibrated floor the vector rank still fuses into the
			// lexical hits; it just admits nothing on its own.
			if floor, ok := emb.VectorFloor(ctx); ok {
				params.VectorMinSimilarity = pgtype.Float8{Float64: floor, Valid: true}
			}
		}
	}

	rows, err := q.SearchBrainNotes(ctx, params)
	if err != nil {
		return nil, err
	}
	hits := make([]BrainSearchHit, 0, len(rows))
	for _, row := range rows {
		hit := BrainSearchHit{
			Note: db.WorkspaceNote{
				ID: row.ID, WorkspaceID: row.WorkspaceID, Title: row.Title, Content: row.Content, Tags: row.Tags,
				Source: row.Source, SourceTaskID: row.SourceTaskID, SourceAgentID: row.SourceAgentID, Pinned: row.Pinned,
				ArchivedAt: row.ArchivedAt, MergedInto: row.MergedInto, CreatedByType: row.CreatedByType, CreatedByID: row.CreatedByID,
				Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
			},
			Score:          row.Score,
			Snippet:        BrainSnippet(row.PassageBody, query),
			PassageHeading: row.PassageHeading,
		}
		if row.LexRank.Valid {
			v := row.LexRank.Int64
			hit.LexRank = &v
		}
		if row.VecRank.Valid {
			v := row.VecRank.Int64
			hit.VecRank = &v
		}
		hits = append(hits, hit)
	}
	return hits, nil
}
