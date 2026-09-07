package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/insight"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// F27 (JEF-25): ask the workspace a question in plain language, get a figure,
// pin it.
//
// The one decision this file exists to enforce: the model never writes SQL. It
// emits a document conforming to the closed DSL in internal/insight, the server
// compiles that document, and a pinned widget re-executes the stored document
// through /run without touching the model again.

const (
	insightWidgetMaxNameRunes     = 80
	insightWidgetMaxQuestionRunes = 500
	insightQuestionMaxRunes       = 500
	// A widget's display blob is chart preferences, not a second document.
	insightDisplayMaxBytes = 4096
)

type InsightAskRequest struct {
	Question string `json:"question"`
}

type InsightAskResponse struct {
	Query      *insight.Query `json:"query"`
	Rows       []insight.Row  `json:"rows"`
	Shape      string         `json:"shape"`
	Warnings   []string       `json:"warnings"`
	DurationMS int            `json:"duration_ms"`
}

type InsightRunRequest struct {
	Query json.RawMessage `json:"query"`
}

type InsightRunResponse struct {
	Rows       []insight.Row `json:"rows"`
	Shape      string        `json:"shape"`
	Warnings   []string      `json:"warnings"`
	DurationMS int           `json:"duration_ms"`
}

type InsightWidgetResponse struct {
	ID                string          `json:"id"`
	WorkspaceID       string          `json:"workspace_id"`
	OwnerID           string          `json:"owner_id"`
	Name              string          `json:"name"`
	Question          string          `json:"question"`
	DefinitionVersion int             `json:"definition_version"`
	Query             json.RawMessage `json:"query"`
	Display           json.RawMessage `json:"display"`
	Visibility        string          `json:"visibility"`
	Position          float64         `json:"position"`
	Revision          int             `json:"revision"`
	CreatedAt         string          `json:"created_at"`
	UpdatedAt         string          `json:"updated_at"`
}

type CreateInsightWidgetRequest struct {
	Name       string          `json:"name"`
	Question   string          `json:"question"`
	Query      json.RawMessage `json:"query"`
	Display    json.RawMessage `json:"display"`
	Visibility string          `json:"visibility"`
}

type UpdateInsightWidgetRequest struct {
	Name       *string         `json:"name"`
	Question   *string         `json:"question"`
	Query      json.RawMessage `json:"query"`
	Display    json.RawMessage `json:"display"`
	Visibility *string         `json:"visibility"`
	Position   *float64        `json:"position"`
	// ExpectedRevision is the value the client read. Omitted (0) means "I did
	// not check", and the update is refused rather than silently clobbering.
	ExpectedRevision int `json:"expected_revision"`
}

func insightWidgetToResponse(wgt db.InsightWidget) InsightWidgetResponse {
	display := wgt.Display
	if len(display) == 0 {
		display = []byte("{}")
	}
	return InsightWidgetResponse{
		ID:                uuidToString(wgt.ID),
		WorkspaceID:       uuidToString(wgt.WorkspaceID),
		OwnerID:           uuidToString(wgt.OwnerID),
		Name:              wgt.Name,
		Question:          wgt.Question,
		DefinitionVersion: int(wgt.DefinitionVersion),
		Query:             json.RawMessage(wgt.Query),
		Display:           json.RawMessage(display),
		Visibility:        wgt.Visibility,
		Position:          wgt.Position,
		Revision:          int(wgt.Revision),
		CreatedAt:         timestampToString(wgt.CreatedAt),
		UpdatedAt:         timestampToString(wgt.UpdatedAt),
	}
}

