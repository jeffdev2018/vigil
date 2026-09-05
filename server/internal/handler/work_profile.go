package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

// Vigil learns you (K71). Every decision a person answers becomes a training
// example; the examples of one kind of decision (same question family, same
// option set) roll up into an observation of the person's work profile:
// "you chose X 9 times out of 10". The next similar decision arrives with
// that hint. A rule decides alone only when the person switched it on, the
// rate is high enough, the decision is not high-stakes, and the asking agent
// is autonomous; an auto-decision is notified and can be overturned, and a
// rule whose auto-decisions get overturned more than 20 % over 30 days is
// demoted to a proposal. Nothing is learned without being visible on the
// person's "what Vigil knows about me" page, where it can be forgotten.
//
// Declared adaptation surface: decision rules and the decision-hour
// histogram. Everything else is frozen.

const (
	InboxTypeDecisionAutoDecided = "decision_auto_decided"
	AuditWorkProfileChanged      = "work_profile.changed"

	workProfileMinExamples   = 5
	workProfileAutoRate      = 0.9
	workProfileDemoteRate    = 0.2
	workProfileReviewSeconds = 45 // ponytail: a flat estimate per decision reviewed by a human

	stakeNormal = "normal"
	stakeHigh   = "high"
)

// DecisionHint is what the person's history says about a pending decision.
type DecisionHint struct {
	Signature   string  `json:"signature"`
	OptionID    string  `json:"option_id"`
	OptionLabel string  `json:"option_label"`
	Count       int     `json:"count"`
	Total       int     `json:"total"`
	Rate        float64 `json:"rate"`
	Auto        bool    `json:"auto"`
	Stake       string  `json:"stake"`
}

type WorkProfileObservationResponse struct {
	ID              string          `json:"id"`
	Key             string          `json:"key"`
	Kind            string          `json:"kind"`
	Value           json.RawMessage `json:"value"`
	Source          string          `json:"source"`
	Count           int32           `json:"count"`
	Corrections     int32           `json:"corrections"`
	Auto            bool            `json:"auto"`
	State           string          `json:"state"`
	Stake           string          `json:"stake"`
	FirstObservedAt time.Time       `json:"first_observed_at"`
	LastObservedAt  time.Time       `json:"last_observed_at"`
}

type WorkProfileResponse struct {
	Observations      []WorkProfileObservationResponse `json:"observations"`
	Examples          int64                            `json:"examples"`
	AutoDecided       int64                            `json:"auto_decided"`
	Overturned        int64                            `json:"overturned"`
	ReviewLoadSeconds int64                            `json:"review_load_seconds"`
	AdaptationSurface []string                         `json:"adaptation_surface"`
}

// decisionSignature names the kind of a decision: its question family and
// its option set. Two decisions with the same signature are "similar".
func decisionSignature(d db.IssueDecision) string {
	family := "question"
	q := d.Question
	switch {
	case strings.HasPrefix(q, "Blocked action ·"):
		family = "gate"
	case strings.HasPrefix(q, "Second approval ·"):
		family = "second_approval"
	case strings.HasPrefix(q, "Show me first ·"):
		family = "preview"
	case strings.HasPrefix(q, "Watchdog ·"):
		family = "watchdog"
	case strings.HasPrefix(q, "Pipeline gate ·"):
		family = "pipeline_gate"
	case d.PlanVersion.Valid:
		family = "plan"
	case d.InterviewGroupID.Valid:
		family = "interview"
	}
	var options []DecisionOption
	_ = json.Unmarshal(d.Options, &options)
	ids := make([]string, 0, len(options))
	for _, o := range options {
		ids = append(ids, o.ID)
	}
	sort.Strings(ids)
	return family + ":" + strings.Join(ids, ",")
}

