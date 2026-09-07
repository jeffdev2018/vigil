package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	skillpkg "github.com/multica-ai/multica/server/internal/skill"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// DAEMON.md import / export (F24 / JEF-15).
//
// A daemon is not a new entity: it is a DECLARATION that projects onto the
// autopilot machinery that already exists — one autopilot, its triggers, and
// one skill bound to its agent. The file is the source of truth, so an import
// is idempotent (identical digest = no write at all), replaces rather than
// merges what it declares, and keeps the document verbatim so the export is
// the file the operator wrote rather than a reconstruction.

const (
	daemonImportStrategyFail      = "fail"
	daemonImportStrategyOverwrite = "overwrite"
	daemonImportStrategyRename    = "rename"

	// maxDaemonMarkdownBytes bounds an uploaded declaration. A DAEMON.md is a
	// page of configuration plus a skill body; anything past this is not one.
	maxDaemonMarkdownBytes = 1 << 20 // 1 MiB

	// maxDaemonRenameAttempts bounds the suffix search for `strategy=rename`.
	maxDaemonRenameAttempts = 50
)

type ImportDaemonRequest struct {
	Markdown string `json:"markdown"`
	Strategy string `json:"strategy"`
}

// DaemonFrontmatterView is the parsed declaration as the UI shows it back.
type DaemonFrontmatterView struct {
	Name               string                   `json:"name"`
	Role               string                   `json:"role"`
	Agent              string                   `json:"agent"`
	Triggers           []skillpkg.DaemonTrigger `json:"triggers"`
	Budget             *skillpkg.DaemonBudget   `json:"budget,omitempty"`
	Outputs            string                   `json:"outputs"`
	IssueTitleTemplate string                   `json:"issue_title_template,omitempty"`
}

type DaemonImportPreviewResponse struct {
	Valid       bool                        `json:"valid"`
	Errors      []skillpkg.DaemonParseError `json:"errors"`
	Frontmatter *DaemonFrontmatterView      `json:"frontmatter,omitempty"`
	// Body is the skill content the daemon's agent would receive, verbatim.
	Body string `json:"body"`
	// Warnings are accepted-but-inert declarations. `budget` is the only one
	// today: it round-trips through the stored document and enforces nothing,
	// because run quota is workspace-scoped, not per-autopilot.
	Warnings []string `json:"warnings,omitempty"`
	// AgentID is the resolved assignee when the named agent exists; empty when
	// it does not, with AgentCandidates naming what the workspace does have.
	AgentID         string   `json:"agent_id,omitempty"`
	AgentCandidates []string `json:"agent_candidates,omitempty"`
	// Digest identifies the document. A re-import of the same digest is a
	// no-op, so the UI can say "nothing would change" before writing.
	Digest string `json:"digest"`
	// ExistingAutopilotID is set when a daemon of this name already exists, so
	// the dialog can offer overwrite / rename before the write is attempted.
	ExistingAutopilotID string `json:"existing_autopilot_id,omitempty"`
	Unchanged           bool   `json:"unchanged"`
}

type DaemonImportResponse struct {
	// Status is created | updated | unchanged.
	Status    string                     `json:"status"`
	Autopilot AutopilotResponse          `json:"autopilot"`
	Triggers  []AutopilotTriggerResponse `json:"triggers"`
	SkillID   string                     `json:"skill_id"`
	Digest    string                     `json:"digest"`
	Warnings  []string                   `json:"warnings,omitempty"`
}

func daemonDigest(markdown string) string {
	sum := sha256.Sum256([]byte(markdown))
	return hex.EncodeToString(sum[:])
}

func validDaemonStrategy(s string) bool {
	switch s {
	case "", daemonImportStrategyFail, daemonImportStrategyOverwrite, daemonImportStrategyRename:
		return true
	default:
		return false
	}
}

// daemonExecutionMode maps the declaration's `outputs` onto the column.
func daemonExecutionMode(outputs string) string {
	if outputs == skillpkg.DaemonOutputRunOnly {
		return "run_only"
	}
	return "create_issue"
}