// AskInsight translates a plain-language question, executes the resulting
// document, and answers with both — the document travels back so the client can
// pin it without asking the model a second time.
func (h *Handler) AskInsight(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req InsightAskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	question := strings.TrimSpace(util.SanitizeTextForPostgres(req.Question))
	if question == "" || utf8.RuneCountInString(question) > insightQuestionMaxRunes {
		writeError(w, http.StatusBadRequest, "question is required")
		return
	}

	userUUID, _ := util.ParseUUID(userID)
	started := time.Now()

	// Model empty: the deployment's configured default. Insights have no
	// reason to pin a different one.
	translator := &insight.Translator{LLM: h.insightLLM()}
	query, err := translator.Translate(r.Context(), question, h.insightVocabulary(r.Context(), workspaceID))
	if err != nil {
		outcome := insight.OutcomeRefused
		status := http.StatusServiceUnavailable
		message := "no assistant is configured for this deployment"
		switch {
		case errors.Is(err, insight.ErrNotConfigured):
		case errors.Is(err, insight.ErrUntranslatable):
			// The vocabulary could not express the question. That is a product
			// signal, not a server fault: 422 so the client keeps the text in
			// the box and invites a rephrase.
			outcome = insight.OutcomeInvalid
			status = http.StatusUnprocessableEntity
			message = "could not turn this question into a query"
		default:
			outcome = insight.OutcomeRefused
			status = http.StatusBadGateway
			message = "the assistant did not answer"
		}
		h.logInsightQuery(r.Context(), workspaceID, userUUID, question, nil, outcome, time.Since(started))
		writeError(w, status, message)
		return
	}

	result, verr, runErr := insight.Run(r.Context(), h.ReadSelector, query, workspaceID)
	if verr != nil {
		// Unreachable in practice — Translate only returns validated
		// documents — but a compile rejection must never become an execution.
		h.logInsightQuery(r.Context(), workspaceID, userUUID, question, query, insight.OutcomeInvalid, time.Since(started))
		writeError(w, http.StatusBadRequest, verr.Error())
		return
	}
	if runErr != nil {
		h.finishInsightRun(w, r, workspaceID, userUUID, question, query, runErr, started)
		return
	}

	result.Rows = h.redactInsightRows(r, workspaceID, query, result.Rows)
	h.logInsightQuery(r.Context(), workspaceID, userUUID, question, query, insight.OutcomeOK, time.Since(started))
	writeJSON(w, http.StatusOK, InsightAskResponse{
		Query:      query,
		Rows:       result.Rows,
		Shape:      result.Shape,
		Warnings:   result.Warnings,
		DurationMS: result.DurationMS,
	})
}

// RunInsight executes a document that already exists. This is what a pinned
// widget calls on every refresh: no model, no translation, no drift.
func (h *Handler) RunInsight(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req InsightRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.Query) == 0 {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	userUUID, _ := util.ParseUUID(userID)
	started := time.Now()

	query, decodeErr := insight.DecodeQuery(req.Query)
	if decodeErr != nil {
		h.logInsightQuery(r.Context(), workspaceID, userUUID, "", nil, insight.OutcomeInvalid, time.Since(started))
		writeError(w, http.StatusBadRequest, decodeErr.Error())
		return
	}

	result, verr, runErr := insight.Run(r.Context(), h.ReadSelector, query, workspaceID)
	if verr != nil {
		h.logInsightQuery(r.Context(), workspaceID, userUUID, "", query, insight.OutcomeInvalid, time.Since(started))
		writeError(w, http.StatusBadRequest, verr.Error())
		return
	}
	if runErr != nil {
		h.finishInsightRun(w, r, workspaceID, userUUID, "", query, runErr, started)
		return
	}

	result.Rows = h.redactInsightRows(r, workspaceID, query, result.Rows)
	h.logInsightQuery(r.Context(), workspaceID, userUUID, "", query, insight.OutcomeOK, time.Since(started))
	writeJSON(w, http.StatusOK, InsightRunResponse{
		Rows:       result.Rows,
		Shape:      result.Shape,
		Warnings:   result.Warnings,
		DurationMS: result.DurationMS,
	})
}

// finishInsightRun maps an execution failure onto a status and a log outcome.
// A timeout is 503 and its own outcome, because it is the one failure the user
// can act on by narrowing the question.
func (h *Handler) finishInsightRun(
	w http.ResponseWriter,
	r *http.Request,
	workspaceID, userUUID pgtype.UUID,
	question string,
	query *insight.Query,
	err error,
	started time.Time,
) {
	if errors.Is(err, insight.ErrTimeout) {
		h.logInsightQuery(r.Context(), workspaceID, userUUID, question, query, insight.OutcomeTimeout, time.Since(started))
		writeError(w, http.StatusServiceUnavailable, "this question took too long to answer; narrow the time range or the grouping")
		return
	}
	logger := slog.Default()
	logger.Error("insight query failed", "workspace_id", uuidToString(workspaceID), "error", err)
	h.logInsightQuery(r.Context(), workspaceID, userUUID, question, query, insight.OutcomeRefused, time.Since(started))
	writeError(w, http.StatusInternalServerError, "failed to run insight query")
}

