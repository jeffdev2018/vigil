package handler

import (
	"context"
	"log/slog"
	"strings"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/decisions"
)

// The auto-ML suggestion (K61) is a nearest-neighbour vote: an item is matched
// against its ten closest resolved neighbours by Postgres full text, and the
// winning state's share of the ranking weight becomes "confidence". That share
// is a ratio of lexical ranking scores, not a probability — two items about the
// same thing in different words are not neighbours, and two unrelated items
// that share vocabulary are. Above the workspace threshold the queue accepts or
// dismisses on its own, so that ratio decides whether work is created or thrown
// away without anybody looking.
//
// A decision model gives a second, independent read of the same item: it judges
// what the item says rather than which words it shares. This does not replace
// the vote — the vote carries what this workspace actually decided in the past,
// which no model knows — it gates the automatic part of it. Both must agree
// before the queue acts by itself. When they disagree, the suggestion is still
// shown; a person decides, which is where an ambiguous item belonged anyway.
//
// Failure is closed on purpose. A deployment that configured a decision
// endpoint has asked for that second read, so losing it means the check did not
// happen — and a check that did not happen must not be reported as passed on a
// path that creates or discards work unattended. The item waits for a human,
// which costs one triage click; the alternative silently widens automation the
// moment the endpoint has a bad minute.

// triageDecisionFloor is how sure the second read has to be to let the queue
// act. Higher than the goal judge's floor because the mistake is worse: a
// wrongly dismissed delivery is work that disappears without anybody seeing
// it, while a wrongly continued goal loop costs one bounded run.
const triageDecisionFloor = 0.75

// triageItemBodyCap bounds what leaves the deployment for this check. The
// title and body are enough to judge an item; the raw payload is not sent at
// all, because a webhook or email payload carries addresses, identifiers and
// customer content that add nothing to "should this be accepted?".
const triageItemBodyCap = 4000

const triageAcceptInstructions = "Should this incoming item become a real issue for this team to work on? Judge what the item says, and use the team's past decisions on similar items as the guide to their bar. True means accept it as work; false means it is noise, a duplicate, an automated notice, or otherwise not work for this team."

// triageSecondRead is the outcome of the decision model's read, recorded in
// the audit entry so a disagreement is explainable after the fact.
type triageSecondRead struct {
	// Asked is false when no decision endpoint is configured, which leaves
	// the vote alone as it was before this check existed.
	Asked bool `json:"asked"`
	// Agrees is true when the second read matches the vote's suggestion with
	// enough confidence to act.
	Agrees bool `json:"agrees"`
	// Probability is the model's probability that the item should be
	// accepted, kept whichever way the verdict went.
	Probability float64 `json:"probability,omitempty"`
	// Why names what happened when Agrees is false: unavailable, or the
	// disagreement itself.
	Why string `json:"why,omitempty"`
}

// mayAutoApply reports whether the queue may act on its own. It returns the
// second read for the audit entry either way.
func (h *Handler) triageMayAutoApply(ctx context.Context, item db.TriageItem, s TriageSuggestion) (bool, triageSecondRead) {
	if !h.Decisions.Enabled() {
		return true, triageSecondRead{}
	}
	read := triageSecondRead{Asked: true}

	state := map[string]any{
		"item_title": item.Title,
		"vote": map[string]any{
			"suggests":   s.Suggested,
			"confidence": s.Confidence,
			"from":       len(s.Neighbors),
		},
	}
	if strings.TrimSpace(item.BodyMarkdown) != "" {
		state["item_body"] = util.TruncateUTF8Bytes(item.BodyMarkdown, triageItemBodyCap)
	}
	if len(s.Neighbors) > 0 {
		past := make([]map[string]string, 0, len(s.Neighbors))
		for _, n := range s.Neighbors {
			past = append(past, map[string]string{"title": n.Title, "the_team_chose": n.State})
		}
		state["similar_items_the_team_already_decided"] = past
	}

	res, err := h.Decisions.Ask(ctx, state, map[string]decisions.Question{
		"accept": decisions.Noul(triageAcceptInstructions,
			"accept it as work for this team",
			"noise, duplicate, automated notice, or not this team's work"),
	})
	if err != nil {
		read.Why = "the decision endpoint did not answer"
		slog.Warn("triage auto: second read unavailable, leaving the item to a human",
			"item_id", uuidToString(item.ID), "error", err)
		return false, read
	}
	probability, ok := res.NoulIn("accept")
	if !ok {
		read.Why = "the decision endpoint answered nothing usable"
		return false, read
	}
	read.Probability = probability

	// The vote and the read have to point the same way, each confidently.
	switch s.Suggested {
	case "accept":
		read.Agrees = probability >= triageDecisionFloor
	case "dismiss":
		read.Agrees = probability <= 1-triageDecisionFloor
	}
	if !read.Agrees {
		read.Why = "the second read disagrees with the vote"
	}
	return read.Agrees, read
}
