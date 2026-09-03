package handler

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// PR stack (F10): the issue plus everything that must land before it,
// walked through `blocks` edges upward with a depth cap and a cycle guard.

func callPRStack(t *testing.T, issueID string) PRStackResponse {
	t.Helper()
	var out PRStackResponse
	testutil.Call(t, testHandler.GetIssuePRStack, testutil.WithURLParams(
		newRequest(http.MethodGet, "/api/issues/"+issueID+"/pr-stack", nil), "id", issueID,
	)).Want(http.StatusOK).JSON(&out)
	return out
}

// blockerChain builds issues where ids[i+1] blocks ids[i]; ids[0] is the leaf.
func blockerChain(t *testing.T, label string, n int) []string {
	t.Helper()
	ids := make([]string, n)
	for i := range ids {
		ids[i] = dbfx.Issue(t, fmt.Sprintf("%s %d", label, i))
	}
	for i := 0; i+1 < n; i++ {
		callCreateDependency(t, ids[i+1], ids[i], "blocks").Want(http.StatusCreated)
	}
	return ids
}

func TestPRStackListsBlockersByDepth(t *testing.T) {
	conn := vcsConnection(t)
	ids := blockerChain(t, "stack depth", 3)
	vcsPR(t, conn, ids[1], 41, "passed")

	got := callPRStack(t, ids[0])
	if got.Truncated || got.Cyclic {
		t.Fatalf("stack = %+v, want neither truncated nor cyclic", got)
	}
	if len(got.Nodes) != 3 {
		t.Fatalf("nodes = %+v, want the issue and its two blockers", got.Nodes)
	}
	for i, n := range got.Nodes {
		if n.Depth != i || n.IssueID != ids[i] {
			t.Fatalf("node %d = %+v, want issue %s at depth %d", i, n, ids[i], i)
		}
	}
	if got.Nodes[0].Ready || got.Nodes[2].Ready {
		t.Errorf("issues without an open PR must not read ready: %+v", got.Nodes)
	}
	if !got.Nodes[1].Ready || len(got.Nodes[1].PRs) != 1 {
		t.Errorf("node 1 = %+v, want ready with its green MR", got.Nodes[1])
	}
}

func TestPRStackTruncatesPastTheCap(t *testing.T) {
	ids := blockerChain(t, "stack deep", prStackMaxDepth+3)
	got := callPRStack(t, ids[0])
	if !got.Truncated {
		t.Fatalf("stack of %d blockers must report truncation", prStackMaxDepth+2)
	}
	if len(got.Nodes) != prStackMaxDepth+1 {
		t.Fatalf("nodes = %d, want the issue plus %d levels", len(got.Nodes), prStackMaxDepth)
	}
}

func TestPRStackSurvivesCycle(t *testing.T) {
	ids := blockerChain(t, "stack cycle", 3)
	// Close the loop below the API's cycle guard: ids[0] blocks ids[2].
	dbfx.InsertNoID(t, "issue_dependency", testutil.Cols{
		"issue_id": ids[0], "depends_on_issue_id": ids[2], "type": "blocks",
	}, "issue_id = $1 AND depends_on_issue_id = $2", ids[0], ids[2])

	got := callPRStack(t, ids[0])
	if !got.Cyclic {
		t.Fatalf("stack = %+v, want the cycle flagged", got)
	}
	if len(got.Nodes) != 3 {
		t.Fatalf("nodes = %+v, want each issue once", got.Nodes)
	}
}