// redactInsightRows folds agents the requester may not see into a single
// `restricted` bucket. Grouping by assignee would otherwise leak the existence
// (and the workload) of another member's private agent to anyone who can ask a
// question — the aggregation is a read of the same data the agent list already
// gates.
func (h *Handler) redactInsightRows(
	r *http.Request,
	workspaceID pgtype.UUID,
	query *insight.Query,
	rows []insight.Row,
) []insight.Row {
	column := ""
	for _, dim := range query.GroupBy {
		if dim == "assignee" || dim == "agent" {
			column = dim
			break
		}
	}
	if column == "" || len(rows) == 0 {
		return rows
	}
	userID := requestUserID(r)
	if userID == "" {
		return rows
	}
	wsString := uuidToString(workspaceID)
	actorType, actorID := h.resolveActor(r, userID, wsString)
	role := ""
	if member, err := h.getWorkspaceMember(r.Context(), userID, wsString); err == nil {
		role = member.Role
	}
	restricted, ok := h.restrictedAgentIDs(r.Context(), wsString, actorType, actorID, role)
	if !ok || len(restricted) == 0 {
		return rows
	}

	merged := make([]insight.Row, 0, len(rows))
	restrictedIndex := -1
	for _, row := range rows {
		key, _ := row[column].(string)
		// The assignee dimension emits "<type>:<uuid>"; the agent dimension
		// emits the bare uuid.
		id := key
		if idx := strings.IndexByte(key, ':'); idx >= 0 {
			if key[:idx] != "agent" {
				merged = append(merged, row)
				continue
			}
			id = key[idx+1:]
		}
		if _, hidden := restricted[id]; !hidden {
			merged = append(merged, row)
			continue
		}
		if restrictedIndex < 0 {
			row[column] = "restricted"
			merged = append(merged, row)
			restrictedIndex = len(merged) - 1
			continue
		}
		// Several hidden agents collapse into one bucket, so the count is
		// still right without saying how many agents produced it.
		if a, aok := merged[restrictedIndex]["value"].(float64); aok {
			if b, bok := row["value"].(float64); bok {
				merged[restrictedIndex]["value"] = a + b
			}
		}
	}
	return merged
}

