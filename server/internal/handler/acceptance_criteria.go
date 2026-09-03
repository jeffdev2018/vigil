package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// Outcome Contract (K12): every acceptance criterion carries its proof, and
// an issue leaves the done category alone while one criterion lacks a
// satisfied proof. The criteria live in the issue's `acceptance_criteria`
// JSONB; older rows hold bare strings, which read as criteria without proof.

const (
	// ErrCodeUnsatisfiedAcceptanceCriteria is the stable 409 code a client can
	// key on when the contract refuses a move to done.
	ErrCodeUnsatisfiedAcceptanceCriteria = "unsatisfied_acceptance_criteria"

	acceptanceMaxCriteria = 50
	acceptanceMaxTextLen  = 2 << 10
	acceptanceMaxRefLen   = 2 << 10

	ProofStateMissing      = "missing"
	ProofStatePendingHuman = "pending_human"
	ProofStateSatisfied    = "satisfied"

	ProofTypeHumanValidation = "human_validation"
)

// acceptanceProofTypes are the proofs a criterion accepts. A human_validation
// from a machine credential only reaches pending_human: the human's own click
// is the proof, not the agent's claim of it.
var acceptanceProofTypes = map[string]bool{
	"test": true, "file": true, "screenshot": true, "url": true, ProofTypeHumanValidation: true,
}

type AcceptanceCriterion struct {
	ID         string `json:"id"`
	Text       string `json:"text"`
	ProofType  string `json:"proof_type,omitempty"`
	ProofRef   string `json:"proof_ref,omitempty"`
	ProofState string `json:"proof_state"`
	// ValidatedBy is the member who clicked a human_validation; empty otherwise.
	ValidatedBy string `json:"validated_by,omitempty"`
	ProvedAt    string `json:"proved_at,omitempty"`
}

// proofState derives the state from the proof fields so a row edited by hand
// or by an older writer can never claim satisfied without a proof behind it.
func (c AcceptanceCriterion) proofState() string {
	switch {
	case c.ProofType == "":
		return ProofStateMissing
	case c.ProofType == ProofTypeHumanValidation && c.ValidatedBy == "":
		return ProofStatePendingHuman
	case c.ProofType != ProofTypeHumanValidation && c.ProofRef == "":
		return ProofStateMissing
	default:
		return ProofStateSatisfied
	}
}

// parseAcceptanceCriteria reads the column defensively: strings become
// proof-less criteria with positional ids, objects are normalized, anything
// else is dropped. Malformed content reads as no criteria, never as satisfied.
func parseAcceptanceCriteria(raw []byte) []AcceptanceCriterion {
	out := []AcceptanceCriterion{}
	if len(raw) == 0 {
		return out
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		slog.Warn("acceptance criteria unreadable, treating as none", "error", err)
		return out
	}
	for i, item := range items {
		var text string
		if json.Unmarshal(item, &text) == nil {
			if strings.TrimSpace(text) != "" {
				out = append(out, AcceptanceCriterion{ID: fmt.Sprintf("c%d", i+1), Text: text, ProofState: ProofStateMissing})
			}
			continue
		}
		var c AcceptanceCriterion
		if json.Unmarshal(item, &c) != nil || strings.TrimSpace(c.Text) == "" {
			continue
		}
		if c.ID == "" {
			c.ID = fmt.Sprintf("c%d", i+1)
		}
		if !acceptanceProofTypes[c.ProofType] {
			c.ProofType, c.ProofRef, c.ValidatedBy, c.ProvedAt = "", "", "", ""
		}
		c.ProofState = c.proofState()
		out = append(out, c)
	}
	return out
}

func unsatisfiedAcceptanceCriteria(raw []byte) []AcceptanceCriterion {
	var out []AcceptanceCriterion
	for _, c := range parseAcceptanceCriteria(raw) {
		if c.ProofState != ProofStateSatisfied {
			out = append(out, c)
		}
	}
	return out
}

// acceptanceCriteriaAllowStatus writes the 409 and returns false when the
// target status behaves as done and a criterion still lacks its proof. The
// refusal names the criteria, so the client can show what is missing.
func (h *Handler) acceptanceCriteriaAllowStatus(w http.ResponseWriter, r *http.Request, issue db.Issue, statusKey string) bool {
	if issuestatus.Effective(r.Context(), h.Queries, issue.WorkspaceID, statusKey) != issuestatus.Done {
		return true
	}
	unsatisfied := unsatisfiedAcceptanceCriteria(issue.AcceptanceCriteria)
	if len(unsatisfied) == 0 {
		return true
	}
	names := make([]string, 0, len(unsatisfied))
	for _, c := range unsatisfied {
		names = append(names, "«"+c.Text+"»")
	}
	writeJSON(w, http.StatusConflict, map[string]any{
		"code":     ErrCodeUnsatisfiedAcceptanceCriteria,
		"error":    fmt.Sprintf("%d acceptance criteria lack proof: %s", len(unsatisfied), strings.Join(names, ", ")),
		"criteria": unsatisfied,
	})
	return false
}