// daemonOutputsFor is the inverse, for export.
func daemonOutputsFor(executionMode string) string {
	if executionMode == "run_only" {
		return skillpkg.DaemonOutputRunOnly
	}
	return skillpkg.DaemonOutputIssue
}

func daemonWarnings(doc skillpkg.DaemonDoc) []string {
	var warnings []string
	if doc.Budget != nil && (doc.Budget.RunsPerDay != nil || doc.Budget.MaxMinutes != nil) {
		// Said plainly rather than dropped silently: run quota lives on the
		// workspace (autopilot_quota_period), driven by the plan entitlement,
		// and there is no per-autopilot limit column to write this into. The
		// values survive in the stored document and come back on export, but
		// nothing enforces them today.
		warnings = append(warnings, "budget is recorded in the document but not enforced: run quota is workspace-wide, not per-daemon")
	}
	return warnings
}

func daemonFrontmatterView(doc skillpkg.DaemonDoc) *DaemonFrontmatterView {
	triggers := doc.Triggers
	if triggers == nil {
		triggers = []skillpkg.DaemonTrigger{}
	}
	return &DaemonFrontmatterView{
		Name:               doc.Name,
		Role:               doc.Role,
		Agent:              doc.Agent,
		Triggers:           triggers,
		Budget:             doc.Budget,
		Outputs:            doc.Outputs,
		IssueTitleTemplate: doc.IssueTitleTemplate,
	}
}

// readDaemonMarkdown pulls the declaration out of the request. A JSON body
// carries it inline; an uploaded DAEMON.md arrives as multipart, the same two
// shapes skill import accepts.
func readDaemonMarkdown(w http.ResponseWriter, r *http.Request) (markdown, strategy string, ok bool) {
	if isMultipartForm(r) {
		r.Body = http.MaxBytesReader(w, r.Body, maxDaemonMarkdownBytes)
		if err := r.ParseMultipartForm(maxDaemonMarkdownBytes); err != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart upload or file exceeds the size limit")
			return "", "", false
		}
		defer func() {
			if r.MultipartForm != nil {
				_ = r.MultipartForm.RemoveAll()
			}
		}()
		file, _, err := r.FormFile("file")
		if err != nil {
			writeError(w, http.StatusBadRequest, "a DAEMON.md file is required in the 'file' field")
			return "", "", false
		}
		defer file.Close()
		body, err := io.ReadAll(io.LimitReader(file, maxDaemonMarkdownBytes))
		if err != nil {
			writeError(w, http.StatusBadRequest, "failed to read the uploaded file")
			return "", "", false
		}
		return string(body), r.FormValue("strategy"), true
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxDaemonMarkdownBytes)
	var req ImportDaemonRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return "", "", false
	}
	return req.Markdown, req.Strategy, true
}

// validateDaemonDomain runs the checks the parser cannot: they need the
// autopilot domain (cron, issue title template), not YAML. Reported against
// the line the field was declared on, like every other parse error.
func validateDaemonDomain(doc skillpkg.DaemonDoc) []skillpkg.DaemonParseError {
	var errs []skillpkg.DaemonParseError
	if doc.IssueTitleTemplate != "" {
		if err := service.ValidateIssueTitleTemplate(doc.IssueTitleTemplate); err != nil {
			errs = append(errs, skillpkg.DaemonParseError{Line: doc.Lines["issue_title_template"], Message: err.Error()})
		}
	}
	for _, t := range doc.Triggers {
		if t.Kind != "schedule" || t.Cron == "" {
			continue
		}
		tz := t.Timezone
		if tz == "" {
			tz = "UTC"
		}
		if _, err := service.ComputeNextRun(t.Cron, tz); err != nil {
			errs = append(errs, skillpkg.DaemonParseError{Line: t.Line, Message: "invalid schedule: " + err.Error()})
		}
	}
	sort.SliceStable(errs, func(i, j int) bool { return errs[i].Line < errs[j].Line })
	return errs
}

