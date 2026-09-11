package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Inbox zero (K63): the Decision Cards waiting for me, options included,
// ordered like the Attention Inbox (K02 risk) then by SLA deadline (K35),
// capped at five with the total, so a phone can answer them one tap each
// through the ordinary respond endpoint (K01). A projection: nothing is
// stored.

const inboxDecisionsCap = 5

type InboxDecisionItem struct {
	InboxItemID     string                `json:"inbox_item_id"`
	IssueID         string                `json:"issue_id"`
	IssueIdentifier string                `json:"issue_identifier"`
	IssueTitle      string                `json:"issue_title"`
	RiskScore       int                   `json:"risk_score"`
	Decision        IssueDecisionResponse `json:"decision"`
}

// ListInboxDecisions: GET /api/inbox/decisions.
func (h *Handler) ListInboxDecisions(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	rows, err := h.Queries.ListAttentionInboxItems(r.Context(), db.ListAttentionInboxItemsParams{WorkspaceID: wsUUID, RecipientType: "member", RecipientID: parseUUID(userID)})
	if err != nil {
		slog.Warn("list inbox decisions failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to list decisions")
		return
	}
	now := time.Now()
	prefix := h.getIssuePrefix(r.Context(), wsUUID)
	// Two round trips for the whole page instead of two per row: the inbox
	// can hold 200 candidate rows for the five cards that get shown.
	type candidate struct {
		row        db.ListAttentionInboxItemsRow
		decisionID pgtype.UUID
	}
	seen := map[string]bool{}
	candidates := make([]candidate, 0)
	decisionIDs := make([]pgtype.UUID, 0)
	issueIDs := make([]pgtype.UUID, 0)
	for _, row := range rows {
		if row.Type != "decision_request" && row.Type != "decision_escalated" {
			continue
		}
		var details struct {
			DecisionID string `json:"decision_id"`
		}
		if json.Unmarshal(row.Details, &details) != nil || details.DecisionID == "" || seen[details.DecisionID] {
			continue
		}
		did, ok := decisionUUID(details.DecisionID)
		if !ok {
			continue
		}
		seen[details.DecisionID] = true
		candidates = append(candidates, candidate{row: row, decisionID: did})
		decisionIDs = append(decisionIDs, did)
		issueIDs = append(issueIDs, row.IssueID)
	}
	decisions := map[string]db.IssueDecision{}
	issues := map[string]db.Issue{}
	if len(candidates) > 0 {
		drows, err := h.Queries.ListIssueDecisionsByIDs(r.Context(), db.ListIssueDecisionsByIDsParams{Ids: decisionIDs, IssueIds: issueIDs})
		if err != nil {
			slog.Warn("list inbox decisions: resolve decisions failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to list decisions")
			return
		}
		for _, d := range drows {
			decisions[uuidToString(d.ID)] = d
		}
		irows, err := h.Queries.ListIssuesByIDsInWorkspace(r.Context(), db.ListIssuesByIDsInWorkspaceParams{WorkspaceID: wsUUID, IssueIds: issueIDs})
		if err != nil {
			slog.Warn("list inbox decisions: resolve issues failed", append(logger.RequestAttrs(r), "error", err)...)
		}
		for _, issue := range irows {
			issues[uuidToString(issue.ID)] = issue
		}
	}
	items := make([]InboxDecisionItem, 0)
	for _, c := range candidates {
		row := c.row
		decision, ok := decisions[uuidToString(c.decisionID)]
		// The card names one issue; a decision that moved to another issue is not it.
		if !ok || decision.IssueID != row.IssueID || len(decision.Response) > 0 {
			continue
		}
		base := db.ListInboxItemsRow(row)
		score, _ := attentionScore(base, now)
		item := InboxDecisionItem{InboxItemID: uuidToString(row.ID), IssueID: uuidToString(row.IssueID), IssueTitle: row.Title, RiskScore: score, Decision: issueDecisionToResponse(decision)}
		item.Decision.Learned = h.decisionHint(r.Context(), wsUUID, userID, decision)
		if issue, ok := issues[uuidToString(row.IssueID)]; ok {
			item.IssueIdentifier = prefix + "-" + strconv.Itoa(int(issue.Number))
			item.IssueTitle = issue.Title
		}
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.RiskScore != b.RiskScore {
			return a.RiskScore > b.RiskScore
		}
		da, db := a.Decision.SlaDeadlineAt, b.Decision.SlaDeadlineAt
		if (da == nil) != (db == nil) {
			return da != nil
		}
		if da != nil && *da != *db {
			return *da < *db
		}
		return a.Decision.CreatedAt < b.Decision.CreatedAt
	})
	total := len(items)
	if len(items) > inboxDecisionsCap {
		items = items[:inboxDecisionsCap]
	}
	writeJSON(w, http.StatusOK, map[string]any{"decisions": items, "total": total})
}

func decisionUUID(s string) (pgtype.UUID, bool) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, false
	}
	return u, true
}
