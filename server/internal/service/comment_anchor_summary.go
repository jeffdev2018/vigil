package service

import (
	"context"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Comment threads anchored to a diff line (F07 / JEF-21) — the agent's side.
//
// A run triggered by an anchored thread is answering a question about a
// specific place in a specific revision of a diff. The trigger summary is the
// one line the agent sees before it reads anything, so the place goes there:
// without it the run starts from "why is this wrong?" with no idea what "this"
// is, and has to guess from the surrounding files.

// AnchorSummaryLine is the trailing line appended to an anchored thread's
// trigger summary. Format:
//
//	Anchored at <path>:<line_start>[-<line_end>][@<sha>] [(flag: <title>)]
//
// The range collapses to a single number when start == end, because
// "path:41-41" reads as a range the author did not ask for.
func AnchorSummaryLine(path string, lineStart, lineEnd int32, sha, flagTitle string) string {
	var b strings.Builder
	b.WriteString("Anchored at ")
	b.WriteString(path)
	b.WriteString(":")
	b.WriteString(strconv.Itoa(int(lineStart)))
	if lineEnd > lineStart {
		b.WriteString("-")
		b.WriteString(strconv.Itoa(int(lineEnd)))
	}
	if sha != "" {
		b.WriteString("@")
		b.WriteString(sha)
	}
	if flagTitle != "" {
		b.WriteString(" (flag: ")
		b.WriteString(flagTitle)
		b.WriteString(")")
	}
	return b.String()
}

// anchorSummarySuffix returns the anchor line for the thread `comment` belongs
// to, or "" when the thread is not anchored.
//
// The anchor lives on the thread ROOT, so a REPLY resolves it by walking up —
// which is the case that matters most: the reply is what triggers the run, and
// it is usually the one comment that does not repeat where it is about.
//
// Every failure degrades to "": a summary without the anchor is a worse prompt,
// a summary that fails to build is no prompt at all.
func (s *TaskService) anchorSummarySuffix(ctx context.Context, workspaceID pgtype.UUID, comment db.Comment) string {
	root := comment
	if !root.AnchorKind.Valid && comment.ParentID.Valid {
		found, err := s.Queries.GetThreadRoot(ctx, db.GetThreadRootParams{
			CommentID:   comment.ID,
			WorkspaceID: workspaceID,
		})
		if err != nil {
			return ""
		}
		root = found
	}
	if !root.AnchorKind.Valid || !root.AnchorFilePath.Valid || root.AnchorFilePath.String == "" {
		return ""
	}
	flagTitle := ""
	if root.AnchorReviewFlagID.Valid {
		// The flag's title is what the reviewer actually wrote; the id would
		// tell the agent nothing it can act on.
		if flag, err := s.Queries.GetReviewFlag(ctx, root.AnchorReviewFlagID); err == nil {
			flagTitle = flag.Title
		}
	}
	return AnchorSummaryLine(
		root.AnchorFilePath.String,
		root.AnchorLineStart.Int32,
		root.AnchorLineEnd.Int32,
		root.AnchorHeadSha.String,
		flagTitle,
	)
}