// resolveDaemonAgent maps the declaration's `agent` onto a workspace agent.
// A UUID is taken as an id; anything else is matched on name, exactly first
// and case-insensitively second, so "Nova" and "nova" both land. Ambiguity is
// a failure, not a coin flip.
func (h *Handler) resolveDaemonAgent(ctx context.Context, wsUUID pgtype.UUID, ref string) (db.Agent, []string, error) {
	agents, err := h.Queries.ListAgents(ctx, wsUUID)
	if err != nil {
		return db.Agent{}, nil, err
	}
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		names = append(names, a.Name)
	}
	sort.Strings(names)

	for _, a := range agents {
		if uuidToString(a.ID) == ref {
			return a, names, nil
		}
	}
	var matches []db.Agent
	for _, a := range agents {
		if strings.EqualFold(strings.TrimSpace(a.Name), ref) {
			matches = append(matches, a)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], names, nil
	case 0:
		return db.Agent{}, names, fmt.Errorf("no agent named %q in this workspace", ref)
	default:
		return db.Agent{}, names, fmt.Errorf("agent name %q matches more than one agent; use the agent id", ref)
	}
}

// PreviewDaemonImport parses a declaration and reports what it would do,
// without writing anything.
func (h *Handler) PreviewDaemonImport(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	markdown, _, ok := readDaemonMarkdown(w, r)
	if !ok {
		return
	}

	doc, errs := skillpkg.ParseDaemonMarkdown(markdown)
	if len(errs) == 0 {
		errs = validateDaemonDomain(doc)
	}
	resp := DaemonImportPreviewResponse{
		Valid:  len(errs) == 0,
		Errors: errs,
		Body:   doc.Body,
		Digest: daemonDigest(markdown),
	}
	if resp.Errors == nil {
		resp.Errors = []skillpkg.DaemonParseError{}
	}
	if !resp.Valid {
		// A document that does not parse has nothing to preview beyond its
		// errors; showing half-parsed frontmatter would read as accepted.
		writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.Frontmatter = daemonFrontmatterView(doc)
	resp.Warnings = daemonWarnings(doc)

	agent, candidates, err := h.resolveDaemonAgent(r.Context(), wsUUID, doc.Agent)
	if err != nil {
		resp.AgentCandidates = candidates
	} else {
		resp.AgentID = uuidToString(agent.ID)
	}

	if existing, lookupErr := h.Queries.GetAutopilotByTitleForImport(r.Context(), db.GetAutopilotByTitleForImportParams{
		WorkspaceID: wsUUID,
		Title:       doc.Name,
	}); lookupErr == nil {
		resp.ExistingAutopilotID = uuidToString(existing.ID)
		resp.Unchanged = existing.SourceDigest.Valid && existing.SourceDigest.String == resp.Digest
	}

	writeJSON(w, http.StatusOK, resp)
}

// ImportDaemon creates or updates the autopilot, triggers and skill a
// DAEMON.md declares.
func (h *Handler) ImportDaemon(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	creatorUUID := parseUUID(userID)

	markdown, strategy, ok := readDaemonMarkdown(w, r)
	if !ok {
		return
	}
	if !validDaemonStrategy(strategy) {
		writeError(w, http.StatusBadRequest, "strategy must be one of: fail, overwrite, rename")
		return
	}
	if strategy == "" {
		strategy = daemonImportStrategyFail
	}

	doc, errs := skillpkg.ParseDaemonMarkdown(markdown)
	if len(errs) == 0 {
		errs = validateDaemonDomain(doc)
	}
	if len(errs) > 0 {
		// No partial write: validation is complete before the transaction is
		// opened, so a rejected document leaves nothing behind.
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "the daemon declaration is not valid",
			"code":   "daemon_invalid",
			"errors": errs,
		})
		return
	}

	agent, candidates, err := h.resolveDaemonAgent(r.Context(), wsUUID, doc.Agent)
	if err != nil {
		msg := err.Error()
		if len(candidates) > 0 {
			msg += "; agents in this workspace: " + strings.Join(candidates, ", ")
		}
		writeErrorCode(w, http.StatusNotFound, "daemon_agent_not_found", msg)
		return
	}

	digest := daemonDigest(markdown)
	// A daemon is addressed by its `name`, which maps to autopilot.title.
	// Reuses the workspace-transfer import lookup: same question, same
	// archived-rows-excluded answer, so a re-import after a delete creates a
	// fresh daemon instead of colliding with a tombstone.
	existing, lookupErr := h.Queries.GetAutopilotByTitleForImport(r.Context(), db.GetAutopilotByTitleForImportParams{
		WorkspaceID: wsUUID,
		Title:       doc.Name,
	})
	hasExisting := lookupErr == nil

	// Idempotence comes FIRST, before the conflict strategy: re-importing the
	// byte-identical document a repository already holds must be a no-op
	// whatever the strategy says, and must not move updated_at.
	if hasExisting && existing.SourceDigest.Valid && existing.SourceDigest.String == digest {
		triggers, _ := h.Queries.ListAutopilotTriggers(r.Context(), existing.ID)
		subs, _ := h.Queries.ListAutopilotSubscribers(r.Context(), existing.ID)
		skillID := ""
		if s, serr := h.Queries.GetSkillByWorkspaceAndName(r.Context(), db.GetSkillByWorkspaceAndNameParams{
			WorkspaceID: wsUUID,
			Name:        doc.Name,
		}); serr == nil {
			skillID = uuidToString(s.ID)
		}
		writeJSON(w, http.StatusOK, DaemonImportResponse{
			Status:    "unchanged",
			Autopilot: autopilotToResponse(existing, subs),
			Triggers:  h.triggersToResponses(triggers),
			SkillID:   skillID,
			Digest:    digest,
			Warnings:  daemonWarnings(doc),
		})
		return
	}

	title := doc.Name
	if hasExisting {
		switch strategy {
		case daemonImportStrategyOverwrite:
			actor, aok := h.requireAutopilotWrite(w, r, existing, workspaceID)
			if !aok {
				return
			}
			// The gate judges the human an agent-invoked import acts for, so
			// the overwrite is recorded against them, not the agent (MUL-7108).
			creatorUUID = actor.UserID
		case daemonImportStrategyRename:
			free, ferr := h.freeDaemonName(r.Context(), wsUUID, doc.Name)
			if ferr != nil {
				writeError(w, http.StatusConflict, ferr.Error())
				return
			}
			title = free
			hasExisting = false
		default:
			writeErrorCode(w, http.StatusConflict, "daemon_name_conflict",
				fmt.Sprintf("a daemon named %q already exists; import with strategy=overwrite to replace it or strategy=rename to keep both", doc.Name))
			return
		}
	}

	result, status, ierr := h.writeDaemon(r, wsUUID, creatorUUID, title, markdown, digest, doc, agent, existing, hasExisting)
	if ierr != nil {
		writeError(w, http.StatusInternalServerError, "failed to import daemon: "+ierr.Error())
		return
	}
	result.Warnings = daemonWarnings(doc)

	event := protocol.EventAutopilotCreated
	if status == http.StatusOK {
		event = protocol.EventAutopilotUpdated
	}
	h.publish(event, workspaceID, "member", userID, map[string]any{"autopilot": result.Autopilot})
	writeJSON(w, status, result)
}

