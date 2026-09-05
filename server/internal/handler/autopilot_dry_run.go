package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Reason codes shared by the webhook ingress, the delivery rows and the
// dry-run. They are the enum the UI localizes, so ingress and dry-run MUST
// name a blocked delivery identically — a preview that invents its own
// vocabulary is worse than no preview.
const (
	reasonTriggerDisabled    = "trigger_disabled"
	reasonAutopilotArchived  = "autopilot_archived"
	reasonAutopilotPaused    = "autopilot_paused"
	reasonEventFiltered      = "event_filtered"
	reasonCriteriaNotMatched = "criteria_not_matched"
	reasonQuotaExceeded      = "quota_exceeded"
)

// webhookDeliveryDecision is the verdict of the pre-admission chain.
type webhookDeliveryDecision struct {
	// Run is true when the delivery should produce an autopilot run.
	Run bool
	// ReasonCode is empty when Run; otherwise the stable code above.
	ReasonCode string
	// Explanation is the classifier's own sentence, when one was asked for.
	// Present on both verdicts so a dry-run can show why an event passed.
	Explanation string
	// MatchedFilters names the event_filters rows that admitted the event.
	// Nil when the trigger declares no filters (everything passes).
	MatchedFilters []WebhookEventFilter
}

// webhookDeliveryState is everything the decision reads: mutable trigger and
// autopilot state plus the normalized envelope. No IDs, no request, no
// database — the point is that a dry-run can build one from a sample payload.
type webhookDeliveryState struct {
	TriggerEnabled     bool
	AutopilotStatus    string
	EventFilters       []byte
	EventMatchCriteria string
	Provider           string
	Envelope           WebhookEnvelope
}

// classifyWebhookEvent is the natural-language routing step. Injected rather
// than called through the Handler so the decision itself stays pure and a test
// can drive every branch without an LLM.
type classifyWebhookEvent func(criteria, provider string, envelope WebhookEnvelope) webhookRoutingVerdict

// evaluateWebhookDelivery decides whether a delivery becomes a run. Order
// matters and mirrors cost: free state checks, then the stored filter list,
// then the one paid LLM call.
func evaluateWebhookDelivery(state webhookDeliveryState, classify classifyWebhookEvent) webhookDeliveryDecision {
	switch {
	case !state.TriggerEnabled:
		return webhookDeliveryDecision{ReasonCode: reasonTriggerDisabled}
	case state.AutopilotStatus == "archived":
		return webhookDeliveryDecision{ReasonCode: reasonAutopilotArchived}
	case state.AutopilotStatus != "active":
		return webhookDeliveryDecision{ReasonCode: reasonAutopilotPaused}
	}

	allowed, matched := matchWebhookEventFilters(state.EventFilters, state.Envelope)
	if !allowed {
		return webhookDeliveryDecision{ReasonCode: reasonEventFiltered}
	}

	// The classifier is deliberately liberal and fails open (no LLM, upstream
	// error, malformed answer → run), so a broken model never silences a
	// webhook. Derived from Rowboat's Pass-1 event router (Apache-2.0).
	if criteria := strings.TrimSpace(state.EventMatchCriteria); criteria != "" && classify != nil {
		verdict := classify(criteria, state.Provider, state.Envelope)
		if !verdict.relevant {
			return webhookDeliveryDecision{
				ReasonCode:     reasonCriteriaNotMatched,
				Explanation:    verdict.reason,
				MatchedFilters: matched,
			}
		}
		return webhookDeliveryDecision{Run: true, Explanation: verdict.reason, MatchedFilters: matched}
	}
	return webhookDeliveryDecision{Run: true, MatchedFilters: matched}
}

// ── Dry-run endpoints ───────────────────────────────────────────────────────

// DryRunWebhookTriggerRequest is the body of POST .../triggers/{id}/dry-run.
type DryRunWebhookTriggerRequest struct {
	// Payload is the event body exactly as a provider would POST it.
	Payload json.RawMessage `json:"payload"`
	// Headers is the small subset the normalizer reads for event inference
	// (X-GitHub-Event, X-Event-Type, …). Signature headers are meaningless
	// here — a dry-run never verifies one.
	Headers map[string]string `json:"headers"`
}

