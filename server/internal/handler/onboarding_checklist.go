package handler

import (
	"log/slog"
	"net/http"

	"github.com/multica-ai/multica/server/internal/logger"
)

// Native onboarding (OS plan, chantier 5): the getting-started checklist a
// fresh workspace shows until every step is done. Read-only and cheap: one
// query. The steps are facts about the workspace, not a per-user progress
// record — a second member sees the same list, already ticked by the first.

// OnboardingChecklist is what the card renders.
type OnboardingChecklist struct {
	// RuntimeKind is "native", "daemon" or "none": what would claim a run.
	RuntimeKind string `json:"runtime_kind"`
	// NativeAvailable says the server can run the native runtime at all.
	NativeAvailable   bool  `json:"native_available"`
	RuntimeReady      bool  `json:"runtime_ready"`
	AgentCreated      bool  `json:"agent_created"`
	IssueCreated      bool  `json:"issue_created"`
	FirstRunCompleted bool  `json:"first_run_completed"`
	FirstDecision     bool  `json:"first_decision_answered"`
	Complete          bool  `json:"complete"`
	Agents            int64 `json:"agents"`
	Issues            int64 `json:"issues"`
	CompletedRuns     int64 `json:"completed_runs"`
}

// GetOnboardingChecklist: GET /api/onboarding/checklist (workspace via header).
func (h *Handler) GetOnboardingChecklist(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, workspaceID, "workspace not found"); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	counts, err := h.Queries.GetOnboardingChecklistCounts(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("onboarding checklist: counts failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load the checklist")
		return
	}
	nativeAvailable := h.NativeAgents.Available()
	out := OnboardingChecklist{
		NativeAvailable:   nativeAvailable,
		Agents:            counts.Agents,
		Issues:            counts.Issues,
		CompletedRuns:     counts.CompletedRuns,
		AgentCreated:      counts.Agents > 0,
		IssueCreated:      counts.Issues > 0,
		FirstRunCompleted: counts.CompletedRuns > 0,
		FirstDecision:     counts.AnsweredDecisions > 0,
		RuntimeKind:       "none",
	}
	switch {
	case counts.OnlineDaemonRuntimes > 0:
		out.RuntimeKind, out.RuntimeReady = "daemon", true
	case counts.NativeRuntimes > 0 && nativeAvailable:
		out.RuntimeKind, out.RuntimeReady = "native", true
	case counts.NativeRuntimes > 0:
		// The row exists but the server cannot run it: say so rather than
		// tick the step.
		out.RuntimeKind = "native"
	}
	out.Complete = out.RuntimeReady && out.AgentCreated && out.IssueCreated && out.FirstRunCompleted
	writeJSON(w, http.StatusOK, out)
}
