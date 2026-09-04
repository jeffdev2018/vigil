package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Morning briefing (K30): three sections composed from live data — done in
// the last 24 hours, awaiting review, blocked and why — sent once per
// workspace and local date as an inbox item to every member, and readable
// in the app at any time. Every line points at an issue; nothing here is
// generated text.

const (
	briefingWindow         = 24 * time.Hour
	briefingMaxPerSection  = 25
	briefingReasonMaxRunes = 240
)

type BriefingItem struct {
	IssueID    string `json:"issue_id"`
	Identifier string `json:"identifier"`
	Title      string `json:"title"`
	Status     string `json:"status"`
	// Reason is why the issue is blocked: the open question, the run's
	// failure, or the handoff note it left.
	Reason string `json:"reason,omitempty"`
	// PendingDecisions counts the cards a human still has to answer.
	PendingDecisions int `json:"pending_decisions,omitempty"`
}

type MorningBriefingResponse struct {
	Date           string         `json:"date"`
	Merged         []BriefingItem `json:"merged"`
	AwaitingReview []BriefingItem `json:"awaiting_review"`
	Blocked        []BriefingItem `json:"blocked"`
	SentAt         *string        `json:"sent_at"`
	// AlreadySent is set by a trigger that found the day's send recorded.
	AlreadySent bool `json:"already_sent,omitempty"`
}

func briefingLocation(name string) *time.Location {
	if loc, err := time.LoadLocation(name); err == nil {
		return loc
	}
	return time.UTC
}

func truncateReason(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > briefingReasonMaxRunes {
		return string(r[:briefingReasonMaxRunes]) + "…"
	}
	return s
}

func cap25(items []BriefingItem) []BriefingItem {
	if items == nil {
		return []BriefingItem{}
	}
	if len(items) > briefingMaxPerSection {
		return items[:briefingMaxPerSection]
	}
	return items
}