// DryRunWebhookTriggerResponse mirrors what a real delivery would have
// recorded, minus the delivery row.
type DryRunWebhookTriggerResponse struct {
	WouldRun bool `json:"would_run"`
	// ReasonCode is null when the event would run.
	ReasonCode *string `json:"reason_code"`
	// Explanation is the classifier's sentence, or "" when no routing rule
	// was consulted.
	Explanation string `json:"explanation"`
	// MatchedFilters is the event_filters rows that admitted the event; empty
	// when the trigger declares none (and therefore accepts everything).
	MatchedFilters []WebhookEventFilter `json:"matched_filters"`
	// Event is the normalized event name the chain actually judged, which is
	// rarely what the raw body literally said.
	Event string `json:"event"`
}

// DryRunAutopilotWebhookTrigger replays the whole delivery decision against a
// sample payload and records nothing: no delivery row, no run, no
// last_fired_at. It DOES call the natural-language classifier for real —
// stubbing it would make the preview lie about the only step whose answer
// cannot be predicted from the trigger's configuration.
//
// Write access is required for the same reason: the classifier is a paid
// upstream call, so read-only viewers must not be able to spend on it.
func (h *Handler) DryRunAutopilotWebhookTrigger(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	ap, ok := h.loadAutopilotInWorkspace(w, r, chi.URLParam(r, "id"), workspaceID)
	if !ok {
		return
	}
	if !h.requireAutopilotWrite(w, r, ap, workspaceID) {
		return
	}
	trigger, ok := h.loadTriggerForAutopilot(w, r, ap, chi.URLParam(r, "triggerId"))
	if !ok {
		return
	}
	if trigger.Kind != "webhook" {
		writeError(w, http.StatusBadRequest, "trigger is not a webhook trigger")
		return
	}

	// Same body cap as the public ingress: a dry-run that accepts a payload
	// the real endpoint would reject with 413 is not a preview.
	r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)
	var req DryRunWebhookTriggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "payload too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Payload) == 0 {
		writeError(w, http.StatusBadRequest, "payload is required")
		return
	}

	headers := http.Header{}
	for k, v := range req.Headers {
		headers.Set(k, v)
	}
	if headers.Get("Content-Type") == "" {
		headers.Set("Content-Type", "application/json")
	}
	envelope, err := normalizeWebhookPayload(req.Payload, headers)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	decision := evaluateWebhookDelivery(webhookDeliveryState{
		TriggerEnabled:     trigger.Enabled,
		AutopilotStatus:    ap.Status,
		EventFilters:       trigger.EventFilters,
		EventMatchCriteria: trigger.EventMatchCriteria,
		Provider:           trigger.Provider,
		Envelope:           envelope,
	}, func(criteria, provider string, env WebhookEnvelope) webhookRoutingVerdict {
		return h.webhookEventMatchesCriteria(r.Context(), criteria, provider, env)
	})

	resp := DryRunWebhookTriggerResponse{
		WouldRun:       decision.Run,
		Explanation:    decision.Explanation,
		MatchedFilters: decision.MatchedFilters,
		Event:          envelope.Event,
	}
	if resp.MatchedFilters == nil {
		resp.MatchedFilters = []WebhookEventFilter{}
	}
	if decision.ReasonCode != "" {
		code := decision.ReasonCode
		resp.ReasonCode = &code
	}
	// Quota is charged at admission, after the routing chain, so it can only
	// turn a would-run into a would-not — never the other way round.
	if resp.WouldRun {
		if code, blocked := h.autopilotQuotaBlocks(r, workspaceID); blocked {
			resp.WouldRun = false
			resp.ReasonCode = &code
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// DryRunScheduleTriggerResponse answers "when would this fire, and would it".
type DryRunScheduleTriggerResponse struct {
	// NextRuns are the next windowed occurrences in RFC3339 UTC. Empty when
	// the expression has no further occurrence.
	NextRuns []string `json:"next_runs"`
	// WouldRun says whether the next occurrence would actually dispatch.
	WouldRun   bool    `json:"would_run"`
	ReasonCode *string `json:"reason_code"`
	// WindowMinutes echoes the band the occurrences were computed inside, so
	// the caller can say "sometime in the next 2 hours" rather than implying
	// the listed minute is exact.
	WindowMinutes int `json:"window_minutes"`
}

const scheduleDryRunOccurrences = 5

// DryRunAutopilotScheduleTrigger previews a schedule trigger: the next five
// firing instants (band offset included, exactly as the scheduler computes
// them) plus the state that would suppress the dispatch. Compute-only, so read
// access to the autopilot is enough.
func (h *Handler) DryRunAutopilotScheduleTrigger(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	ap, ok := h.loadAutopilotInWorkspace(w, r, chi.URLParam(r, "id"), workspaceID)
	if !ok {
		return
	}
	trigger, ok := h.loadTriggerForAutopilot(w, r, ap, chi.URLParam(r, "triggerId"))
	if !ok {
		return
	}
	if trigger.Kind != "schedule" {
		writeError(w, http.StatusBadRequest, "trigger is not a schedule trigger")
		return
	}
	if !trigger.CronExpression.Valid || trigger.CronExpression.String == "" {
		writeError(w, http.StatusBadRequest, "trigger has no cron expression")
		return
	}
	tz := trigger.Timezone.String
	if tz == "" {
		tz = "UTC"
	}

	windowMinutes := int(trigger.WindowMinutes)
	occurrences, err := nextPreviewOccurrences(
		trigger.CronExpression.String, tz, windowMinutes, scheduleDryRunOccurrences,
	)
	if err != nil {
		// A stored expression that no longer parses is a real state the
		// operator has to see, not a 500.
		writeCronPreviewError(w, cronPreviewInvalidCron, err.Error())
		return
	}
	nextRuns := make([]string, 0, len(occurrences))
	for _, at := range occurrences {
		nextRuns = append(nextRuns, at.Format(time.RFC3339))
	}

	resp := DryRunScheduleTriggerResponse{
		NextRuns:      nextRuns,
		WouldRun:      true,
		WindowMinutes: windowMinutes,
	}
	switch {
	case !trigger.Enabled:
		resp.WouldRun, resp.ReasonCode = false, ptrString(reasonTriggerDisabled)
	case ap.Status == "archived":
		resp.WouldRun, resp.ReasonCode = false, ptrString(reasonAutopilotArchived)
	case ap.Status != "active":
		resp.WouldRun, resp.ReasonCode = false, ptrString(reasonAutopilotPaused)
	default:
		if code, blocked := h.autopilotQuotaBlocks(r, workspaceID); blocked {
			resp.WouldRun, resp.ReasonCode = false, &code
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func ptrString(s string) *string { return &s }

// autopilotQuotaBlocks reports whether the workspace's run quota would reject
// a new run right now. Only an ENFORCING policy blocks: "observe" counts runs
// without refusing them, and a quota lookup failure must not invent a
// rejection the real path would not make.
func (h *Handler) autopilotQuotaBlocks(r *http.Request, workspaceID string) (string, bool) {
	if h.AutopilotService == nil {
		return "", false
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return "", false
	}
	usage, err := h.AutopilotService.AutopilotQuotaUsage(r.Context(), wsUUID)
	if err != nil || !usage.Enabled {
		return "", false
	}
	// "observe" counts runs without refusing them; only "enforce" rejects.
	if usage.Action != "enforce" || usage.Reached == nil || !*usage.Reached {
		return "", false
	}
	return reasonQuotaExceeded, true
}

// loadTriggerForAutopilot resolves a trigger id that must belong to the given
// autopilot. Cross-autopilot ids answer 404 rather than 403 so a guess leaks
// nothing about which ids exist.
func (h *Handler) loadTriggerForAutopilot(
	w http.ResponseWriter, r *http.Request, ap db.Autopilot, triggerID string,
) (db.AutopilotTrigger, bool) {
	triggerUUID, ok := parseUUIDOrBadRequest(w, triggerID, "trigger id")
	if !ok {
		return db.AutopilotTrigger{}, false
	}
	trigger, err := h.Queries.GetAutopilotTrigger(r.Context(), triggerUUID)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to load trigger")
			return db.AutopilotTrigger{}, false
		}
		writeError(w, http.StatusNotFound, "trigger not found")
		return db.AutopilotTrigger{}, false
	}
	if uuidToString(trigger.AutopilotID) != uuidToString(ap.ID) {
		writeError(w, http.StatusNotFound, "trigger not found")
		return db.AutopilotTrigger{}, false
	}
	return trigger, true
}