// ListInsightWidgets returns the widgets this member may see: their own, plus
// everything shared with the workspace.
func (h *Handler) ListInsightWidgets(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	rows, err := h.Queries.ListInsightWidgets(r.Context(), db.ListInsightWidgetsParams{
		WorkspaceID: workspaceID,
		ViewerID:    userUUID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list insight widgets")
		return
	}
	out := make([]InsightWidgetResponse, len(rows))
	for i, row := range rows {
		out[i] = insightWidgetToResponse(row)
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) CreateInsightWidget(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}
	var req CreateInsightWidgetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	name, nameOK := validateInsightName(req.Name)
	if !nameOK {
		writeError(w, http.StatusBadRequest, "name must be 1-80 characters")
		return
	}
	if len(req.Query) == 0 {
		writeError(w, http.StatusBadRequest, "query is required")
		return
	}
	// Pinning an invalid document would create a widget that can never load.
	query, decodeErr := insight.DecodeQuery(req.Query)
	if decodeErr != nil {
		writeError(w, http.StatusBadRequest, decodeErr.Error())
		return
	}
	visibility, visOK := h.resolveInsightVisibility(r, req.Visibility, userID, uuidToString(workspaceID))
	if !visOK {
		writeError(w, http.StatusForbidden, "only a workspace owner or admin can share an insight")
		return
	}
	display, displayOK := normalizeInsightDisplay(req.Display)
	if !displayOK {
		writeError(w, http.StatusBadRequest, "display must be a small JSON object")
		return
	}
	encoded, err := json.Marshal(query)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid query")
		return
	}

	created, err := h.Queries.CreateInsightWidget(r.Context(), db.CreateInsightWidgetParams{
		WorkspaceID: workspaceID,
		OwnerID:     userUUID,
		Name:        name,
		Question:    truncateRunes(util.SanitizeTextForPostgres(req.Question), insightWidgetMaxQuestionRunes),
		Query:       encoded,
		Display:     display,
		Visibility:  visibility,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create insight widget")
		return
	}
	writeJSON(w, http.StatusCreated, insightWidgetToResponse(created))
}

func (h *Handler) UpdateInsightWidget(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	// The path segment is a pure UUID, never a human-readable id.
	widgetID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	var req UpdateInsightWidgetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ExpectedRevision <= 0 {
		writeError(w, http.StatusBadRequest, "expected_revision is required")
		return
	}

	existing, err := h.Queries.GetInsightWidget(r.Context(), db.GetInsightWidgetParams{
		ID:          widgetID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "insight widget not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load insight widget")
		return
	}
	// A shared widget is readable by everyone and writable by its owner and
	// workspace admins; a private one is its owner's alone.
	if uuidToString(existing.OwnerID) != userID {
		member, mok := h.requireWorkspaceMember(w, r, uuidToString(workspaceID), "insight widget not found")
		if !mok {
			return
		}
		if existing.Visibility != "workspace" || !roleAllowed(member.Role, "owner", "admin") {
			writeError(w, http.StatusForbidden, "you cannot edit this insight")
			return
		}
	}

	params := db.UpdateInsightWidgetParams{
		ID:               widgetID,
		WorkspaceID:      workspaceID,
		ExpectedRevision: int32(req.ExpectedRevision),
	}
	if req.Name != nil {
		name, nameOK := validateInsightName(*req.Name)
		if !nameOK {
			writeError(w, http.StatusBadRequest, "name must be 1-80 characters")
			return
		}
		params.Name = pgtype.Text{String: name, Valid: true}
	}
	if req.Question != nil {
		params.Question = pgtype.Text{
			String: truncateRunes(util.SanitizeTextForPostgres(*req.Question), insightWidgetMaxQuestionRunes),
			Valid:  true,
		}
	}
	if len(req.Query) > 0 {
		query, decodeErr := insight.DecodeQuery(req.Query)
		if decodeErr != nil {
			writeError(w, http.StatusBadRequest, decodeErr.Error())
			return
		}
		encoded, err := json.Marshal(query)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid query")
			return
		}
		params.Query = encoded
	}
	if req.Display != nil {
		display, displayOK := normalizeInsightDisplay(req.Display)
		if !displayOK {
			writeError(w, http.StatusBadRequest, "display must be a small JSON object")
			return
		}
		params.Display = display
	}
	if req.Visibility != nil {
		visibility, visOK := h.resolveInsightVisibility(r, *req.Visibility, userID, uuidToString(workspaceID))
		if !visOK {
			writeError(w, http.StatusForbidden, "only a workspace owner or admin can share an insight")
			return
		}
		params.Visibility = pgtype.Text{String: visibility, Valid: true}
	}
	if req.Position != nil {
		params.Position = pgtype.Float8{Float64: *req.Position, Valid: true}
	}

	updated, err := h.Queries.UpdateInsightWidget(r.Context(), params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// The row exists (we just read it), so no match means the
			// revision moved: someone else saved first.
			writeError(w, http.StatusConflict, "this insight changed since you loaded it")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to update insight widget")
		return
	}
	writeJSON(w, http.StatusOK, insightWidgetToResponse(updated))
}

