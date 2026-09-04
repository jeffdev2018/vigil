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
	seen := map[string]bool{}
	items := make([]InboxDecisionItem, 0)
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
		decision, err := h.Queries.GetIssueDecision(r.Context(), db.GetIssueDecisionParams{ID: did, IssueID: row.IssueID})
		if err != nil || len(decision.Response) > 0 {
			continue
		}
		seen[details.DecisionID] = true
		base := db.ListInboxItemsRow(row)
		score, _ := attentionScore(base, now)
		item := InboxDecisionItem{InboxItemID: uuidToString(row.ID), IssueID: uuidToString(row.IssueID), IssueTitle: row.Title, RiskScore: score, Decision: issueDecisionToResponse(decision)}
		if issue, err := h.Queries.GetIssue(r.Context(), row.IssueID); err == nil {
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