// composeBriefing reads the three sections as of now.
func (h *Handler) composeBriefing(ctx context.Context, wsID pgtype.UUID, now time.Time, loc *time.Location) (MorningBriefingResponse, error) {
	prefix := h.getIssuePrefix(ctx, wsID)
	item := func(i db.Issue) BriefingItem {
		return BriefingItem{IssueID: uuidToString(i.ID), Identifier: fmt.Sprintf("%s-%d", prefix, i.Number), Title: i.Title, Status: i.Status}
	}
	out := MorningBriefingResponse{Date: now.In(loc).Format("2006-01-02"), Merged: []BriefingItem{}, AwaitingReview: []BriefingItem{}, Blocked: []BriefingItem{}}

	done, err := h.Queries.ListIssuesCompletedBetween(ctx, db.ListIssuesCompletedBetweenParams{
		WorkspaceID: wsID, CompletedAt: pgtype.Timestamptz{Time: now.Add(-briefingWindow), Valid: true}, CompletedAt_2: pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return out, fmt.Errorf("completed issues: %w", err)
	}
	for _, i := range done {
		out.Merged = append(out.Merged, item(i))
	}

	reviewKeys, err := issuestatus.ExpandCategories(ctx, h.Queries, wsID, []string{issuestatus.InReview})
	if err != nil {
		return out, fmt.Errorf("review statuses: %w", err)
	}
	inReview, err := h.Queries.ListIssuesInStatuses(ctx, db.ListIssuesInStatusesParams{WorkspaceID: wsID, Statuses: reviewKeys})
	if err != nil {
		return out, fmt.Errorf("issues in review: %w", err)
	}
	for _, i := range inReview {
		out.AwaitingReview = append(out.AwaitingReview, item(i))
	}

	// Blocked: the blocked category, plus any issue with a card waiting on a
	// human; the reason is the question, else what the last run said.
	pending, err := h.Queries.ListPendingDecisionsForWorkspace(ctx, wsID)
	if err != nil {
		return out, fmt.Errorf("pending decisions: %w", err)
	}
	questions := map[string][]string{}
	for _, d := range pending {
		id := uuidToString(d.IssueID)
		questions[id] = append(questions[id], d.Question)
	}
	blockedKeys, err := issuestatus.ExpandCategories(ctx, h.Queries, wsID, []string{issuestatus.Blocked})
	if err != nil {
		return out, fmt.Errorf("blocked statuses: %w", err)
	}
	blocked, err := h.Queries.ListIssuesInStatuses(ctx, db.ListIssuesInStatusesParams{WorkspaceID: wsID, Statuses: blockedKeys})
	if err != nil {
		return out, fmt.Errorf("blocked issues: %w", err)
	}
	seen := map[string]bool{}
	addBlocked := func(i db.Issue) {
		id := uuidToString(i.ID)
		if seen[id] {
			return
		}
		seen[id] = true
		b := item(i)
		if qs := questions[id]; len(qs) > 0 {
			b.PendingDecisions = len(qs)
			b.Reason = truncateReason(qs[0])
		} else if tasks, err := h.Queries.ListTasksByIssue(ctx, i.ID); err == nil && len(tasks) > 0 {
			last := tasks[0]
			switch {
			case last.Error.Valid && strings.TrimSpace(last.Error.String) != "":
				b.Reason = truncateReason(last.Error.String)
			case last.FailureReason.Valid && strings.TrimSpace(last.FailureReason.String) != "":
				b.Reason = truncateReason(last.FailureReason.String)
			case last.HandoffNote.Valid && strings.TrimSpace(last.HandoffNote.String) != "":
				b.Reason = truncateReason(last.HandoffNote.String)
			}
		}
		out.Blocked = append(out.Blocked, b)
	}
	for _, i := range blocked {
		addBlocked(i)
	}
	for _, d := range pending {
		if seen[uuidToString(d.IssueID)] {
			continue
		}
		if i, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: d.IssueID, WorkspaceID: wsID}); err == nil {
			addBlocked(i)
		}
	}
	out.Merged, out.AwaitingReview, out.Blocked = cap25(out.Merged), cap25(out.AwaitingReview), cap25(out.Blocked)
	return out, nil
}

// sendBriefing records the day's send first (the unique index decides) and
// then files one inbox item per member. Returns false when already sent.
func (h *Handler) sendBriefing(ctx context.Context, wsID pgtype.UUID, briefing MorningBriefingResponse, actorType, actorID string) (bool, error) {
	var date pgtype.Date
	if err := date.Scan(briefing.Date); err != nil {
		return false, err
	}
	summary, _ := json.Marshal(map[string]int{"merged": len(briefing.Merged), "awaiting_review": len(briefing.AwaitingReview), "blocked": len(briefing.Blocked)})
	_, err := h.Queries.RecordMorningBriefingSent(ctx, db.RecordMorningBriefingSentParams{
		WorkspaceID: wsID, SentForDate: date, ChannelsDelivered: []byte(`["inbox"]`), Summary: summary,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("record send: %w", err)
	}
	members, err := h.Queries.ListMembers(ctx, wsID)
	if err != nil {
		return true, fmt.Errorf("list members: %w", err)
	}
	details, _ := json.Marshal(briefing)
	title := fmt.Sprintf("This morning: %d done · %d awaiting review · %d blocked", len(briefing.Merged), len(briefing.AwaitingReview), len(briefing.Blocked))
	for _, m := range members {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: wsID, RecipientType: "member", RecipientID: m.UserID,
			Type: "morning_briefing", Severity: "info", Title: title,
			Body:      pgtype.Text{String: briefing.Date, Valid: true},
			ActorType: pgtype.Text{String: actorType, Valid: actorType != ""}, ActorID: parseUUIDOrZero(actorID), Details: details,
		})
		if err != nil {
			slog.Warn("morning briefing: inbox item failed", "error", err, "workspace_id", uuidToString(wsID))
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(wsID), actorType, actorID, map[string]any{"item": inboxToResponse(item)})
	}
	return true, nil
}