func (h *Handler) DeleteInsightWidget(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	widgetID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	existing, err := h.Queries.GetInsightWidget(r.Context(), db.GetInsightWidgetParams{
		ID:          widgetID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "insight widget not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load insight widget")
		return
	}
	if uuidToString(existing.OwnerID) != userID {
		member, mok := h.requireWorkspaceMember(w, r, uuidToString(workspaceID), "insight widget not found")
		if !mok {
			return
		}
		if !roleAllowed(member.Role, "owner", "admin") {
			writeError(w, http.StatusForbidden, "you cannot delete this insight")
			return
		}
	}
	if _, err := h.Queries.DeleteInsightWidget(r.Context(), db.DeleteInsightWidgetParams{
		ID:          widgetID,
		WorkspaceID: workspaceID,
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete insight widget")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveInsightVisibility defaults to private and gates sharing on the
// workspace role, so a regular member cannot publish a chart to everyone.
func (h *Handler) resolveInsightVisibility(r *http.Request, requested, userID, workspaceID string) (string, bool) {
	switch requested {
	case "", "private":
		return "private", true
	case "workspace":
		member, err := h.getWorkspaceMember(r.Context(), userID, workspaceID)
		if err != nil || !roleAllowed(member.Role, "owner", "admin") {
			return "", false
		}
		return "workspace", true
	default:
		return "", false
	}
}

func validateInsightName(raw string) (string, bool) {
	name := strings.TrimSpace(util.SanitizeTextForPostgres(raw))
	if name == "" || utf8.RuneCountInString(name) > insightWidgetMaxNameRunes {
		return "", false
	}
	return name, true
}

// normalizeInsightDisplay accepts only a small JSON object. It is chart
// preferences (a colour, a pinned unit), never a second query.
func normalizeInsightDisplay(raw json.RawMessage) ([]byte, bool) {
	if len(raw) == 0 {
		return []byte("{}"), true
	}
	if len(raw) > insightDisplayMaxBytes {
		return nil, false
	}
	var probe map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, false
	}
	return raw, true
}

func truncateRunes(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	return string(runes[:max])
}

// logInsightQuery records one asked question. Best-effort by design: a failed
// log write must never fail the answer the user is waiting for.
func (h *Handler) logInsightQuery(
	ctx context.Context,
	workspaceID, userID pgtype.UUID,
	question string,
	query *insight.Query,
	outcome string,
	elapsed time.Duration,
) {
	var compiled []byte
	if query != nil {
		if encoded, err := json.Marshal(query); err == nil {
			compiled = encoded
		}
	}
	err := h.Queries.CreateInsightQueryLog(ctx, db.CreateInsightQueryLogParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Question:    truncateRunes(question, insightQuestionMaxRunes),
		Compiled:    compiled,
		Outcome:     outcome,
		DurationMs:  int32(elapsed.Milliseconds()),
	})
	if err != nil {
		slog.Default().Warn("failed to record insight query log", "error", err, "outcome", outcome)
	}
}

// insightLLM returns the assist-layer client, or nil when the deployment has
// none. A nil client makes /ask answer 503 while pinned widgets keep working.
func (h *Handler) insightLLM() insight.LLM {
	if h.LLM == nil {
		return nil
	}
	return h.LLM
}

// insightVocabulary loads what the model is allowed to name. Every list is
// workspace-scoped; a failed load degrades to an empty section rather than
// failing the question, because a model with a partial vocabulary still beats
// no answer at all — and a document naming a key that does not exist compiles
// to a count of zero, never to another workspace's data.
func (h *Handler) insightVocabulary(ctx context.Context, workspaceID pgtype.UUID) insight.Vocabulary {
	var vocab insight.Vocabulary

	if statuses, err := h.Queries.ListIssueStatusEntries(ctx, db.ListIssueStatusEntriesParams{
		WorkspaceID:     workspaceID,
		IncludeArchived: false,
	}); err == nil {
		for _, s := range statuses {
			vocab.Statuses = append(vocab.Statuses, insight.VocabEntry{ID: s.Key, Name: s.Name, Note: s.Category})
		}
	}
	if projects, err := h.Queries.ListProjects(ctx, db.ListProjectsParams{WorkspaceID: workspaceID}); err == nil {
		for _, p := range projects {
			vocab.Projects = append(vocab.Projects, insight.VocabEntry{ID: uuidToString(p.ID), Name: p.Title})
		}
	}
	if labels, err := h.Queries.ListLabels(ctx, db.ListLabelsParams{
		WorkspaceID:  workspaceID,
		ResourceType: "issue",
	}); err == nil {
		for _, l := range labels {
			vocab.Labels = append(vocab.Labels, insight.VocabEntry{ID: uuidToString(l.ID), Name: l.Name})
		}
	}
	if types, err := h.Queries.ListIssueTypeEntries(ctx, db.ListIssueTypeEntriesParams{
		WorkspaceID:     workspaceID,
		IncludeArchived: false,
	}); err == nil {
		for _, it := range types {
			vocab.IssueTypes = append(vocab.IssueTypes, insight.VocabEntry{ID: it.Key, Name: it.Name})
		}
	}
	if properties, err := h.Queries.ListIssueProperties(ctx, db.ListIssuePropertiesParams{
		WorkspaceID:     workspaceID,
		IncludeArchived: false,
	}); err == nil {
		for _, p := range properties {
			vocab.Properties = append(vocab.Properties, insight.VocabEntry{
				ID:   uuidToString(p.ID),
				Name: p.Name,
				Note: p.Type,
			})
		}
	}
	return vocab
}
