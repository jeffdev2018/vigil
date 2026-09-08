package handler

import (
	"context"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Comment threads anchored to a diff line (F07 / JEF-21).
//
// A thread can point at a file, a line range and a side of ONE revision of a
// linked pull request's head. Three decisions shape everything below:
//
//   - the anchor lives on the thread ROOT. A request that sends both an anchor
//     and a parent_id is refused rather than silently ignored: a reply that
//     could carry its own anchor would let one thread describe two places, and
//     the reader would have no way to tell which one the thread is about.
//   - replies INHERIT the root's anchor at read time. Nothing is copied onto
//     the reply row, so moving or correcting an anchor stays a single write.
//   - a head that moves marks the anchor stale, never hides it. The question
//     was asked about the code it was asked about, and the reader who has to
//     answer it needs to see that the code has since changed.
//
// anchor_kind is deliberately open text. `diff_line` is the only value this
// build writes; a thread carrying a kind this build does not know is rendered
// WITHOUT its anchor rather than dropped, so a newer server never makes a
// discussion disappear from an older client.

const (
	// anchorKindDiffLine is the only kind this build writes.
	anchorKindDiffLine = "diff_line"

	// anchoredThreadsCap bounds the anchored-threads read. It counts THREADS,
	// not rows: capping rows would return a thread missing its last replies,
	// which reads as a conversation whose end was deleted.
	anchoredThreadsCap = 200

	anchorFilePathMax = 1024
)

// CommentAnchorRequest is the optional `anchor` object on POST /comments.
// head_sha is optional: omitted, it defaults to the pull request's current
// head, which is what the reviewer is looking at when they click a line.
type CommentAnchorRequest struct {
	PrID          string `json:"pr_id"`
	HeadSha       string `json:"head_sha"`
	FilePath      string `json:"file_path"`
	LineStart     *int   `json:"line_start"`
	LineEnd       *int   `json:"line_end"`
	Side          string `json:"side"`
	ReviewFlagID  string `json:"review_flag_id"`
	PrSource      string `json:"pr_source"`
	AnchorKindRaw string `json:"kind"`
}

// CommentAnchorResponse is the anchor as every comment response exposes it.
// `stale` is computed against the pull request's CURRENT head, so it is a
// property of the read rather than of the stored row.
type CommentAnchorResponse struct {
	Kind         string  `json:"kind"`
	PrSource     string  `json:"pr_source"`
	PrID         string  `json:"pr_id"`
	HeadSha      string  `json:"head_sha"`
	FilePath     string  `json:"file_path"`
	LineStart    int32   `json:"line_start"`
	LineEnd      int32   `json:"line_end"`
	Side         string  `json:"side"`
	ReviewFlagID *string `json:"review_flag_id"`
}

// AnchoredThread is one anchored discussion: its root and every reply under
// it, plus the anchor resolved once for the whole thread.
type AnchoredThread struct {
	Root        CommentResponse        `json:"root"`
	Replies     []CommentResponse      `json:"replies"`
	Anchor      *CommentAnchorResponse `json:"anchor"`
	AnchorStale bool                   `json:"anchor_stale"`
}

type anchoredThreadsResponse struct {
	Threads []AnchoredThread `json:"threads"`
}

// commentAnchorFromRow reads the anchor off a comment row. Returns nil when
// the row is not anchored, which is every ordinary comment.
func commentAnchorFromRow(c db.Comment) *CommentAnchorResponse {
	if !c.AnchorKind.Valid || c.AnchorKind.String == "" {
		return nil
	}
	return &CommentAnchorResponse{
		Kind:         c.AnchorKind.String,
		PrSource:     c.AnchorPrSource.String,
		PrID:         uuidToString(c.AnchorPrID),
		HeadSha:      c.AnchorHeadSha.String,
		FilePath:     c.AnchorFilePath.String,
		LineStart:    c.AnchorLineStart.Int32,
		LineEnd:      c.AnchorLineEnd.Int32,
		Side:         c.AnchorSide.String,
		ReviewFlagID: uuidToPtr(c.AnchorReviewFlagID),
	}
}

// anchorColumns is what CreateComment stamps on the row. Kept as its own type
// so the validated result travels as one value instead of nine parallel ones.
type anchorColumns struct {
	Kind         pgtype.Text
	PrSource     pgtype.Text
	PrID         pgtype.UUID
	HeadSha      pgtype.Text
	FilePath     pgtype.Text
	LineStart    pgtype.Int4
	LineEnd      pgtype.Int4
	Side         pgtype.Text
	ReviewFlagID pgtype.UUID
}

// validateCommentAnchor turns the request's anchor into the columns to stamp.
// It writes the error response itself and returns ok=false, matching the other
// request-boundary validators in this package.
//
// parentID is passed so the "anchors live on the root" rule is refused HERE,
// at the boundary, rather than by a storage constraint that could not explain
// itself. See the file comment for why a reply may not carry its own anchor.
func (h *Handler) validateCommentAnchor(w http.ResponseWriter, r *http.Request, issue db.Issue, req *CommentAnchorRequest, parentID pgtype.UUID) (anchorColumns, bool) {
	if parentID.Valid {
		writeError(w, http.StatusBadRequest, "anchor is only allowed on a thread root; reply without an anchor and the thread's anchor is inherited")
		return anchorColumns{}, false
	}
	// The pull request must be linked to THIS issue. loadLinkedPR answers only
	// from the issue's own link lists, so an id belonging to another issue or
	// workspace is indistinguishable from one that does not exist.
	pr, ok := h.loadLinkedPR(r.Context(), issue, strings.TrimSpace(req.PrID))
	if !ok {
		writeError(w, http.StatusNotFound, "pull request not found")
		return anchorColumns{}, false
	}
	filePath := strings.TrimSpace(req.FilePath)
	if filePath == "" {
		writeError(w, http.StatusBadRequest, "anchor.file_path is required")
		return anchorColumns{}, false
	}
	if len(filePath) > anchorFilePathMax {
		writeError(w, http.StatusBadRequest, "anchor.file_path is too long")
		return anchorColumns{}, false
	}
	if req.LineStart == nil || *req.LineStart < 1 {
		writeError(w, http.StatusBadRequest, "anchor.line_start must be a positive line number")
		return anchorColumns{}, false
	}
	lineStart := *req.LineStart
	lineEnd := lineStart
	if req.LineEnd != nil {
		lineEnd = *req.LineEnd
	}
	if lineEnd < lineStart {
		writeError(w, http.StatusBadRequest, "anchor.line_end must be greater than or equal to anchor.line_start")
		return anchorColumns{}, false
	}
	// `new` is the changed code and the overwhelmingly common case, so an
	// omitted side means new. An unknown side is refused rather than coerced:
	// silently moving a thread to the other side of a diff would point it at
	// different code than the author was reading.
	side := strings.TrimSpace(req.Side)
	if side == "" {
		side = "new"
	}
	if side != "old" && side != "new" {
		writeError(w, http.StatusBadRequest, "anchor.side must be old or new")
		return anchorColumns{}, false
	}
	// The head the reviewer was looking at. Defaulting to the pull request's
	// current head is what makes "click a line, ask a question" work without
	// the client having to know about commits at all.
	headSha := strings.TrimSpace(req.HeadSha)
	if headSha == "" {
		headSha = pr.headSHA
	}

	var reviewFlagID pgtype.UUID
	if id := strings.TrimSpace(req.ReviewFlagID); id != "" {
		parsed, ok := parseUUIDOrBadRequest(w, id, "anchor.review_flag_id")
		if !ok {
			return anchorColumns{}, false
		}
		flag, err := h.Queries.GetReviewFlag(r.Context(), parsed)
		// The flag must belong to the same issue AND the same pull request.
		// Either mismatch is a 404 for the same reason loadLinkedPR is: the
		// caller must not be able to probe another issue's findings.
		if err != nil ||
			uuidToString(flag.IssueID) != uuidToString(issue.ID) ||
			uuidToString(flag.PrID) != uuidToString(pr.id) {
			writeError(w, http.StatusNotFound, "review flag not found")
			return anchorColumns{}, false
		}
		reviewFlagID = parsed
	}

	return anchorColumns{
		Kind:         pgtype.Text{String: anchorKindDiffLine, Valid: true},
		PrSource:     pgtype.Text{String: pr.source, Valid: true},
		PrID:         pr.id,
		HeadSha:      pgtype.Text{String: headSha, Valid: true},
		FilePath:     pgtype.Text{String: filePath, Valid: true},
		LineStart:    pgtype.Int4{Int32: int32(lineStart), Valid: true},
		LineEnd:      pgtype.Int4{Int32: int32(lineEnd), Valid: true},
		Side:         pgtype.Text{String: side, Valid: true},
		ReviewFlagID: reviewFlagID,
	}, true
}

// headCache memoises "what head is this pull request on right now" for the
// span of one response. A list can hold many threads on the same pull request
// and the staleness of every one of them is decided by the same lookup.
type headCache struct {
	h    *Handler
	seen map[string]string
}

func (h *Handler) newHeadCache() *headCache {
	return &headCache{h: h, seen: map[string]string{}}
}

func (c *headCache) current(ctx context.Context, source string, prID pgtype.UUID) string {
	key := source + ":" + uuidToString(prID)
	if sha, ok := c.seen[key]; ok {
		return sha
	}
	sha := c.h.currentHeadSHA(ctx, source, prID)
	c.seen[key] = sha
	return sha
}

// stale reports whether an anchor describes a head the pull request has moved
// past. An unknown current head (the pull request row is gone, or the source
// is one this build does not read) is NOT stale: claiming the code changed
// when we could not look is worse than saying nothing.
func (c *headCache) stale(ctx context.Context, anchor *CommentAnchorResponse) bool {
	if anchor == nil || anchor.HeadSha == "" {
		return false
	}
	// parseUUIDOrZero is safe here: PrID came out of the database through
	// uuidToString, so it is either a valid UUID or the empty string.
	current := c.current(ctx, anchor.PrSource, parseUUIDOrZero(anchor.PrID))
	return current != "" && current != anchor.HeadSha
}

// resolveCommentAnchors returns the anchor of each comment in `rows`, and
// whether it is stale, index-aligned with the input.
//
// A reply's anchor is its thread ROOT's. The root is resolved from the set
// being rendered whenever it is there — which is every read that holds
// complete threads, so the common path costs no query at all. Only comments
// whose root is missing (a partial read such as --since / --tail) fall back to
// ONE batched lookup for the whole set.
//
// Returns (nil, nil) when nothing in the set is anchored, which is the
// overwhelmingly common case and also skips the head lookups entirely.
func (h *Handler) resolveCommentAnchors(ctx context.Context, workspaceID pgtype.UUID, rows []db.Comment) ([]*CommentAnchorResponse, []bool) {
	if len(rows) == 0 {
		return nil, nil
	}
	byID := make(map[string]db.Comment, len(rows))
	for _, c := range rows {
		byID[uuidToString(c.ID)] = c
	}

	anchors := make([]*CommentAnchorResponse, len(rows))
	var missing []pgtype.UUID
	missingIdx := map[string][]int{}
	anyAnchor := false

	for i, c := range rows {
		if rootID, ok := commentRootID(uuidToString(c.ID), byID); ok {
			anchors[i] = commentAnchorFromRow(byID[rootID])
			if anchors[i] != nil {
				anyAnchor = true
			}
			continue
		}
		// The thread root is outside this set. Batch it.
		id := uuidToString(c.ID)
		if _, seen := missingIdx[id]; !seen {
			missing = append(missing, c.ID)
		}
		missingIdx[id] = append(missingIdx[id], i)
	}

	if len(missing) > 0 {
		found, err := h.Queries.ListAnchoredRootsForComments(ctx, db.ListAnchoredRootsForCommentsParams{
			CommentIds:  missing,
			WorkspaceID: workspaceID,
		})
		// A failed lookup degrades to "no anchor", exactly like an older
		// backend: the thread still renders, it just loses its chip.
		if err == nil {
			for _, row := range found {
				anchor := commentAnchorFromRow(row.Comment())
				if anchor == nil {
					continue
				}
				for _, i := range missingIdx[uuidToString(row.Seed)] {
					anchors[i] = anchor
					anyAnchor = true
				}
			}
		}
	}
	if !anyAnchor {
		return nil, nil
	}

	stale := make([]bool, len(rows))
	cache := h.newHeadCache()
	for i, a := range anchors {
		if a != nil {
			stale[i] = cache.stale(ctx, a)
		}
	}
	return anchors, stale
}

// applyCommentAnchors fills `anchor` and `anchor_stale` on a set of comment
// responses. `rows` and `resp` are index-aligned.
func (h *Handler) applyCommentAnchors(ctx context.Context, workspaceID pgtype.UUID, rows []db.Comment, resp []CommentResponse) {
	if len(rows) != len(resp) {
		return
	}
	anchors, stale := h.resolveCommentAnchors(ctx, workspaceID, rows)
	for i := range anchors {
		if anchors[i] == nil {
			continue
		}
		resp[i].Anchor = anchors[i]
		resp[i].AnchorStale = stale[i]
	}
}

// commentThreadAnchorHead returns the head_sha of the anchored thread a
// comment belongs to (its own anchor, or its thread root's), or an invalid
// Text when the thread is not anchored.
//
// This is what makes an anchored question keep its own head for dedup: a
// reply asked about code that has since been pushed over must NOT merge into
// the run for the current head, which is reviewing different code.
func (h *Handler) commentThreadAnchorHead(ctx context.Context, workspaceID, commentID pgtype.UUID) pgtype.Text {
	if !commentID.Valid {
		return pgtype.Text{}
	}
	c, err := h.Queries.GetCommentInWorkspace(ctx, db.GetCommentInWorkspaceParams{ID: commentID, WorkspaceID: workspaceID})
	if err != nil {
		return pgtype.Text{}
	}
	if c.AnchorKind.Valid && c.AnchorHeadSha.Valid && c.AnchorHeadSha.String != "" {
		return c.AnchorHeadSha
	}
	if !c.ParentID.Valid {
		return pgtype.Text{}
	}
	root, err := h.Queries.GetThreadRoot(ctx, db.GetThreadRootParams{CommentID: commentID, WorkspaceID: workspaceID})
	if err != nil || !root.AnchorKind.Valid || !root.AnchorHeadSha.Valid || root.AnchorHeadSha.String == "" {
		return pgtype.Text{}
	}
	return root.AnchorHeadSha
}

// GetIssueAnchoredThreads: GET
// /api/issues/{id}/pull-requests/{prId}/anchored-threads[?sha=<sha>].
//
// Without `sha` it answers for every head, so a reviewer on the current head
// still sees the question someone asked about the revision before it — marked
// stale, but readable and repliable.
func (h *Handler) GetIssueAnchoredThreads(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	pr, ok := h.loadLinkedPR(r.Context(), issue, chi.URLParam(r, "prId"))
	if !ok {
		writeError(w, http.StatusNotFound, "pull request not found")
		return
	}
	rows, err := h.Queries.ListAnchoredThreadsForPr(r.Context(), db.ListAnchoredThreadsForPrParams{
		WorkspaceID: issue.WorkspaceID,
		IssueID:     issue.ID,
		PrID:        pr.id,
		HeadSha:     strings.TrimSpace(r.URL.Query().Get("sha")),
		MaxThreads:  anchoredThreadsCap,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load anchored threads")
		return
	}
	writeJSON(w, http.StatusOK, h.buildAnchoredThreads(r, issue, rows))
}

// buildAnchoredThreads groups the flat (root + descendants) row set the query
// returns back into threads, preserving the query's chronological order for
// both the threads and the replies inside each one.
func (h *Handler) buildAnchoredThreads(r *http.Request, issue db.Issue, rows []db.ListAnchoredThreadsForPrRow) anchoredThreadsResponse {
	comments := make([]db.Comment, 0, len(rows))
	ids := make([]pgtype.UUID, 0, len(rows))
	for _, row := range rows {
		comments = append(comments, row.Comment())
		ids = append(ids, row.ID)
	}
	byID := make(map[string]db.Comment, len(comments))
	for _, c := range comments {
		byID[uuidToString(c.ID)] = c
	}

	reactions := h.groupReactions(r, ids)
	attachments := h.groupAttachments(r, ids)
	cache := h.newHeadCache()

	out := anchoredThreadsResponse{Threads: []AnchoredThread{}}
	index := map[string]int{}
	for _, c := range comments {
		id := uuidToString(c.ID)
		resp := commentToResponse(c, reactions[id], attachments[id])
		if !c.ParentID.Valid {
			anchor := commentAnchorFromRow(c)
			stale := cache.stale(r.Context(), anchor)
			resp.Anchor = anchor
			resp.AnchorStale = stale
			index[id] = len(out.Threads)
			out.Threads = append(out.Threads, AnchoredThread{Root: resp, Replies: []CommentResponse{}, Anchor: anchor, AnchorStale: stale})
			continue
		}
		rootID, ok := commentRootID(id, byID)
		if !ok {
			continue
		}
		at, ok := index[rootID]
		if !ok {
			continue
		}
		resp.Anchor = out.Threads[at].Anchor
		resp.AnchorStale = out.Threads[at].AnchorStale
		out.Threads[at].Replies = append(out.Threads[at].Replies, resp)
	}
	return out
}

// applyCommentAnchor is the single-comment form of applyCommentAnchors, for the
// endpoints that answer with one row. A reply reaches the batched root lookup
// (a set of one), which is the "one extra lookup keyed by parent_id" case.
func (h *Handler) applyCommentAnchor(ctx context.Context, workspaceID pgtype.UUID, row db.Comment, resp *CommentResponse) {
	one := []CommentResponse{*resp}
	h.applyCommentAnchors(ctx, workspaceID, []db.Comment{row}, one)
	*resp = one[0]
}