// freeDaemonName finds an unused "<name>-N" for strategy=rename. Both the
// autopilot title and the skill name have to be free: they are two faces of
// the same daemon, so a name only half-available is not available.
func (h *Handler) freeDaemonName(ctx context.Context, wsUUID pgtype.UUID, base string) (string, error) {
	for suffix := 2; suffix < maxDaemonRenameAttempts+2; suffix++ {
		candidate := fmt.Sprintf("%s-%d", base, suffix)
		if _, err := h.Queries.GetAutopilotByTitleForImport(ctx, db.GetAutopilotByTitleForImportParams{
			WorkspaceID: wsUUID,
			Title:       candidate,
		}); err == nil {
			continue
		}
		if _, err := h.Queries.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{
			WorkspaceID: wsUUID,
			Name:        candidate,
		}); err == nil {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("could not find a free name after %d attempts", maxDaemonRenameAttempts)
}

// writeDaemon performs the whole projection in one transaction: the
// autopilot, its declared triggers, the skill and the agent binding either all
// land or none do. A half-imported daemon — an autopilot whose triggers say
// something the document does not — is worse than a failed import.
func (h *Handler) writeDaemon(
	r *http.Request,
	wsUUID, creatorUUID pgtype.UUID,
	title, markdown, digest string,
	doc skillpkg.DaemonDoc,
	agent db.Agent,
	existing db.Autopilot,
	hasExisting bool,
) (DaemonImportResponse, int, error) {
	ctx := r.Context()
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return DaemonImportResponse{}, 0, err
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	description := pgtype.Text{String: doc.Role, Valid: true}
	titleTemplate := pgtype.Text{String: doc.IssueTitleTemplate, Valid: doc.IssueTitleTemplate != ""}
	mode := daemonExecutionMode(doc.Outputs)

	var autopilot db.Autopilot
	status := http.StatusCreated
	if hasExisting {
		status = http.StatusOK
		autopilot, err = qtx.UpdateAutopilot(ctx, db.UpdateAutopilotParams{
			ID:                 existing.ID,
			Title:              pgtype.Text{String: title, Valid: true},
			Description:        description,
			AssigneeType:       pgtype.Text{String: "agent", Valid: true},
			AssigneeID:         agent.ID,
			ExecutionMode:      pgtype.Text{String: mode, Valid: true},
			IssueTitleTemplate: titleTemplate,
			ProjectID:          existing.ProjectID,
		})
		if err != nil {
			return DaemonImportResponse{}, 0, err
		}
		// A declaration that moved the assignee, the role or the output mode
		// changed what future runs do and on whose authority, so it republishes
		// the rule version — the same test UpdateAutopilot applies.
		if autopilotRuleSubstantiveChange(existing, autopilot) {
			if err := h.recordAutopilotRuleVersion(ctx, qtx, autopilot, "member", creatorUUID); err != nil {
				return DaemonImportResponse{}, 0, err
			}
		}
	} else {
		autopilot, err = qtx.CreateAutopilot(ctx, db.CreateAutopilotParams{
			WorkspaceID:        wsUUID,
			Title:              title,
			AssigneeType:       "agent",
			AssigneeID:         agent.ID,
			Status:             "active",
			ExecutionMode:      mode,
			BatchEligible:      false,
			CreatedByType:      "member",
			CreatedByID:        creatorUUID,
			Description:        description,
			IssueTitleTemplate: titleTemplate,
		})
		if err != nil {
			return DaemonImportResponse{}, 0, err
		}
		if err := h.recordAutopilotRuleVersion(ctx, qtx, autopilot, "member", creatorUUID); err != nil {
			return DaemonImportResponse{}, 0, err
		}
	}

	autopilot, err = qtx.SetAutopilotSource(ctx, db.SetAutopilotSourceParams{
		ID:             autopilot.ID,
		SourceMarkdown: pgtype.Text{String: markdown, Valid: true},
		SourceDigest:   pgtype.Text{String: digest, Valid: true},
	})
	if err != nil {
		return DaemonImportResponse{}, 0, err
	}

	triggers, err := h.replaceDaemonTriggers(ctx, qtx, autopilot, doc, creatorUUID)
	if err != nil {
		return DaemonImportResponse{}, 0, err
	}

	skill, err := upsertDaemonSkill(ctx, qtx, wsUUID, creatorUUID, title, doc)
	if err != nil {
		return DaemonImportResponse{}, 0, err
	}
	if err := qtx.AddAgentSkill(ctx, db.AddAgentSkillParams{AgentID: agent.ID, SkillID: skill.ID}); err != nil {
		return DaemonImportResponse{}, 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return DaemonImportResponse{}, 0, err
	}

	subs, err := h.Queries.ListAutopilotSubscribers(ctx, autopilot.ID)
	if err != nil {
		subs = nil
	}
	return DaemonImportResponse{
		Status:    map[bool]string{true: "updated", false: "created"}[hasExisting],
		Autopilot: autopilotToResponse(autopilot, subs),
		Triggers:  h.triggersToResponses(triggers),
		SkillID:   uuidToString(skill.ID),
		Digest:    digest,
	}, status, nil
}

// replaceDaemonTriggers makes the declared trigger list the whole truth. A
// declarative import REPLACES: a trigger the document dropped must stop
// firing, and merging would leave a schedule alive that the file says was
// deleted. Webhook tokens are minted fresh for the same reason — the document
// declares the trigger, not the URL.
func (h *Handler) replaceDaemonTriggers(ctx context.Context, qtx *db.Queries, ap db.Autopilot, doc skillpkg.DaemonDoc, publisherID pgtype.UUID) ([]db.AutopilotTrigger, error) {
	current, err := qtx.ListAutopilotTriggers(ctx, ap.ID)
	if err != nil {
		return nil, err
	}
	for _, t := range current {
		if err := qtx.DeleteAutopilotTrigger(ctx, t.ID); err != nil {
			return nil, err
		}
	}

	created := make([]db.AutopilotTrigger, 0, len(doc.Triggers))
	for _, t := range doc.Triggers {
		params := db.CreateAutopilotTriggerParams{
			AutopilotID:     ap.ID,
			Kind:            t.Kind,
			Enabled:         true,
			Label:           pgtype.Text{String: t.Label, Valid: t.Label != ""},
			PublishedByType: pgtype.Text{String: "member", Valid: publisherID.Valid},
			PublishedByID:   publisherID,
			CreatedByType:   pgtype.Text{String: "member", Valid: publisherID.Valid},
			CreatedByID:     publisherID,
		}
		switch t.Kind {
		case "schedule":
			tz := t.Timezone
			if tz == "" {
				tz = "UTC"
			}
			next, nerr := service.ComputeNextRun(t.Cron, tz)
			if nerr != nil {
				return nil, nerr
			}
			params.CronExpression = pgtype.Text{String: t.Cron, Valid: true}
			params.Timezone = pgtype.Text{String: tz, Valid: true}
			params.NextRunAt = pgtype.Timestamptz{Time: next, Valid: true}
		case "webhook":
			// One shot, no retry: the standalone create path retries on a
			// token collision because it owns its transaction, and a unique
			// violation inside this one would abort the whole import. A 256-bit
			// token makes the collision a non-event; if it ever happened the
			// import fails loudly rather than reusing someone else's URL.
			token, terr := generateWebhookToken()
			if terr != nil {
				return nil, terr
			}
			params.WebhookToken = pgtype.Text{String: token, Valid: true}
			params.Provider = pgtype.Text{String: "generic", Valid: true}
		}
		trigger, cerr := qtx.CreateAutopilotTrigger(ctx, params)
		if cerr != nil {
			return nil, cerr
		}
		created = append(created, trigger)
	}
	return created, nil
}

// upsertDaemonSkill writes the declaration's body as the skill named after the
// daemon. The skill is a PROJECTION of the daemon, not an independent object:
// the conflict strategy governs the daemon, and the skill of that name follows
// it. That is why an existing skill is updated in place rather than routed
// through a second conflict decision the operator would have to answer twice.
func upsertDaemonSkill(ctx context.Context, qtx *db.Queries, wsUUID, creatorUUID pgtype.UUID, name string, doc skillpkg.DaemonDoc) (db.Skill, error) {
	description := doc.Role
	config, err := json.Marshal(map[string]any{
		"origin": map[string]any{"kind": "daemon", "name": name},
	})
	if err != nil {
		return db.Skill{}, err
	}

	existing, err := qtx.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{
		WorkspaceID: wsUUID,
		Name:        name,
	})
	if err == nil {
		return qtx.UpdateSkill(ctx, db.UpdateSkillParams{
			ID:          existing.ID,
			Description: pgtype.Text{String: sanitizeNullBytes(description), Valid: true},
			Content:     pgtype.Text{String: sanitizeNullBytes(doc.Body), Valid: true},
			Config:      config,
		})
	}
	return qtx.CreateSkill(ctx, db.CreateSkillParams{
		WorkspaceID: wsUUID,
		Name:        sanitizeNullBytes(name),
		Description: sanitizeNullBytes(description),
		Content:     sanitizeNullBytes(doc.Body),
		Config:      config,
		CreatedBy:   creatorUUID,
	})
}

