package handler

import (
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// JEF-292: the orphaned attachment sweep marks issue/comment attachments no
// longer referenced by their owning content, unmarks them if referenced
// again, and deletes what stays unreferenced past the retention window.

func attachmentUnreferencedSince(t *testing.T, attachmentID string) bool {
	t.Helper()
	var valid bool
	if err := testPool.QueryRow(context.Background(),
		`SELECT unreferenced_since IS NOT NULL FROM attachment WHERE id = $1`, attachmentID).Scan(&valid); err != nil {
		t.Fatalf("read unreferenced_since: %v", err)
	}
	return valid
}

func attachmentExists(t *testing.T, attachmentID string) bool {
	t.Helper()
	var n int
	if err := testPool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM attachment WHERE id = $1`, attachmentID).Scan(&n); err != nil {
		t.Fatalf("count attachment: %v", err)
	}
	return n > 0
}

func TestAttachmentSweepIssueOwned_ReferencedStaysUnmarked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	attID := dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": testWorkspaceID, "uploader_type": "member", "uploader_id": testUserID,
		"filename": "shot.png", "url": "https://example.test/sweep-ref.png",
		"content_type": "image/png", "size_bytes": 1,
	})
	issueID := dbfx.Issue(t, "sweep referenced issue", testutil.Cols{
		"description": "See the attached ![shot](/api/attachments/" + attID + "/download).",
	})
	dbfx.Exec(t, `UPDATE attachment SET issue_id = $1 WHERE id = $2`, issueID, attID)

	marked, _, _, err := testHandler.SweepUnreferencedAttachments(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if marked != 0 {
		t.Errorf("marked = %d, want 0 (referenced attachment must not be marked)", marked)
	}
	if attachmentUnreferencedSince(t, attID) {
		t.Error("referenced attachment was marked unreferenced")
	}
}

func TestAttachmentSweepIssueOwned_UnreferencedGetsMarked(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	attID := dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": testWorkspaceID, "uploader_type": "member", "uploader_id": testUserID,
		"filename": "shot.png", "url": "https://example.test/sweep-orphan.png",
		"content_type": "image/png", "size_bytes": 1,
	})
	issueID := dbfx.Issue(t, "sweep orphan issue", testutil.Cols{"description": "nothing here"})
	dbfx.Exec(t, `UPDATE attachment SET issue_id = $1 WHERE id = $2`, issueID, attID)

	if _, _, _, err := testHandler.SweepUnreferencedAttachments(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if !attachmentUnreferencedSince(t, attID) {
		t.Fatal("unreferenced attachment was not marked")
	}

	// Referenced again (pasted into a reply): the next round must clear it.
	dbfx.Comment(t, issueID, "actually see /api/attachments/"+attID+"/download")
	if _, _, _, err := testHandler.SweepUnreferencedAttachments(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if attachmentUnreferencedSince(t, attID) {
		t.Error("re-referenced attachment (via a comment) was not unmarked")
	}
}

func TestAttachmentSweepCommentOwned_ReferencedAndUnreferenced(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := dbfx.Issue(t, "sweep comment issue")
	commentID := dbfx.Comment(t, issueID, "see the file")

	refID := dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": issueID, "comment_id": commentID,
		"uploader_type": "member", "uploader_id": testUserID,
		"filename": "a.png", "url": "https://example.test/sweep-comment-ref.png",
		"content_type": "image/png", "size_bytes": 1,
	})
	dbfx.Exec(t, `UPDATE comment SET content = $1 WHERE id = $2`,
		"see /api/attachments/"+refID+"/download", commentID)

	orphanID := dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": issueID, "comment_id": commentID,
		"uploader_type": "member", "uploader_id": testUserID,
		"filename": "b.png", "url": "https://example.test/sweep-comment-orphan.png",
		"content_type": "image/png", "size_bytes": 1,
	})

	if _, _, _, err := testHandler.SweepUnreferencedAttachments(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if attachmentUnreferencedSince(t, refID) {
		t.Error("referenced comment attachment was marked unreferenced")
	}
	if !attachmentUnreferencedSince(t, orphanID) {
		t.Error("unreferenced comment attachment was not marked")
	}
}

func TestAttachmentSweepDeletesAfterRetention(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	issueID := dbfx.Issue(t, "sweep delete issue", testutil.Cols{"description": "nothing here"})
	attID := dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": testWorkspaceID, "issue_id": issueID,
		"uploader_type": "member", "uploader_id": testUserID,
		"filename": "old.png", "url": "https://example.test/sweep-expired.png",
		"content_type": "image/png", "size_bytes": 1,
	})

	// Mark it unreferenced 8 days ago — past the 7-day retention window.
	dbfx.Exec(t, `UPDATE attachment SET unreferenced_since = now() - interval '8 days' WHERE id = $1`, attID)

	_, _, deleted, err := testHandler.SweepUnreferencedAttachments(context.Background())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if attachmentExists(t, attID) {
		t.Error("attachment past retention was not deleted")
	}
}

func TestAttachmentSweepDoesNotTouchOtherOwnerTypes(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	// A chat-owned attachment (no issue_id, no comment_id): never referenced
	// by any markdown content, must stay untouched by the sweep.
	chatID := dbfx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": testWorkspaceID, "uploader_type": "member", "uploader_id": testUserID,
		"filename": "chat.png", "url": "https://example.test/sweep-chat.png",
		"content_type": "image/png", "size_bytes": 1,
	})

	if _, _, _, err := testHandler.SweepUnreferencedAttachments(context.Background()); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if attachmentUnreferencedSince(t, chatID) {
		t.Error("out-of-scope attachment (no issue_id/comment_id) was marked by the sweep")
	}
	if !attachmentExists(t, chatID) {
		t.Error("out-of-scope attachment was deleted by the sweep")
	}
}
