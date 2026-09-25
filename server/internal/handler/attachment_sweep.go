package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Orphaned attachment sweep (JEF-292). An attachment uploaded into an issue
// description or a comment is only useful while the markdown still points at
// it — once the text is edited to remove the reference, the row and its
// storage object are dead weight. This sweep finds those, marks them with a
// grace period (so a fast delete-then-undo edit is never punished), and
// deletes what stays orphaned.
//
// Scope: only issue-owned (comment_id IS NULL, issue_id NOT NULL) and
// comment-owned (comment_id NOT NULL) attachments are considered. Every
// other owner type is skipped outright, on purpose:
//   - chat (chat_session_id/chat_message_id): a chat message attachment is
//     part of the immutable transcript, not a markdown reference that can be
//     edited out.
//   - note (note_id): Brain notes have their own lifecycle (archive, curation
//     rewrite) that does not go through this sweep; verifying the same
//     reference rule applies there is unreviewed and out of scope for JEF-292.
//   - source_context_id: captured-context snapshots are immutable historical
//     copies by design (see DeleteAttachment's SourceContextID guard) and are
//     already reclaimed by source_context_sweeper.go.
//   - capture_id, task_id-only (unbound uploads): not markdown-referenced
//     content at all.
const (
	// attachmentSweepScanBatchSize bounds how many candidate attachments each
	// mark/unmark pass inspects per tick, per owner kind.
	attachmentSweepScanBatchSize = 200
	// attachmentSweepDeleteBatchSize bounds how many attachments the deletion
	// pass removes per tick, so a large backlog drains gradually instead of
	// holding storage + DB work for one long round.
	attachmentSweepDeleteBatchSize = 50
	// attachmentUnreferencedRetention is the grace period between an
	// attachment first being found unreferenced and the sweep deleting it.
	attachmentUnreferencedRetention = 7 * 24 * time.Hour
)

// SweepUnreferencedAttachments runs one round of the orphan sweep: mark,
// unmark, then delete what has been unreferenced longer than the retention
// window. Errors from each stage are logged here (never swallowed) and
// joined into the returned error so a caller with its own logging can act on
// it too; a failure in one stage does not stop the others from running.
func (h *Handler) SweepUnreferencedAttachments(ctx context.Context) (marked, unmarked, deleted int, err error) {
	m, u, markErr := h.sweepIssueOwnedAttachments(ctx)
	marked += m
	unmarked += u
	if markErr != nil {
		slog.Warn("attachment sweep: issue-owned mark pass failed", "error", markErr)
	}

	m, u, commentErr := h.sweepCommentOwnedAttachments(ctx)
	marked += m
	unmarked += u
	if commentErr != nil {
		slog.Warn("attachment sweep: comment-owned mark pass failed", "error", commentErr)
	}

	d, delErr := h.sweepDeleteExpiredAttachments(ctx)
	deleted += d
	if delErr != nil {
		slog.Warn("attachment sweep: delete pass failed", "error", delErr)
	}

	return marked, unmarked, deleted, errors.Join(markErr, commentErr, delErr)
}

// sweepIssueOwnedAttachments handles attachments dropped directly on an
// issue (comment_id IS NULL). Referenced content is the issue description
// OR any comment on that same issue — a user can paste the stable download
// link into a reply instead of editing the description.
func (h *Handler) sweepIssueOwnedAttachments(ctx context.Context) (marked, unmarked int, err error) {
	candidates, err := h.Queries.ListIssueOwnedAttachmentsForSweep(ctx, attachmentSweepScanBatchSize)
	if err != nil {
		return 0, 0, fmt.Errorf("list issue-owned candidates: %w", err)
	}
	if len(candidates) == 0 {
		return 0, 0, nil
	}

	issueIDs := make([]pgtype.UUID, 0, len(candidates))
	seen := map[string]bool{}
	for _, c := range candidates {
		key := uuidToString(c.IssueID)
		if seen[key] {
			continue
		}
		seen[key] = true
		issueIDs = append(issueIDs, c.IssueID)
	}
	commentRows, err := h.Queries.ListCommentContentsByIssueIDs(ctx, issueIDs)
	if err != nil {
		return 0, 0, fmt.Errorf("list comment contents: %w", err)
	}
	commentsByIssue := make(map[string][]string, len(issueIDs))
	for _, row := range commentRows {
		key := uuidToString(row.IssueID)
		commentsByIssue[key] = append(commentsByIssue[key], row.Content)
	}

	var stageErr error
	for _, c := range candidates {
		haystack := c.Description.String
		if pieces := commentsByIssue[uuidToString(c.IssueID)]; len(pieces) > 0 {
			haystack = haystack + "\n" + strings.Join(pieces, "\n")
		}
		referenced := attachmentReferenced(haystack, c.ID, c.Url)
		wasUnreferenced := c.UnreferencedSince.Valid
		if referenced != wasUnreferenced {
			// referenced && !wasUnreferenced already correct, or
			// !referenced && wasUnreferenced already correct — nothing to do.
			continue
		}
		if referenced {
			if err := h.Queries.ClearAttachmentUnreferenced(ctx, c.ID); err != nil {
				stageErr = errors.Join(stageErr, fmt.Errorf("clear attachment %s: %w", uuidToString(c.ID), err))
				continue
			}
			unmarked++
		} else {
			if err := h.Queries.MarkAttachmentUnreferenced(ctx, c.ID); err != nil {
				stageErr = errors.Join(stageErr, fmt.Errorf("mark attachment %s: %w", uuidToString(c.ID), err))
				continue
			}
			marked++
		}
	}
	return marked, unmarked, stageErr
}