func parseUUIDOrZero(s string) pgtype.UUID {
	if s == "" {
		return pgtype.UUID{}
	}
	return parseUUID(s)
}

// SendDueMorningBriefings is the scheduler's entry: every enabled workspace
// whose local clock passed its hour today, and has no send recorded for
// today, gets one. Returns how many were sent.
func (h *Handler) SendDueMorningBriefings(ctx context.Context, now time.Time) (int, error) {
	workspaces, err := h.Queries.ListWorkspacesForBriefing(ctx)
	if err != nil {
		return 0, fmt.Errorf("list workspaces: %w", err)
	}
	sent := 0
	for _, ws := range workspaces {
		cfg, ok := service.MorningBriefingSettings(ws.Settings)
		if !ok {
			continue
		}
		loc := briefingLocation(cfg.Timezone)
		local := now.In(loc)
		if local.Hour() < cfg.Hour {
			continue
		}
		var date pgtype.Date
		_ = date.Scan(local.Format("2006-01-02"))
		if _, err := h.Queries.GetMorningBriefingSent(ctx, db.GetMorningBriefingSentParams{WorkspaceID: ws.ID, SentForDate: date}); err == nil {
			continue
		}
		briefing, err := h.composeBriefing(ctx, ws.ID, now, loc)
		if err != nil {
			slog.Warn("morning briefing: compose failed", "error", err, "workspace_id", uuidToString(ws.ID))
			continue
		}
		did, err := h.sendBriefing(ctx, ws.ID, briefing, "system", "")
		if err != nil {
			slog.Warn("morning briefing: send failed", "error", err, "workspace_id", uuidToString(ws.ID))
			continue
		}
		if did {
			sent++
		}
	}
	return sent, nil
}

// GetMorningBriefingToday — GET /api/morning-briefing/today.
func (h *Handler) GetMorningBriefingToday(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	cfg, _ := service.MorningBriefingSettings(ws.Settings)
	loc := briefingLocation(cfg.Timezone)
	if cfg.Timezone == "" {
		loc = briefingLocation(h.resolveViewingTZ(r))
	}
	briefing, err := h.composeBriefing(r.Context(), wsUUID, time.Now(), loc)
	if err != nil {
		slog.Warn("morning briefing failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to compose the briefing")
		return
	}
	var date pgtype.Date
	_ = date.Scan(briefing.Date)
	if sent, err := h.Queries.GetMorningBriefingSent(r.Context(), db.GetMorningBriefingSentParams{WorkspaceID: wsUUID, SentForDate: date}); err == nil {
		briefing.SentAt = timestampToPtr(sent.CreatedAt)
	}
	writeJSON(w, http.StatusOK, briefing)
}

// TriggerMorningBriefing — POST /api/morning-briefing/trigger (owner/admin):
// sends today's briefing now; a day already sent answers with already_sent.
func (h *Handler) TriggerMorningBriefing(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	cfg, _ := service.MorningBriefingSettings(ws.Settings)
	loc := briefingLocation(cfg.Timezone)
	briefing, err := h.composeBriefing(r.Context(), wsUUID, time.Now(), loc)
	if err != nil {
		slog.Warn("morning briefing failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to compose the briefing")
		return
	}
	did, err := h.sendBriefing(r.Context(), wsUUID, briefing, "member", userID)
	if err != nil {
		slog.Warn("morning briefing send failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to send the briefing")
		return
	}
	var date pgtype.Date
	_ = date.Scan(briefing.Date)
	if sent, err := h.Queries.GetMorningBriefingSent(r.Context(), db.GetMorningBriefingSentParams{WorkspaceID: wsUUID, SentForDate: date}); err == nil {
		briefing.SentAt = timestampToPtr(sent.CreatedAt)
	}
	briefing.AlreadySent = !did
	writeJSON(w, http.StatusOK, briefing)
}