// decisionStake: money, outgoing data, external messages are never automated.
func decisionStake(d db.IssueDecision) string {
	q := d.Question
	if strings.HasPrefix(q, "Blocked action ·") || strings.HasPrefix(q, "Second approval ·") {
		return stakeHigh
	}
	if strings.Contains(q, "cannot be undone") {
		return stakeHigh
	}
	var options []DecisionOption
	_ = json.Unmarshal(d.Options, &options)
	for _, o := range options {
		if strings.Contains(strings.ToLower(o.Impact), "irreversible") || strings.Contains(strings.ToLower(o.Impact), "cannot be undone") {
			return stakeHigh
		}
	}
	return stakeNormal
}

func observationKind(key string) string {
	if strings.HasPrefix(key, "decision:") {
		return "decision_rule"
	}
	return key
}

func observationStake(key string) string {
	if strings.HasPrefix(key, "decision:gate:") || strings.HasPrefix(key, "decision:second_approval:") {
		return stakeHigh
	}
	return stakeNormal
}

// learnFromDecision records the example and refreshes the person's rule for
// that kind of decision. Called after a human answered (auto=false) or after
// a rule decided for them (auto=true).
func (h *Handler) learnFromDecision(ctx context.Context, wsID pgtype.UUID, userID string, decision db.IssueDecision, answer DecisionAnswer, auto bool) {
	uid := parseUUID(userID)
	if !uid.Valid {
		return
	}
	sig := decisionSignature(decision)
	stake := decisionStake(decision)
	var options []DecisionOption
	_ = json.Unmarshal(decision.Options, &options)
	optionsJSON, _ := json.Marshal(options)
	if _, err := h.Queries.CreateDecisionTrainingExample(ctx, db.CreateDecisionTrainingExampleParams{
		ID: dbid.NewV7(), WorkspaceID: wsID, UserID: uid, DecisionID: decision.ID, Signature: sig,
		Question: redact.Text(truncate(decision.Question, 500)), Options: optionsJSON, OptionID: answer.OptionID, ModifiedText: redact.Text(truncate(answer.ModifiedText, 500)),
		Stake: stake, Auto: auto,
	}); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Warn("work profile: record example failed", "error", err)
	}
	// The rule: the dominant option over the person's examples of this kind.
	examples, err := h.Queries.ListDecisionTrainingExamples(ctx, db.ListDecisionTrainingExamplesParams{WorkspaceID: wsID, UserID: uid, Signature: sig})
	if err != nil || len(examples) == 0 {
		return
	}
	counts := map[string]int{}
	total := 0
	for _, e := range examples {
		if e.OptionID == "" || e.Overturned {
			continue
		}
		counts[e.OptionID]++
		total++
	}
	best, bestN := "", 0
	for id, n := range counts {
		if n > bestN || (n == bestN && id < best) {
			best, bestN = id, n
		}
	}
	if best == "" {
		return
	}
	label := best
	for _, o := range options {
		if o.ID == best {
			label = o.Label
		}
	}
	key := "decision:" + sig
	prev, err := h.Queries.GetWorkProfileObservation(ctx, db.GetWorkProfileObservationParams{WorkspaceID: wsID, UserID: uid, Key: key})
	autoOn, state, corrections := false, "learned", int32(0)
	if err == nil {
		autoOn, state, corrections = prev.Auto, prev.State, prev.Corrections
	}
	if !auto && answer.OptionID != best {
		corrections++ // the person went against the rule
	}
	value, _ := json.Marshal(map[string]any{"option_id": best, "option_label": label, "count": bestN, "total": total, "family": strings.SplitN(sig, ":", 2)[0]})
	if _, err := h.Queries.UpsertWorkProfileObservation(ctx, db.UpsertWorkProfileObservationParams{
		ID: dbid.NewV7(), WorkspaceID: wsID, UserID: uid, Key: key, Value: value, Source: "decisions", Count: int32(total), Corrections: corrections, Auto: autoOn, State: state,
	}); err != nil {
		slog.Warn("work profile: upsert rule failed", "error", err)
	}
	// Decision-hour histogram: when the person answers.
	hour := time.Now().UTC().Hour()
	hkey := "decision_hour"
	hist := map[string]int{}
	if hprev, err := h.Queries.GetWorkProfileObservation(ctx, db.GetWorkProfileObservationParams{WorkspaceID: wsID, UserID: uid, Key: hkey}); err == nil {
		_ = json.Unmarshal(hprev.Value, &hist)
	}
	if !auto {
		hist[time.Date(2000, 1, 1, hour, 0, 0, 0, time.UTC).Format("15")]++
		hv, _ := json.Marshal(hist)
		n := 0
		for _, c := range hist {
			n += c
		}
		_, _ = h.Queries.UpsertWorkProfileObservation(ctx, db.UpsertWorkProfileObservationParams{ID: dbid.NewV7(), WorkspaceID: wsID, UserID: uid, Key: hkey, Value: hv, Source: "decisions", Count: int32(n), State: "learned"})
	}
}