func (h *Handler) triggersToResponses(triggers []db.AutopilotTrigger) []AutopilotTriggerResponse {
	out := make([]AutopilotTriggerResponse, 0, len(triggers))
	for _, t := range triggers {
		out = append(out, h.triggerToResponse(t))
	}
	return out
}

// ExportDaemon returns the DAEMON.md for an autopilot.
//
// An autopilot imported from a document exports that document VERBATIM. The
// alternative — rebuilding it from columns — silently drops everything the
// schema has no home for, so a round-trip would quietly delete the operator's
// `budget` block and any comment they wrote. An autopilot that never had a
// document gets one rendered from its current configuration.
func (h *Handler) ExportDaemon(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)
	ap, ok := h.loadAutopilotInWorkspace(w, r, chi.URLParam(r, "id"), workspaceID)
	if !ok {
		return
	}

	markdown := ap.SourceMarkdown.String
	if !ap.SourceMarkdown.Valid || strings.TrimSpace(markdown) == "" {
		rendered, err := h.renderDaemonFromAutopilot(r, ap)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to render daemon markdown")
			return
		}
		markdown = rendered
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", "DAEMON.md"))
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, markdown)
}

func (h *Handler) renderDaemonFromAutopilot(r *http.Request, ap db.Autopilot) (string, error) {
	ctx := r.Context()
	doc := skillpkg.DaemonDoc{
		Name:               ap.Title,
		Role:               ap.Description.String,
		Agent:              uuidToString(ap.AssigneeID),
		Outputs:            daemonOutputsFor(ap.ExecutionMode),
		IssueTitleTemplate: ap.IssueTitleTemplate.String,
	}
	if agent, err := h.Queries.GetAgent(ctx, ap.AssigneeID); err == nil && strings.TrimSpace(agent.Name) != "" {
		doc.Agent = agent.Name
	}
	triggers, err := h.Queries.ListAutopilotTriggers(ctx, ap.ID)
	if err != nil {
		return "", err
	}
	for _, t := range triggers {
		if t.Kind != "schedule" && t.Kind != "webhook" {
			// Only the kinds a declaration can express round-trip. A retired
			// kind is skipped rather than written into a file that would then
			// fail to re-import.
			continue
		}
		entry := skillpkg.DaemonTrigger{Kind: t.Kind, Label: t.Label.String}
		if t.Kind == "schedule" {
			// cron / timezone belong to a schedule only. The column defaults
			// timezone to 'UTC' on every row, so copying it onto a webhook
			// entry would render a document the importer then rejects.
			entry.Cron = t.CronExpression.String
			entry.Timezone = t.Timezone.String
		}
		doc.Triggers = append(doc.Triggers, entry)
	}
	if skill, err := h.Queries.GetSkillByWorkspaceAndName(ctx, db.GetSkillByWorkspaceAndNameParams{
		WorkspaceID: ap.WorkspaceID,
		Name:        ap.Title,
	}); err == nil {
		doc.Body = skill.Content
	}
	return skillpkg.RenderDaemonMarkdown(doc)
}