// sweepCommentOwnedAttachments handles attachments dropped on a comment
// (comment_id NOT NULL). Only that comment's own content is checked — see
// the package doc comment above for why issue-owned attachments look wider.
func (h *Handler) sweepCommentOwnedAttachments(ctx context.Context) (marked, unmarked int, err error) {
	candidates, err := h.Queries.ListCommentOwnedAttachmentsForSweep(ctx, attachmentSweepScanBatchSize)
	if err != nil {
		return 0, 0, fmt.Errorf("list comment-owned candidates: %w", err)
	}

	var stageErr error
	for _, c := range candidates {
		referenced := attachmentReferenced(c.Content, c.ID, c.Url)
		wasUnreferenced := c.UnreferencedSince.Valid
		if referenced != wasUnreferenced {
			// Already in the correct state — nothing to do.
			continue
		}
		if referenced {
			if err := h.Queries.ClearAttachmentUnreferenced(ctx, c.ID); err != nil {
				stageErr = errors.Join(stageErr, fmt.Errorf("clear attachment %s: %w", uuidToString(c.ID), err))
				continue
			}
			unmarked++
		} else {
			if err := h.Queries.MarkAttachmentUnreferenced(ctx, c.ID); err != nil {
				stageErr = errors.Join(stageErr, fmt.Errorf("mark attachment %s: %w", uuidToString(c.ID), err))
				continue
			}
			marked++
		}
	}
	return marked, unmarked, stageErr
}

// sweepDeleteExpiredAttachments removes attachments that have stayed
// unreferenced past attachmentUnreferencedRetention, through the same
// storage+row path DELETE /api/attachments/{id} uses (see
// deleteAttachmentStorageAndRow in file.go). One failure does not stop the
// rest of the batch.
func (h *Handler) sweepDeleteExpiredAttachments(ctx context.Context) (deleted int, err error) {
	cutoff := pgtype.Timestamptz{Time: time.Now().Add(-attachmentUnreferencedRetention), Valid: true}
	expired, err := h.Queries.ListAttachmentsUnreferencedSince(ctx, db.ListAttachmentsUnreferencedSinceParams{
		Cutoff:     cutoff,
		LimitCount: attachmentSweepDeleteBatchSize,
	})
	if err != nil {
		return 0, fmt.Errorf("list expired attachments: %w", err)
	}

	var stageErr error
	for _, att := range expired {
		if _, err := h.deleteAttachmentStorageAndRow(ctx, att); err != nil {
			stageErr = errors.Join(stageErr, fmt.Errorf("delete attachment %s: %w", uuidToString(att.ID), err))
			continue
		}
		deleted++
	}
	return deleted, stageErr
}

// attachmentReferenced reports whether content still contains a reference to
// the given attachment — either the stable /api/attachments/{id}/download
// path or the raw storage URL (with or without its query/fragment).
// Mirrors packages/core/types/attachment-url.ts#contentReferencesAttachment;
// the server never persists download_url/markdown_url (those are computed
// at read time from the same stored url), so only these two forms apply here.
func attachmentReferenced(content string, attachmentID pgtype.UUID, rawURL string) bool {
	if content == "" {
		return false
	}
	if strings.Contains(content, util.AttachmentDownloadPath(uuidToString(attachmentID))) {
		return true
	}
	if rawURL == "" {
		return false
	}
	if strings.Contains(content, rawURL) {
		return true
	}
	if stable := stripURLQueryAndFragment(rawURL); stable != "" && strings.Contains(content, stable) {
		return true
	}
	return false
}

func stripURLQueryAndFragment(raw string) string {
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		return raw[:i]
	}
	return raw
}