// decisionHint reads the person's rule for a pending decision, if any.
func (h *Handler) decisionHint(ctx context.Context, wsID pgtype.UUID, userID string, decision db.IssueDecision) *DecisionHint {
	uid := parseUUID(userID)
	if !uid.Valid || decision.RespondedAt.Valid {
		return nil
	}
	sig := decisionSignature(decision)
	obs, err := h.Queries.GetWorkProfileObservation(ctx, db.GetWorkProfileObservationParams{WorkspaceID: wsID, UserID: uid, Key: "decision:" + sig})
	if err != nil {
		return nil
	}
	var v struct {
		OptionID    string `json:"option_id"`
		OptionLabel string `json:"option_label"`
		Count       int    `json:"count"`
		Total       int    `json:"total"`
	}
	if json.Unmarshal(obs.Value, &v) != nil || v.Total < workProfileMinExamples || v.OptionID == "" {
		return nil
	}
	// The option must still exist on this decision.
	var options []DecisionOption
	_ = json.Unmarshal(decision.Options, &options)
	found := false
	for _, o := range options {
		if o.ID == v.OptionID {
			found = true
			v.OptionLabel = o.Label
		}
	}
	if !found {
		return nil
	}
	stake := decisionStake(decision)
	rate := float64(v.Count) / float64(v.Total)
	return &DecisionHint{Signature: sig, OptionID: v.OptionID, OptionLabel: v.OptionLabel, Count: v.Count, Total: v.Total, Rate: rate,
		Auto: obs.Auto && obs.State == "learned" && stake == stakeNormal && rate >= workProfileAutoRate, Stake: stake}
}

// autoDecide answers a fresh decision on behalf of the first recipient whose
// rule qualifies: switched on, calibrated enough, normal stakes, and an
// autonomous asker when an agent asked. Returns true when it decided.
func (h *Handler) autoDecide(ctx context.Context, issue db.Issue, decision db.IssueDecision, recipients []pgtype.UUID) bool {
	if decision.RespondedAt.Valid || decisionStake(decision) == stakeHigh {
		return false
	}
	if decision.AskedByType == "agent" {
		agent, err := h.Queries.GetAgent(ctx, decision.AskedByID)
		if err != nil || agent.TrustMode != TrustAutonomous {
			return false
		}
	}
	for _, uid := range recipients {
		userID := uuidToString(uid)
		hint := h.decisionHint(ctx, issue.WorkspaceID, userID, decision)
		if hint == nil || !hint.Auto {
			continue
		}
		answer := DecisionAnswer{OptionID: hint.OptionID}
		if _, code, err := h.answerDecisionCore(ctx, issue, decision, userID, "member", userID, answer, hint.OptionLabel, "", nil); err != nil || code != "" {
			slog.Warn("work profile: auto-decide failed", "decision_id", uuidToString(decision.ID), "error", err, "code", code)
			return false
		}
		// answerDecisionCore already recorded the example as a human answer; flag it.
		if err := h.Queries.SetDecisionTrainingExampleAuto(ctx, decision.ID); err != nil {
			slog.Warn("work profile: flag auto example failed", "error", err)
		}
		details, _ := json.Marshal(map[string]any{"decision_id": uuidToString(decision.ID), "option_id": hint.OptionID, "option_label": hint.OptionLabel, "count": hint.Count, "total": hint.Total, "signature": hint.Signature})
		if item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: issue.WorkspaceID, RecipientType: "member", RecipientID: uid, Type: InboxTypeDecisionAutoDecided, Severity: "info",
			IssueID: issue.ID, Title: issue.Title, Body: pgtype.Text{String: "Decided for you: " + hint.OptionLabel + " (" + truncate(decision.Question, 200) + ")", Valid: true},
			ActorType: pgtype.Text{String: "system", Valid: true}, Details: details,
		}); err == nil {
			h.publish(protocol.EventInboxNew, uuidToString(issue.WorkspaceID), "system", "", map[string]any{"item": inboxToResponse(item)})
		}
		h.audit(ctx, issue.WorkspaceID, "system", "", AuditDecisionAnswered, "issue_decision", decision.ID, map[string]any{"auto": true, "on_behalf_of": userID, "option_id": hint.OptionID, "count": hint.Count, "total": hint.Total}, nil)
		return true
	}
	return false
}

