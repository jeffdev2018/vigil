package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// Skill Miner (K58). A member comment that closely follows an agent comment
// on the same issue is a correction signal (stronger when the issue's status
// just moved backwards). A scheduled job clusters an agent's recent signals
// by the words of the corrections; a cluster of at least three becomes a
// DRAFT skill in the library, with the source issues and comments listed
// for verification. A draft is never assignable until a human publishes
// it; dismissing one keeps the signals. The draft text is a starting point
// to edit, never a finished artifact.

const (
	AuditSkillMined            = "skill.mined"
	skillMinerWindow           = 48 * time.Hour
	skillMinerMinAge           = 10 * time.Minute
	skillMinerMinCluster       = 3
	skillMinerSimilarity       = 0.25
	skillMinerMaxExcerpt       = 600
	skillMinerMaxSignalsPerRun = 500
)

// statusRank orders the workflow so a backwards move is recognizable.
var statusRank = map[string]int{"backlog": 0, "todo": 1, "in_progress": 2, "in_review": 3, "done": 4}

// detectCorrectionSignal runs after a comment lands: a member speaking right
// after an agent on the same issue is a correction candidate.
func (h *Handler) detectCorrectionSignal(ctx context.Context, issue db.Issue, comment db.Comment, authorType string) {
	if authorType != "member" || comment.Type != "comment" || !comment.AuthorID.Valid {
		return
	}
	prev, err := h.Queries.GetLatestAgentCommentBefore(ctx, db.GetLatestAgentCommentBeforeParams{IssueID: issue.ID, Before: comment.CreatedAt})
	if err != nil || !prev.AuthorID.Valid || comment.CreatedAt.Time.Sub(prev.CreatedAt.Time) > skillMinerWindow {
		return
	}
	regressed := false
	if entries, err := h.Queries.ListAuditLogEntries(ctx, db.ListAuditLogEntriesParams{WorkspaceID: issue.WorkspaceID, EntityID: issue.ID, Action: pgtype.Text{String: AuditIssueStatus, Valid: true}, PageSize: 1}); err == nil && len(entries) == 1 {
		var d struct {
			From string `json:"from"`
			To   string `json:"to"`
		}
		if json.Unmarshal(entries[0].Details, &d) == nil && time.Since(entries[0].OccurredAt.Time) < 24*time.Hour {
			fromRank, fromKnown := statusRank[d.From]
			toRank, toKnown := statusRank[d.To]
			regressed = fromKnown && toKnown && toRank < fromRank
		}
	}
	if _, err := h.Queries.CreateAgentCorrectionSignal(ctx, db.CreateAgentCorrectionSignalParams{
		ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, IssueID: issue.ID, AgentID: prev.AuthorID,
		AgentCommentID: prev.ID, CorrectionCommentID: comment.ID, StatusRegressed: regressed,
	}); err != nil && !strings.Contains(err.Error(), "no rows") {
		slog.Warn("skill miner: record signal failed", "issue_id", uuidToString(issue.ID), "error", err)
	}
}

// --- mining ------------------------------------------------------------------

type minerSignal struct {
	row   db.ListUnminedCorrectionSignalsRow
	words map[string]bool
}

// signalWords is the bag the clustering compares: lowercase words of four
// letters or more, without the most common English/French stop words.
func signalWords(s string) map[string]bool {
	stop := map[string]bool{"this": true, "that": true, "with": true, "from": true, "have": true, "please": true, "should": true, "would": true, "could": true, "when": true, "what": true, "your": true, "there": true, "here": true, "cette": true, "pour": true, "dans": true, "avec": true, "vous": true, "nous": true, "mais": true, "plus": true, "faut": true, "être": true, "sont": true}
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r >= 0x00C0) }) {
		if len([]rune(w)) >= 4 && !stop[w] {
			out[w] = true
		}
	}
	return out
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	inter := 0
	for w := range a {
		if b[w] {
			inter++
		}
	}
	return float64(inter) / float64(len(a)+len(b)-inter)
}

// clusterSignals groups an agent's signals greedily: a signal joins the
// first cluster whose seed it resembles enough.
func clusterSignals(signals []minerSignal) [][]minerSignal {
	var clusters [][]minerSignal
	for _, s := range signals {
		placed := false
		for i := range clusters {
			if jaccard(clusters[i][0].words, s.words) >= skillMinerSimilarity {
				clusters[i] = append(clusters[i], s)
				placed = true
				break
			}
		}
		if !placed {
			clusters = append(clusters, []minerSignal{s})
		}
	}
	return clusters
}

