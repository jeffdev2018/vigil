package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Goal ancestry (F22): the chain of parent issues a claimed run descends from,
// root first, so an agent on a depth-3 sub-issue knows the objective it serves.
// Bytes bound tokens, so the caps are in bytes (same discipline as
// SourceContextMaxAgentInputBytes).
const (
	// goalAncestryMaxDepth is how many ancestors a claim carries.
	goalAncestryMaxDepth = 5
	// goalAncestryFetchDepth is how far the walk looks past the cap, only to
	// report how many levels were left out. Deeper lineages count as "more".
	goalAncestryFetchDepth = 32
	// goalAncestryMaxTotalBytes caps the whole chain; descriptions are dropped
	// from the farthest ancestor toward the direct parent until it fits.
	goalAncestryMaxTotalBytes = 8 << 10
	// goalAncestryMaxNodeBytes caps one ancestor's description.
	goalAncestryMaxNodeBytes = 2 << 10
	// goalAncestryMaxCriteria caps acceptance criteria per ancestor.
	goalAncestryMaxCriteria = 12
)

// GoalAncestryNode is one ancestor as the daemon writes it into the brief.
// Depth counts from the claimed issue: 1 is the direct parent.
type GoalAncestryNode struct {
	Identifier         string   `json:"identifier"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	Depth              int      `json:"depth"`
}

type claimGoalAncestry struct {
	Nodes []GoalAncestryNode
	// Omitted is how many ancestor levels beyond goalAncestryMaxDepth were
	// dropped, so the brief can say the chain is cut rather than complete.
	Omitted int
}

func (c claimGoalAncestry) applyTo(resp *AgentTaskResponse) {
	if len(c.Nodes) == 0 {
		return
	}
	resp.GoalAncestry = c.Nodes
	resp.GoalAncestryOmitted = c.Omitted
}

// resolveClaimGoalAncestry walks issue.parent_issue_id upward from issueID.
// With includeStart the start issue itself is the nearest node (quick-create
// files its new issue under it). A parent in another workspace ends the chain;
// a cycle (the FK allows one) is cut at its first repeated id.
func (h *Handler) resolveClaimGoalAncestry(ctx context.Context, issueID, workspaceID pgtype.UUID, includeStart bool) (claimGoalAncestry, error) {
	rows, err := h.Queries.ListIssueAncestors(ctx, db.ListIssueAncestorsParams{
		IssueID:     issueID,
		WorkspaceID: workspaceID,
		MaxDepth:    goalAncestryFetchDepth,
	})
	if err != nil {
		return claimGoalAncestry{}, fmt.Errorf("list issue ancestors: %w", err)
	}
	prefix := h.getIssuePrefix(ctx, workspaceID)

	type raw struct {
		id       pgtype.UUID
		number   int32
		title    string
		desc     pgtype.Text
		criteria []byte
	}
	var chain []raw // nearest first
	if includeStart {
		start, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: issueID, WorkspaceID: workspaceID})
		if err != nil {
			return claimGoalAncestry{}, fmt.Errorf("get start issue: %w", err)
		}
		chain = append(chain, raw{start.ID, start.Number, start.Title, start.Description, start.AcceptanceCriteria})
	}
	seen := map[string]bool{uuidToString(issueID): true}
	for _, r := range rows {
		key := uuidToString(r.ID)
		if seen[key] {
			break // cycle: the rest of the walk repeats
		}
		seen[key] = true
		chain = append(chain, raw{r.ID, r.Number, r.Title, r.Description, r.AcceptanceCriteria})
	}

	out := claimGoalAncestry{}
	if len(chain) > goalAncestryMaxDepth {
		out.Omitted = len(chain) - goalAncestryMaxDepth
		chain = chain[:goalAncestryMaxDepth]
	}
	// Root first: reverse the nearest-first walk.
	for i := len(chain) - 1; i >= 0; i-- {
		n := chain[i]
		out.Nodes = append(out.Nodes, GoalAncestryNode{
			Identifier:         prefix + "-" + strconv.Itoa(int(n.number)),
			Title:              n.title,
			Description:        truncateUTF8(n.desc.String, goalAncestryMaxNodeBytes),
			AcceptanceCriteria: acceptanceCriteriaLines(n.criteria),
			Depth:              i + 1,
		})
	}
	capGoalAncestryBytes(out.Nodes)
	return out, nil
}

// capGoalAncestryBytes enforces goalAncestryMaxTotalBytes by shedding, from the
// farthest ancestor toward the direct parent, first the criteria then the
// description. Identifier and title are never cut: the chain of names is the
// part that must survive.
func capGoalAncestryBytes(nodes []GoalAncestryNode) {
	size := func() int {
		n := 0
		for _, node := range nodes {
			n += len(node.Identifier) + len(node.Title) + len(node.Description)
			for _, c := range node.AcceptanceCriteria {
				n += len(c)
			}
		}
		return n
	}
	for i := range nodes {
		if size() <= goalAncestryMaxTotalBytes {
			return
		}
		nodes[i].AcceptanceCriteria = nil
		if size() <= goalAncestryMaxTotalBytes {
			return
		}
		nodes[i].Description = ""
	}
}

// acceptanceCriteriaLines renders the free-form acceptance_criteria JSONB as
// plain lines: a JSON array of strings, or of objects with a text / title /
// description field. Anything else is skipped rather than invented.
func acceptanceCriteriaLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(b, &items); err != nil {
		return nil
	}
	var out []string
	for _, item := range items {
		if len(out) >= goalAncestryMaxCriteria {
			break
		}
		var s string
		if err := json.Unmarshal(item, &s); err == nil {
			if s != "" {
				out = append(out, truncateUTF8(s, goalAncestryMaxNodeBytes))
			}
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(item, &obj); err != nil {
			continue
		}
		for _, key := range []string{"text", "title", "description"} {
			if v, ok := obj[key].(string); ok && v != "" {
				out = append(out, truncateUTF8(v, goalAncestryMaxNodeBytes))
				break
			}
		}
	}
	return out
}

// truncateUTF8 cuts s to at most max bytes without splitting a rune, marking
// the cut with an ellipsis so the reader knows text is missing.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	const mark = "…"
	cut := max - len(mark)
	if cut <= 0 {
		return ""
	}
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + mark
}