// --- API ---------------------------------------------------------------------

func (h *Handler) workProfileResponse(ctx context.Context, wsID, uid pgtype.UUID) (WorkProfileResponse, error) {
	rows, err := h.Queries.ListWorkProfileObservations(ctx, db.ListWorkProfileObservationsParams{WorkspaceID: wsID, UserID: uid})
	if err != nil {
		return WorkProfileResponse{}, err
	}
	out := WorkProfileResponse{Observations: make([]WorkProfileObservationResponse, 0, len(rows)), AdaptationSurface: []string{"decision_rules", "decision_hours"}}
	for _, o := range rows {
		out.Observations = append(out.Observations, WorkProfileObservationResponse{
			ID: uuidToString(o.ID), Key: o.Key, Kind: observationKind(o.Key), Value: json.RawMessage(o.Value), Source: o.Source, Count: o.Count, Corrections: o.Corrections,
			Auto: o.Auto, State: o.State, Stake: observationStake(o.Key), FirstObservedAt: o.FirstObservedAt.Time, LastObservedAt: o.LastObservedAt.Time,
		})
	}
	stats, err := h.Queries.CountDecisionTrainingExamples(ctx, db.CountDecisionTrainingExamplesParams{WorkspaceID: wsID, UserID: uid})
	if err == nil {
		out.Examples, out.AutoDecided, out.Overturned = stats.Total, stats.AutoDecided, stats.Overturned
		out.ReviewLoadSeconds = (stats.Total - stats.AutoDecided) * workProfileReviewSeconds
	}
	return out, nil
}