// MineSkills is the scheduler entry point: every cluster of recurring
// corrections becomes one draft skill.
func (h *Handler) MineSkills(ctx context.Context, now time.Time) (int, error) {
	rows, err := h.Queries.ListUnminedCorrectionSignals(ctx, pgtype.Timestamptz{Time: now.Add(-skillMinerMinAge), Valid: true})
	if err != nil {
		return 0, err
	}
	byAgent := map[string][]minerSignal{}
	order := []string{}
	for _, r := range rows {
		key := uuidToString(r.WorkspaceID) + "/" + uuidToString(r.AgentID)
		if _, seen := byAgent[key]; !seen {
			order = append(order, key)
		}
		byAgent[key] = append(byAgent[key], minerSignal{row: r, words: signalWords(r.CorrectionContent + " " + r.IssueTitle)})
	}
	drafted := 0
	for _, key := range order {
		for _, cluster := range clusterSignals(byAgent[key]) {
			if len(cluster) < skillMinerMinCluster {
				continue
			}
			if err := h.draftSkillFromCluster(ctx, cluster); err != nil {
				slog.Warn("skill miner: draft failed", "error", err)
				continue
			}
			drafted++
		}
	}
	return drafted, nil
}

type minedDraft struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
}

const skillMinerPrompt = `You write reusable skills for AI agents from recurring human corrections. Given an agent and several times a human corrected it the same way, write ONE skill the agent should follow next time. Return JSON only: {"name": "kebab-case-under-40-chars", "description": "one sentence, what the skill prevents", "content": "markdown: a title, when it applies, the rule in 3-6 imperative bullets, one short example drawn from the corrections"}. Never include names, emails, secrets or issue identifiers in the content; describe the pattern, not the people.`

// draftSkillFromCluster writes the draft: an LLM distillation when one is
// configured, else a template that quotes the corrections. Either way the
// sources ride in config.origin and the skill stays a draft.
func (h *Handler) draftSkillFromCluster(ctx context.Context, cluster []minerSignal) error {
	first := cluster[0].row
	agent, err := h.Queries.GetAgent(ctx, first.AgentID)
	if err != nil {
		return err
	}
	var lines []string
	ids, issueIDs, commentIDs := make([]pgtype.UUID, 0, len(cluster)), []string{}, []string{}
	regressed := 0
	seenIssue := map[string]bool{}
	for i, s := range cluster {
		ids = append(ids, s.row.ID)
		if id := uuidToString(s.row.IssueID); !seenIssue[id] {
			seenIssue[id] = true
			issueIDs = append(issueIDs, id)
		}
		commentIDs = append(commentIDs, uuidToString(s.row.CorrectionCommentID))
		if s.row.StatusRegressed {
			regressed++
		}
		lines = append(lines, fmt.Sprintf("%d. On \"%s\": %s", i+1, truncate(s.row.IssueTitle, 80), truncate(redact.Text(strings.TrimSpace(s.row.CorrectionContent)), skillMinerMaxExcerpt)))
	}
	corrections := strings.Join(lines, "\n")
	draft := minedDraft{
		Name:        "mined-" + slugify(strings.Join(topWords(cluster, 3), "-")),
		Description: fmt.Sprintf("Recurring correction of %s, seen %d times.", agent.Name, len(cluster)),
		Content: fmt.Sprintf("# Recurring correction (%s)\n\nHumans corrected this agent the same way %d times (%d with a status moved back). Turn it into a rule.\n\n## What people said\n\n%s\n\n## Rule (to edit)\n\n- …\n",
			agent.Name, len(cluster), regressed, corrections),
	}
	if h.LLM != nil && h.LLM.Enabled() {
		user := fmt.Sprintf("Agent: %s\nCorrections:\n%s", agent.Name, corrections)
		if raw, err := h.LLM.GenerateJSON(ctx, h.LLM.DefaultModel(), skillMinerPrompt, user, 0.2, 1200); err == nil {
			var got minedDraft
			if json.Unmarshal([]byte(raw), &got) == nil && got.Name != "" && got.Content != "" {
				draft = got
				draft.Name = "mined-" + slugify(draft.Name)
				draft.Content = redact.Text(draft.Content)
				if draft.Description == "" {
					draft.Description = fmt.Sprintf("Recurring correction of %s, seen %d times.", agent.Name, len(cluster))
				}
			}
		} else {
			slog.Warn("skill miner: llm failed, template draft", "error", err)
		}
	}
	name := uniqueSkillName(ctx, h.Queries, first.WorkspaceID, truncate(draft.Name, 60))
	origin, _ := json.Marshal(map[string]any{"origin": map[string]any{
		"type": "skill_miner", "agent_id": uuidToString(first.AgentID), "agent_name": agent.Name, "signal_ids": uuidsToStrings(ids),
		"issue_ids": issueIDs, "comment_ids": commentIDs, "signals": len(cluster), "status_regressed": regressed, "llm": h.LLM != nil && h.LLM.Enabled(),
	}})
	skill, err := h.Queries.CreateSkill(ctx, db.CreateSkillParams{WorkspaceID: first.WorkspaceID, Name: name, Description: truncate(draft.Description, 300), Content: draft.Content, Config: origin, CreatedBy: pgtype.UUID{}})
	if err != nil {
		return err
	}
	if _, err := h.Queries.SetSkillStatus(ctx, db.SetSkillStatusParams{ID: skill.ID, WorkspaceID: first.WorkspaceID, Status: "draft"}); err != nil {
		return err
	}
	if err := h.Queries.MarkCorrectionSignalsMined(ctx, db.MarkCorrectionSignalsMinedParams{Column1: ids, MinedSkillID: skill.ID}); err != nil {
		return err
	}
	h.audit(ctx, first.WorkspaceID, "system", "", AuditSkillMined, "skill", skill.ID, map[string]any{"agent_id": uuidToString(first.AgentID), "signals": len(cluster), "issues": len(issueIDs)}, nil)
	h.publish("skill:mined", uuidToString(first.WorkspaceID), "system", "", map[string]any{"skill_id": uuidToString(skill.ID), "agent_id": uuidToString(first.AgentID)})
	return nil
}

