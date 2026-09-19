package handler

import (
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// TestAddIssueReactionRejectsOverlongEmoji covers the audit finding that
// AddIssueReaction/RemoveIssueReaction stored req.Emoji with only a
// non-empty check, no length or format bound, and no http.MaxBytesReader —
// an authenticated member of the issue could store an arbitrarily long
// string as a "reaction".
func TestAddIssueReactionRejectsOverlongEmoji(t *testing.T) {
	issueID := dbfx.Issue(t, "reaction validation "+t.Name())

	req := newRequest(http.MethodPost, "/api/issues/"+issueID+"/reactions", map[string]any{
		"emoji": strings.Repeat("a", issueReactionMaxEmojiRunes+1),
	})
	req = withURLParam(req, "id", issueID)
	testutil.Call(t, testHandler.AddIssueReaction, req).Want(http.StatusBadRequest)

	if n := dbfx.Count(t, `SELECT count(*) FROM issue_reaction WHERE issue_id = $1`, issueID); n != 0 {
		t.Fatalf("reaction rows after rejected emoji = %d, want 0", n)
	}

	// A normal emoji still works.
	req = newRequest(http.MethodPost, "/api/issues/"+issueID+"/reactions", map[string]any{"emoji": "👍"})
	req = withURLParam(req, "id", issueID)
	testutil.Call(t, testHandler.AddIssueReaction, req).Want(http.StatusCreated)
}
