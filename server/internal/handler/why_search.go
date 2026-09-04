package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Why search (K55): comments, decision records and agent text messages are
// indexed as they are written, and a plain-language question finds the
// source that answers it, with a link. Full-text search first; the table is
// shaped so embeddings can be added without moving rows.

const (
	whySourceComment        = "comment"
	whySourceTaskMessage    = "task_message"
	whySourceDecisionRecord = "decision_record"
	whyMinContentChars      = 20
	whyMinQueryChars        = 3
)

// indexWhy upserts one chunk. Best effort: a search index must never fail
// the write it follows.
func (h *Handler) indexWhy(ctx context.Context, wsID pgtype.UUID, sourceType string, sourceID, issueID pgtype.UUID, content string) {
	content = strings.TrimSpace(content)
	if len([]rune(content)) < whyMinContentChars {
		return
	}
	if err := h.Queries.UpsertWhyChunk(ctx, db.UpsertWhyChunkParams{
		ID: dbid.NewV7(), WorkspaceID: wsID, SourceType: sourceType, SourceID: sourceID, IssueID: issueID, Content: content,
	}); err != nil {
		slog.Warn("why search: index failed", "error", err, "source_type", sourceType, "source_id", uuidToString(sourceID))
	}
}

func (h *Handler) unindexWhy(ctx context.Context, sourceType string, sourceID pgtype.UUID) {
	if err := h.Queries.DeleteWhyChunk(ctx, db.DeleteWhyChunkParams{SourceType: sourceType, SourceID: sourceID}); err != nil {
		slog.Warn("why search: unindex failed", "error", err, "source_type", sourceType, "source_id", uuidToString(sourceID))
	}
}

func decisionRecordWhyContent(title, context, decision string) string {
	return strings.TrimSpace(strings.Join([]string{title, context, decision}, "\n"))
}

type WhySearchResult struct {
	ID              string  `json:"id"`
	SourceType      string  `json:"source_type"`
	SourceID        string  `json:"source_id"`
	IssueID         *string `json:"issue_id"`
	IssueIdentifier string  `json:"issue_identifier,omitempty"`
	IssueTitle      string  `json:"issue_title,omitempty"`
	Snippet         string  `json:"snippet"`
	Score           float64 `json:"score"`
	CreatedAt       string  `json:"created_at"`
}

// GET /api/search/why?q=
func (h *Handler) SearchWhy(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < whyMinQueryChars {
		writeErrorCode(w, http.StatusBadRequest, "search_query_too_short", fmt.Sprintf("q needs at least %d characters", whyMinQueryChars))
		return
	}
	rows, err := h.Queries.SearchWhy(r.Context(), db.SearchWhyParams{WorkspaceID: wsUUID, Query: q})
	if err == nil && len(rows) == 0 && len(strings.Fields(q)) > 1 {
		// Nothing holds every word: any word will do, ranked.
		rows, err = h.Queries.SearchWhy(r.Context(), db.SearchWhyParams{WorkspaceID: wsUUID, Query: strings.Join(strings.Fields(q), " or ")})
	}
	if err != nil {
		slog.Warn("why search failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	out := make([]WhySearchResult, 0, len(rows))
	for _, row := range rows {
		res := WhySearchResult{
			ID: uuidToString(row.ID), SourceType: row.SourceType, SourceID: uuidToString(row.SourceID), IssueID: uuidToPtr(row.IssueID),
			Snippet: row.Snippet, Score: row.Score, CreatedAt: timestampToString(row.CreatedAt),
		}
		if row.IssueNumber.Valid {
			res.IssueIdentifier = fmt.Sprintf("%s-%d", prefix, row.IssueNumber.Int32)
			res.IssueTitle = row.IssueTitle.String
		}
		out = append(out, res)
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": out, "query": q})
}

// POST /api/search/why/reindex (owner/admin): rebuilds the index from the
// last 5000 comments, decision records and agent text messages.
func (h *Handler) ReindexWhy(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	ctx := r.Context()
	counts := map[string]int{whySourceComment: 0, whySourceDecisionRecord: 0, whySourceTaskMessage: 0}
	comments, err := h.Queries.ListWorkspaceCommentsForWhy(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list comments")
		return
	}
	for _, c := range comments {
		h.indexWhy(ctx, wsUUID, whySourceComment, c.ID, c.IssueID, c.Content)
		counts[whySourceComment]++
	}
	records, err := h.Queries.ListWorkspaceDecisionRecordsForWhy(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list decision records")
		return
	}
	for _, d := range records {
		h.indexWhy(ctx, wsUUID, whySourceDecisionRecord, d.ID, d.IssueID, decisionRecordWhyContent(d.Title, d.Context, d.Decision))
		counts[whySourceDecisionRecord]++
	}
	messages, err := h.Queries.ListWorkspaceTextMessagesForWhy(ctx, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list run messages")
		return
	}
	for _, m := range messages {
		h.indexWhy(ctx, wsUUID, whySourceTaskMessage, m.ID, m.IssueID, m.Content.String)
		counts[whySourceTaskMessage]++
	}
	writeJSON(w, http.StatusOK, map[string]any{"indexed": counts})
}