func topWords(cluster []minerSignal, n int) []string {
	counts := map[string]int{}
	for _, s := range cluster {
		for w := range s.words {
			counts[w]++
		}
	}
	words := make([]string, 0, len(counts))
	for w := range counts {
		words = append(words, w)
	}
	sort.Slice(words, func(i, j int) bool {
		if counts[words[i]] != counts[words[j]] {
			return counts[words[i]] > counts[words[j]]
		}
		return words[i] < words[j]
	})
	if len(words) > n {
		words = words[:n]
	}
	if len(words) == 0 {
		words = []string{"correction"}
	}
	return words
}

func slugify(s string) string {
	var b strings.Builder
	last := '-'
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			last = r
		case last != '-':
			b.WriteRune('-')
			last = '-'
		}
	}
	return strings.Trim(b.String(), "-")
}

func uniqueSkillName(ctx context.Context, q *db.Queries, wsID pgtype.UUID, base string) string {
	name := base
	for i := 2; i < 50; i++ {
		if _, err := q.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{WorkspaceID: wsID, Name: name}); err != nil {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
	return fmt.Sprintf("%s-%d", base, time.Now().Unix())
}

// --- API ---------------------------------------------------------------------

type SkillDraftSource struct {
	IssueID     string `json:"issue_id"`
	IssueNumber int32  `json:"issue_number"`
	IssueTitle  string `json:"issue_title"`
	CommentID   string `json:"comment_id"`
	Regressed   bool   `json:"status_regressed"`
}

type SkillDraftResponse struct {
	SkillSummaryResponse
	Sources []SkillDraftSource `json:"sources"`
}

// ListSkillDrafts: GET /api/skill-miner/drafts — mined drafts with their sources.
func (h *Handler) ListSkillDrafts(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsID := parseUUID(h.resolveWorkspaceID(r))
	rows, err := h.Queries.ListDraftSkills(r.Context(), wsID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list drafts")
		return
	}
	out := make([]SkillDraftResponse, 0, len(rows))
	for _, s := range rows {
		item := SkillDraftResponse{SkillSummaryResponse: skillSummaryToResponse(s.ID, s.WorkspaceID, s.Name, s.Description, s.Config, s.CreatedBy, s.CreatedAt, s.UpdatedAt), Sources: []SkillDraftSource{}}
		item.Status = s.Status
		if signals, err := h.Queries.ListCorrectionSignalsForSkill(r.Context(), s.ID); err == nil {
			for _, sig := range signals {
				item.Sources = append(item.Sources, SkillDraftSource{IssueID: uuidToString(sig.IssueID), IssueNumber: sig.IssueNumber, IssueTitle: sig.IssueTitle, CommentID: uuidToString(sig.CorrectionCommentID), Regressed: sig.StatusRegressed})
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": out})
}

// rejectDraftSkills refuses to attach a draft to an agent (400).
func (h *Handler) rejectDraftSkills(w http.ResponseWriter, r *http.Request, skillIDs []pgtype.UUID) bool {
	if len(skillIDs) == 0 {
		return true
	}
	n, err := h.Queries.CountDraftSkillsAmong(r.Context(), skillIDs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check skills")
		return false
	}
	if n > 0 {
		writeError(w, http.StatusBadRequest, "a draft skill must be published before it is attached to an agent")
		return false
	}
	return true
}