func (h *Handler) writeAcceptanceCriteria(w http.ResponseWriter, r *http.Request, issue db.Issue, criteria []AcceptanceCriterion, userID string) {
	raw, err := json.Marshal(criteria)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode acceptance criteria")
		return
	}
	updated, err := h.Queries.UpdateIssueAcceptanceCriteria(r.Context(), db.UpdateIssueAcceptanceCriteriaParams{ID: issue.ID, AcceptanceCriteria: raw})
	if err != nil {
		slog.Warn("update acceptance criteria failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to update acceptance criteria")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(issue.WorkspaceID))
	h.publishIssueAuxChanged(r, updated, actorType, actorID)
	writeJSON(w, http.StatusOK, map[string]any{"criteria": parseAcceptanceCriteria(updated.AcceptanceCriteria)})
}

// ListAcceptanceCriteria — GET /api/issues/{id}/acceptance-criteria.
func (h *Handler) ListAcceptanceCriteria(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"criteria": parseAcceptanceCriteria(issue.AcceptanceCriteria)})
}

// SetAcceptanceCriteria — PUT /api/issues/{id}/acceptance-criteria replaces
// the list. A criterion keeps its proof when it comes back with the same id
// (or, id-less, the same text) and unchanged text; a reworded criterion is a
// new promise and starts without proof.
func (h *Handler) SetAcceptanceCriteria(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Criteria []struct {
			ID   string `json:"id"`
			Text string `json:"text"`
		} `json:"criteria"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Criteria) > acceptanceMaxCriteria {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("at most %d criteria", acceptanceMaxCriteria))
		return
	}
	existing := parseAcceptanceCriteria(issue.AcceptanceCriteria)
	byID := map[string]AcceptanceCriterion{}
	byText := map[string]AcceptanceCriterion{}
	for _, c := range existing {
		byID[c.ID] = c
		byText[c.Text] = c
	}
	next := make([]AcceptanceCriterion, 0, len(req.Criteria))
	seen := map[string]bool{}
	for _, in := range req.Criteria {
		text := strings.TrimSpace(in.Text)
		if text == "" || len(text) > acceptanceMaxTextLen {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("each criterion needs a text of at most %d bytes", acceptanceMaxTextLen))
			return
		}
		prev, found := byID[strings.TrimSpace(in.ID)]
		if !found && in.ID == "" {
			prev, found = byText[text]
		}
		c := AcceptanceCriterion{ID: strings.TrimSpace(in.ID), Text: text, ProofState: ProofStateMissing}
		if found {
			c.ID = prev.ID
			if prev.Text == text {
				c = prev
			}
		}
		if c.ID == "" {
			c.ID = uuidToString(dbid.NewV7())
		}
		if seen[c.ID] {
			writeError(w, http.StatusBadRequest, "criterion ids must be unique")
			return
		}
		seen[c.ID] = true
		next = append(next, c)
	}
	h.writeAcceptanceCriteria(w, r, issue, next, userID)
}

// ProveAcceptanceCriterion — PATCH /api/issues/{id}/acceptance-criteria/{criterionId}/proof
// attaches (or, with an empty proof_type, clears) the criterion's proof.
func (h *Handler) ProveAcceptanceCriterion(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		ProofType string `json:"proof_type"`
		ProofRef  string `json:"proof_ref"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.ProofType = strings.TrimSpace(req.ProofType)
	req.ProofRef = strings.TrimSpace(req.ProofRef)
	if req.ProofType != "" && !acceptanceProofTypes[req.ProofType] {
		writeError(w, http.StatusBadRequest, "proof_type must be one of test, file, screenshot, url, human_validation")
		return
	}
	if len(req.ProofRef) > acceptanceMaxRefLen {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("proof_ref is at most %d bytes", acceptanceMaxRefLen))
		return
	}
	if req.ProofType != "" && req.ProofType != ProofTypeHumanValidation && req.ProofRef == "" {
		writeError(w, http.StatusBadRequest, "proof_ref is required for this proof_type")
		return
	}
	criteria := parseAcceptanceCriteria(issue.AcceptanceCriteria)
	target := chi.URLParam(r, "criterionId")
	idx := -1
	for i := range criteria {
		if criteria[i].ID == target {
			idx = i
		}
	}
	if idx < 0 {
		writeError(w, http.StatusNotFound, "criterion not found")
		return
	}
	c := &criteria[idx]
	c.ProofType, c.ProofRef, c.ValidatedBy, c.ProvedAt = req.ProofType, req.ProofRef, "", ""
	if req.ProofType != "" {
		c.ProvedAt = time.Now().UTC().Format(time.RFC3339)
		if req.ProofType == ProofTypeHumanValidation && !isMachineCredentialActor(r) {
			c.ValidatedBy = userID
		}
	}
	c.ProofState = c.proofState()
	h.writeAcceptanceCriteria(w, r, issue, criteria, userID)
}
