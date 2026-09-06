package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Comment threads anchored to a diff line (F07 / JEF-21).
//
// The format matrix lives here rather than in a handler test: it is a pure
// function and every caller renders the same string.

func TestAnchorSummaryLine(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		start, end int32
		sha, flag  string
		want       string
	}{
		{
			name: "single line collapses the range",
			path: "server/internal/handler/comment.go", start: 41, end: 41,
			sha:  "abc123",
			want: "Anchored at server/internal/handler/comment.go:41@abc123",
		},
		{
			name: "range keeps both ends",
			path: "packages/core/api/client.ts", start: 10, end: 14,
			sha:  "deadbeef",
			want: "Anchored at packages/core/api/client.ts:10-14@deadbeef",
		},
		{
			name: "review flag title is quoted after the location",
			path: "a.go", start: 3, end: 3, sha: "sha1", flag: "nil deref on empty slice",
			want: "Anchored at a.go:3@sha1 (flag: nil deref on empty slice)",
		},
		{
			// A head this build could not resolve must not print a bare "@":
			// the agent would read it as part of the path.
			name: "missing head drops the @ entirely",
			path: "a.go", start: 3, end: 3,
			want: "Anchored at a.go:3",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AnchorSummaryLine(tc.path, tc.start, tc.end, tc.sha, tc.flag); got != tc.want {
				t.Fatalf("AnchorSummaryLine = %q, want %q", got, tc.want)
			}
		})
	}
}

// anchorSummaryFixture builds a workspace + issue and returns the ids the
// summary tests need. It reuses the raw-SQL style of the other tests in this
// package, which cannot reach internal/testutil's handler fixtures.
type anchorSummaryFixture struct {
	workspaceID pgtype.UUID
	issueID     pgtype.UUID
	userID      pgtype.UUID
}

func createAnchorSummaryFixture(t *testing.T, ctx context.Context) anchorSummaryFixture {
	t.Helper()
	pool := newHeadShaDedupPool(t)
	suffix := time.Now().UnixNano()

	var userID, workspaceID, issueID string
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id`,
		"Anchor Summary", fmt.Sprintf("anchor-summary-%d@multica.ai", suffix)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ($1, $2, '', 'ANS') RETURNING id
	`, "Anchor Summary", fmt.Sprintf("anchor-summary-%d", suffix)).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'anchor summary issue', 'in_review', 'none', $2, 'member', $3, 0) RETURNING id
	`, workspaceID, userID, 980000+int(suffix%1000)).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	t.Cleanup(func() {
		c := context.Background()
		pool.Exec(c, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		pool.Exec(c, `DELETE FROM issue WHERE id = $1`, issueID)
		pool.Exec(c, `DELETE FROM workspace WHERE id = $1`, workspaceID)
		pool.Exec(c, `DELETE FROM "user" WHERE id = $1`, userID)
	})
	return anchorSummaryFixture{
		workspaceID: util.MustParseUUID(workspaceID),
		issueID:     util.MustParseUUID(issueID),
		userID:      util.MustParseUUID(userID),
	}
}

// insertAnchoredComment writes a comment row directly. parentID empty inserts a
// thread root; anchored=false leaves every anchor column NULL.
func insertAnchoredComment(t *testing.T, ctx context.Context, fx anchorSummaryFixture, content, parentID string, anchored bool) string {
	t.Helper()
	pool := newHeadShaDedupPool(t)
	var id string
	var parent any
	if parentID != "" {
		parent = parentID
	}
	kind, path, sha := any(nil), any(nil), any(nil)
	var start, end any
	if anchored {
		kind, path, sha = "diff_line", "server/internal/handler/comment.go", "abc1234"
		start, end = 41, 44
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id,
			anchor_kind, anchor_file_path, anchor_head_sha, anchor_line_start, anchor_line_end, anchor_side)
		VALUES ($1, $2, 'member', $3, $4, 'comment', $5, $6, $7, $8, $9, $10, 'new')
		RETURNING id
	`, util.UUIDToString(fx.issueID), util.UUIDToString(fx.workspaceID), util.UUIDToString(fx.userID),
		content, parent, kind, path, sha, start, end).Scan(&id); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	return id
}

// The run a reply triggers must know WHERE the question is about. The reply is
// exactly the comment that does not repeat the location, so resolving the
// anchor from the thread root is the case that matters.
func TestBuildCommentTriggerSummary_AnchoredThread(t *testing.T) {
	ctx := context.Background()
	pool := newHeadShaDedupPool(t)
	q := db.New(pool)
	svc := &TaskService{Queries: q}
	fx := createAnchorSummaryFixture(t, ctx)

	rootID := insertAnchoredComment(t, ctx, fx, "why is this branch here?", "", true)
	replyID := insertAnchoredComment(t, ctx, fx, "still unclear", rootID, false)
	plainID := insertAnchoredComment(t, ctx, fx, "unrelated thought", "", false)

	const wantLine = "Anchored at server/internal/handler/comment.go:41-44@abc1234"

	root := svc.buildCommentTriggerSummary(ctx, fx.workspaceID, util.MustParseUUID(rootID))
	if !root.Valid || root.String != "why is this branch here?\n"+wantLine {
		t.Fatalf("root summary = %q, want the content plus %q", root.String, wantLine)
	}

	reply := svc.buildCommentTriggerSummary(ctx, fx.workspaceID, util.MustParseUUID(replyID))
	if !reply.Valid || reply.String != "still unclear\n"+wantLine {
		t.Fatalf("reply summary = %q, want the reply content plus the ROOT's anchor %q", reply.String, wantLine)
	}

	plain := svc.buildCommentTriggerSummary(ctx, fx.workspaceID, util.MustParseUUID(plainID))
	if plain.String != "unrelated thought" {
		t.Fatalf("unanchored summary = %q, want the content unchanged", plain.String)
	}
}
