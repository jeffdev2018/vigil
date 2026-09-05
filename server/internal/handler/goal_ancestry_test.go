package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// F22: the parent chain a claim carries. Lineages are built with dbfx.Issue
// and parent_issue_id. Its FK (001_init) rules out orphans but not cycles.

// issueChain builds depth issues, each the child of the previous, and returns
// their ids root first.
func issueChain(t *testing.T, label string, depth int, over ...testutil.Cols) []string {
	t.Helper()
	ids := make([]string, 0, depth)
	var parent string
	for i := 0; i < depth; i++ {
		cols := testutil.Cols{"description": label + " description " + string(rune('A'+i))}
		if parent != "" {
			cols["parent_issue_id"] = parent
		}
		for _, o := range over {
			for k, v := range o {
				cols[k] = v
			}
		}
		parent = dbfx.Issue(t, label+" level "+string(rune('A'+i)), cols)
		ids = append(ids, parent)
	}
	return ids
}

func resolveAncestry(t *testing.T, issueID string, includeStart bool) claimGoalAncestry {
	t.Helper()
	out, err := testHandler.resolveClaimGoalAncestry(context.Background(), parseUUID(issueID), parseUUID(testWorkspaceID), includeStart)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGoalAncestryOrdersRootToParentAndSkipsSelf(t *testing.T) {
	ids := issueChain(t, "ancestry order", 3)
	got := resolveAncestry(t, ids[2], false)

	if len(got.Nodes) != 2 || got.Omitted != 0 {
		t.Fatalf("nodes = %+v omitted = %d, want the two ancestors", got.Nodes, got.Omitted)
	}
	if !strings.HasSuffix(got.Nodes[0].Title, "level A") || got.Nodes[0].Depth != 2 {
		t.Errorf("first node = %+v, want the root at depth 2", got.Nodes[0])
	}
	if !strings.HasSuffix(got.Nodes[1].Title, "level B") || got.Nodes[1].Depth != 1 {
		t.Errorf("second node = %+v, want the direct parent at depth 1", got.Nodes[1])
	}
	if got.Nodes[1].Description != "ancestry order description B" {
		t.Errorf("description = %q, want the parent's", got.Nodes[1].Description)
	}
	for _, n := range got.Nodes {
		if strings.HasSuffix(n.Title, "level C") {
			t.Fatalf("the claimed issue itself must not be in its ancestry: %+v", got.Nodes)
		}
	}
}

func TestGoalAncestryRootIssueIsEmpty(t *testing.T) {
	root := dbfx.Issue(t, "ancestry root only")
	got := resolveAncestry(t, root, false)
	if len(got.Nodes) != 0 || got.Omitted != 0 {
		t.Fatalf("root issue ancestry = %+v, want none", got)
	}
	var resp AgentTaskResponse
	got.applyTo(&resp)
	if resp.GoalAncestry != nil {
		t.Fatalf("applyTo on an empty chain must leave the field omitted, got %+v", resp.GoalAncestry)
	}
}

func TestGoalAncestryIncludesStartForQuickCreate(t *testing.T) {
	ids := issueChain(t, "ancestry quick-create", 2)
	got := resolveAncestry(t, ids[1], true)
	if len(got.Nodes) != 2 || got.Nodes[1].Depth != 1 || !strings.HasSuffix(got.Nodes[1].Title, "level B") {
		t.Fatalf("nodes = %+v, want the parent-to-be as the nearest node", got.Nodes)
	}
}

func TestGoalAncestryTruncatesDeepLineage(t *testing.T) {
	ids := issueChain(t, "ancestry deep", 10) // 9 ancestors above the leaf
	got := resolveAncestry(t, ids[9], false)
	if len(got.Nodes) != goalAncestryMaxDepth {
		t.Fatalf("nodes = %d, want the %d-level cap", len(got.Nodes), goalAncestryMaxDepth)
	}
	if got.Omitted != 9-goalAncestryMaxDepth {
		t.Fatalf("omitted = %d, want %d levels declared", got.Omitted, 9-goalAncestryMaxDepth)
	}
	// The nearest ancestors survive; the far ones are the ones cut.
	if got.Nodes[len(got.Nodes)-1].Depth != 1 || !strings.HasSuffix(got.Nodes[len(got.Nodes)-1].Title, "level I") {
		t.Errorf("nearest node = %+v, want the direct parent", got.Nodes[len(got.Nodes)-1])
	}
}

func TestGoalAncestrySurvivesCycle(t *testing.T) {
	ids := issueChain(t, "ancestry cycle", 2)
	// Close the loop: root's parent is its own child.
	dbfx.Exec(t, `UPDATE issue SET parent_issue_id = $2 WHERE id = $1`, ids[0], ids[1])
	got := resolveAncestry(t, ids[1], false)
	if len(got.Nodes) != 1 || !strings.HasSuffix(got.Nodes[0].Title, "level A") {
		t.Fatalf("nodes = %+v, want the single real ancestor with the cycle cut", got.Nodes)
	}
}

func TestGoalAncestryStopsAtForeignWorkspace(t *testing.T) {
	foreign := dbfx.Workspace(t, "Ancestry foreign", "ancestry-foreign-"+uuid.NewString())
	foreignRoot := dbfx.Issue(t, "ancestry foreign root", testutil.Cols{"workspace_id": foreign})
	mid := dbfx.Issue(t, "ancestry mid", testutil.Cols{"parent_issue_id": foreignRoot})
	leaf := dbfx.Issue(t, "ancestry leaf", testutil.Cols{"parent_issue_id": mid})

	got := resolveAncestry(t, leaf, false)
	if len(got.Nodes) != 1 || got.Nodes[0].Title != "ancestry mid" {
		t.Fatalf("nodes = %+v, want the chain to stop at the workspace boundary", got.Nodes)
	}
}

func TestGoalAncestryHonoursByteCaps(t *testing.T) {
	huge := strings.Repeat("x", 3<<10)
	criteria := `["` + strings.Repeat("c", 500) + `", {"text": "` + strings.Repeat("d", 500) + `"}, 42]`
	ids := issueChain(t, "ancestry bytes", 6, testutil.Cols{
		"description":         huge,
		"acceptance_criteria": criteria,
	})
	got := resolveAncestry(t, ids[5], false)

	total := 0
	for _, n := range got.Nodes {
		if len(n.Description) > goalAncestryMaxNodeBytes {
			t.Errorf("node %s description = %d bytes, want <= %d", n.Identifier, len(n.Description), goalAncestryMaxNodeBytes)
		}
		if n.Title == "" || n.Identifier == "" {
			t.Errorf("node %+v lost its title or identifier", n)
		}
		total += len(n.Identifier) + len(n.Title) + len(n.Description)
		for _, c := range n.AcceptanceCriteria {
			total += len(c)
		}
	}
	if total > goalAncestryMaxTotalBytes {
		t.Fatalf("chain = %d bytes, want <= %d", total, goalAncestryMaxTotalBytes)
	}
	// The direct parent keeps its context; the far ancestors gave theirs up.
	nearest := got.Nodes[len(got.Nodes)-1]
	if nearest.Description == "" || len(nearest.AcceptanceCriteria) != 2 {
		t.Fatalf("nearest node = %+v, want description and both string-like criteria kept", nearest)
	}
	if got.Nodes[0].Description != "" {
		t.Errorf("root node still carries a description after the cap: %d bytes", len(got.Nodes[0].Description))
	}
}

func TestGoalAncestryIsDeterministic(t *testing.T) {
	ids := issueChain(t, "ancestry determinism", 4)
	first := resolveAncestry(t, ids[3], false)
	second := resolveAncestry(t, ids[3], false)
	if len(first.Nodes) != len(second.Nodes) {
		t.Fatalf("node counts differ: %d vs %d", len(first.Nodes), len(second.Nodes))
	}
	for i := range first.Nodes {
		if first.Nodes[i].Identifier != second.Nodes[i].Identifier ||
			first.Nodes[i].Title != second.Nodes[i].Title || first.Nodes[i].Description != second.Nodes[i].Description {
			t.Fatalf("node %d differs between claims: %+v vs %+v", i, first.Nodes[i], second.Nodes[i])
		}
	}
}

// TestClaimTaskCarriesGoalAncestry pins the wire: the claim of a depth-3 issue
// carries its two ancestors root first, and nothing else about the response
// changes shape.
func TestClaimTaskCarriesGoalAncestry(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	var agentID, runtimeID string
	dbfx.QueryRow(t, `SELECT id, runtime_id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID, &runtimeID)

	ids := issueChain(t, "ancestry claim", 3)
	dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": ids[2]})

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "test-claim-goal-ancestry")
	req = withURLParam(req, "runtimeId", runtimeID)
	var resp struct {
		Task *struct {
			IssueID             string             `json:"issue_id"`
			GoalAncestry        []GoalAncestryNode `json:"goal_ancestry"`
			GoalAncestryOmitted int                `json:"goal_ancestry_omitted"`
		} `json:"task"`
	}
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&resp)
	if resp.Task == nil || resp.Task.IssueID != ids[2] {
		t.Fatalf("claimed task = %+v, want the leaf issue", resp.Task)
	}
	if len(resp.Task.GoalAncestry) != 2 || resp.Task.GoalAncestry[0].Depth != 2 || resp.Task.GoalAncestry[1].Depth != 1 {
		t.Fatalf("goal_ancestry = %+v, want root then parent", resp.Task.GoalAncestry)
	}
	if resp.Task.GoalAncestryOmitted != 0 {
		t.Errorf("goal_ancestry_omitted = %d, want 0", resp.Task.GoalAncestryOmitted)
	}
}