// GetMyWorkProfile: GET /api/work-profile — what Vigil knows about me, here.
func (h *Handler) GetMyWorkProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsID := parseUUID(h.resolveWorkspaceID(r))
	resp, err := h.workProfileResponse(r.Context(), wsID, parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the work profile")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// PatchWorkProfileObservation: PATCH /api/work-profile/{id} {auto}
func (h *Handler) PatchWorkProfileObservation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "observation id")
	if !ok {
		return
	}
	wsID := parseUUID(h.resolveWorkspaceID(r))
	obs, err := h.Queries.GetWorkProfileObservationByID(r.Context(), db.GetWorkProfileObservationByIDParams{ID: id, WorkspaceID: wsID})
	if err != nil || uuidToString(obs.UserID) != userID {
		writeError(w, http.StatusNotFound, "observation not found")
		return
	}
	var req struct {
		Auto *bool `json:"auto"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.Auto == nil {
		writeError(w, http.StatusBadRequest, "auto is required")
		return
	}
	if *req.Auto && observationStake(obs.Key) == stakeHigh {
		writeError(w, http.StatusBadRequest, "a high-stakes decision is never automated")
		return
	}
	state := "learned"
	if !*req.Auto {
		state = obs.State
	}
	updated, err := h.Queries.SetWorkProfileObservationAuto(r.Context(), db.SetWorkProfileObservationAutoParams{ID: id, WorkspaceID: wsID, Auto: *req.Auto, State: state})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update the observation")
		return
	}
	h.audit(r.Context(), wsID, "member", userID, AuditWorkProfileChanged, "work_profile_observation", id, map[string]any{"key": obs.Key, "auto": *req.Auto}, nil)
	resp, _ := h.workProfileResponse(r.Context(), wsID, parseUUID(userID))
	_ = updated
	writeJSON(w, http.StatusOK, resp)
}

// DeleteWorkProfileObservation: DELETE /api/work-profile/{id} — forget.
func (h *Handler) DeleteWorkProfileObservation(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "observation id")
	if !ok {
		return
	}
	wsID := parseUUID(h.resolveWorkspaceID(r))
	n, err := h.Queries.DeleteWorkProfileObservation(r.Context(), db.DeleteWorkProfileObservationParams{ID: id, WorkspaceID: wsID, UserID: parseUUID(userID)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to forget the observation")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "observation not found")
		return
	}
	h.audit(r.Context(), wsID, "member", userID, AuditWorkProfileChanged, "work_profile_observation", id, map[string]any{"forgotten": true}, nil)
	w.WriteHeader(http.StatusNoContent)
}

// OverturnDecisionExample: POST /api/decision-examples/{id}/overturn — the
// person says the rule was wrong here; the rule's corrections grow and it is
// demoted to a proposal past 20 % over 30 days.
func (h *Handler) OverturnDecisionExample(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "example id")
	if !ok {
		return
	}
	wsID := parseUUID(h.resolveWorkspaceID(r))
	ex, err := h.Queries.OverturnDecisionTrainingExample(r.Context(), db.OverturnDecisionTrainingExampleParams{ID: id, WorkspaceID: wsID, UserID: parseUUID(userID)})
	if err != nil {
		writeError(w, http.StatusNotFound, "example not found")
		return
	}
	h.demoteRuleIfNoisy(r.Context(), wsID, ex.UserID, ex.Signature)
	h.audit(r.Context(), wsID, "member", userID, AuditWorkProfileChanged, "decision_training_example", id, map[string]any{"overturned": true, "signature": ex.Signature}, nil)
	resp, _ := h.workProfileResponse(r.Context(), wsID, parseUUID(userID))
	writeJSON(w, http.StatusOK, resp)
}

// demoteRuleIfNoisy re-reviews a rule: more than 20 % of its auto-decisions
// overturned over 30 days turns auto off and marks it a proposal.
func (h *Handler) demoteRuleIfNoisy(ctx context.Context, wsID, uid pgtype.UUID, signature string) {
	key := "decision:" + signature
	obs, err := h.Queries.GetWorkProfileObservation(ctx, db.GetWorkProfileObservationParams{WorkspaceID: wsID, UserID: uid, Key: key})
	if err != nil {
		return
	}
	stats, err := h.Queries.CountRecentCorrections(ctx, db.CountRecentCorrectionsParams{WorkspaceID: wsID, UserID: uid, Signature: signature})
	if err != nil {
		return
	}
	corrections := obs.Corrections + 1
	auto, state := obs.Auto, obs.State
	if stats.AutoDecided > 0 && float64(stats.Overturned)/float64(stats.AutoDecided) > workProfileDemoteRate {
		auto, state = false, "proposed"
	}
	if _, err := h.Queries.UpsertWorkProfileObservation(ctx, db.UpsertWorkProfileObservationParams{
		ID: dbid.NewV7(), WorkspaceID: wsID, UserID: uid, Key: key, Value: obs.Value, Source: obs.Source, Count: obs.Count, Corrections: corrections, Auto: auto, State: state,
	}); err != nil {
		slog.Warn("work profile: demote failed", "error", err)
	}
}
