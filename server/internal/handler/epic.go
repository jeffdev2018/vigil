package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Epic Mode (F18 / JEF-30): a project-level artifact pipeline.
//
// PRD -> tech plan -> wireframe -> tickets, with a human gate between each
// step. A step is written by a read-only agent run (the same shape as cross
// review and the PR walkthrough) or by a human, reviewed as a draft, then
// approved — and only an approved step unlocks the next one.
//
// The last step is the one with consequences: approving `tickets` lets a human
// apply the breakdown, which creates real child issues under the epic's host
// issue and links them with `blocked_by` edges. Everything before it is text.
//
// Two invariants carry the design:
//
//   - A row whose `content` is empty is a GENERATION CLAIM, not an artifact.
//     It is what `generating` reports, and the unique (project_id, kind,
//     version) index is the fence that keeps two concurrent generates from
//     claiming one version. Nothing else writes empty content: the PUT
//     rejects it and the settle hook treats an empty block as malformed.
//   - Approving is not a lock. Editing or regenerating an approved step
//     supersedes every LATER step, because those were approved against a
//     premise that just changed. The response says which steps that reopened.

const (
	epicKindPRD       = "prd"
	epicKindTechPlan  = "tech_plan"
	epicKindWireframe = "wireframe"
	epicKindTickets   = "tickets"

	epicStateDraft      = "draft"
	epicStateApproved   = "approved"
	epicStateSuperseded = "superseded"

	// epicContentMaxBytes bounds one artifact. A PRD is prose, not a corpus.
	epicContentMaxBytes = 128 << 10
	// epicMaxTickets caps one apply. Fifty child issues is already more than a
	// human will review in one sitting; past that the breakdown is wrong.
	epicMaxTickets = 50

	ErrCodeEpicStepLocked  = "epic_step_locked"
	ErrCodeEpicNoAgent     = "epic_no_agent"
	ErrCodeEpicGenerating  = "epic_generating"
	ErrCodeEpicNoDraft     = "epic_no_draft"
	ErrCodeEpicNotApproved = "epic_not_approved"
	ErrCodeTooManyTickets  = "too_many_tickets"
	ErrCodeEpicNoTickets   = "epic_no_tickets"
	ErrCodeEpicApplyFailed = "epic_apply_failed"

	AuditEpicStepGenerated = "epic_step.generated"
	AuditEpicStepEdited    = "epic_step.edited"
	AuditEpicStepApproved  = "epic_step.approved"
	AuditEpicTicketsApply  = "epic_tickets.applied"
	AuditEpicStepFailed    = "epic_step.failed"

	// EventEpicArtifactUpdated / Approved are published on workspace scope so
	// an open project panel re-queries the pipeline it is showing.
	EventEpicArtifactUpdated  = "epic_artifact:updated"
	EventEpicArtifactApproved = "epic_artifact:approved"
	EventEpicArtifactFailed   = "epic_artifact:failed"
)

// epicStepOrder is the pipeline. Index N requires index N-1 approved.
var epicStepOrder = []string{epicKindPRD, epicKindTechPlan, epicKindWireframe, epicKindTickets}

var epicStepFence = regexp.MustCompile("(?s)```epic_step\\s*(\\{.*\\})\\s*```")

// --- responses -------------------------------------------------------------

type EpicArtifactResponse struct {
	ID          string          `json:"id"`
	ProjectID   string          `json:"project_id"`
	Kind        string          `json:"kind"`
	Version     int32           `json:"version"`
	Content     string          `json:"content"`
	Payload     json.RawMessage `json:"payload"`
	State       string          `json:"state"`
	AuthorType  string          `json:"author_type"`
	AuthorID    string          `json:"author_id"`
	ApprovedBy  string          `json:"approved_by"`
	ApprovedAt  *string         `json:"approved_at"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
	GeneratedBy string          `json:"generated_by_task_id"`
}

// EpicStepResponse is one rail entry. `latest` and `approved` may be the same
// row; `generating` is a real answer, not an error — a run is out.
type EpicStepResponse struct {
	Kind       string                `json:"kind"`
	Latest     *EpicArtifactResponse `json:"latest"`
	Approved   *EpicArtifactResponse `json:"approved"`
	Generating bool                  `json:"generating"`
}

type EpicEnvelope struct {
	Steps       map[string]EpicStepResponse `json:"steps"`
	EpicIssueID string                      `json:"epic_issue_id"`
	// NextKind is the first step that is neither approved nor generating and
	// whose predecessor is approved. Empty when the pipeline is complete.
	NextKind string `json:"next_kind"`
}

type EpicGenerateResponse struct {
	TaskID string `json:"task_id"`
	Kind   string `json:"kind"`
}

// EpicStepWriteResponse carries the new draft plus the steps that editing it
// reopened, so the client can say so instead of silently losing an approval.
type EpicStepWriteResponse struct {
	Artifact       EpicArtifactResponse `json:"artifact"`
	ReopenedSteps  []string             `json:"reopened_steps"`
	EpicIssueID    string               `json:"epic_issue_id"`
	SupersededPrev bool                 `json:"superseded_previous"`
}

type EpicTicket struct {
	ExternalKey string   `json:"external_key"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	DependsOn   []string `json:"depends_on"`
}

type EpicApplyResponse struct {
	Created      []IssueResponse `json:"created"`
	Existing     []string        `json:"existing"`
	Dependencies int             `json:"dependencies"`
	EpicIssueID  string          `json:"epic_issue_id"`
}

func epicArtifactToResponse(a db.EpicArtifact) EpicArtifactResponse {
	payload := json.RawMessage(a.Payload)
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	return EpicArtifactResponse{
		ID:          uuidToString(a.ID),
		ProjectID:   uuidToString(a.ProjectID),
		Kind:        a.Kind,
		Version:     a.Version,
		Content:     a.Content,
		Payload:     payload,
		State:       a.State,
		AuthorType:  a.AuthorType,
		AuthorID:    uuidToString(a.AuthorID),
		ApprovedBy:  uuidToString(a.ApprovedBy),
		ApprovedAt:  timestampToPtr(a.ApprovedAt),
		CreatedAt:   timestampToString(a.CreatedAt),
		UpdatedAt:   timestampToString(a.UpdatedAt),
		GeneratedBy: uuidToString(a.GeneratedByTaskID),
	}
}

// --- the gate --------------------------------------------------------------

// epicIsClaim reports whether a row is a generation claim rather than a
// finished artifact. See the file header: empty content is the discriminator.
func epicIsClaim(a db.EpicArtifact) bool { return strings.TrimSpace(a.Content) == "" }

// epicSteps is the pipeline reduced to what every gate decision needs.
type epicSteps map[string]EpicStepResponse

// epicStepAllowed is the ONE gate. Generate, edit, approve and apply all ask
// it, so a step can never be reached down one path that another refuses.
// It returns the missing predecessor when the answer is no.
func epicStepAllowed(kind string, steps epicSteps) (bool, string) {
	for i, k := range epicStepOrder {
		if k != kind {
			continue
		}
		if i == 0 {
			return true, ""
		}
		prev := epicStepOrder[i-1]
		if steps[prev].Approved != nil {
			return true, ""
		}
		return false, prev
	}
	// An unknown kind never reaches here: the whitelist runs first.
	return false, ""
}

// epicNextKind is the step a human should work on next.
func epicNextKind(steps epicSteps) string {
	for _, k := range epicStepOrder {
		if steps[k].Approved != nil {
			continue
		}
		if ok, _ := epicStepAllowed(k, steps); ok {
			return k
		}
		return ""
	}
	return ""
}

// epicLaterKinds are the steps that come after kind — the ones an edit to kind
// reopens.
func epicLaterKinds(kind string) []string {
	for i, k := range epicStepOrder {
		if k == kind {
			return append([]string{}, epicStepOrder[i+1:]...)
		}
	}
	return nil
}

func epicKindValid(kind string) bool {
	for _, k := range epicStepOrder {
		if k == kind {
			return true
		}
	}
	return false
}

// --- loading ---------------------------------------------------------------

// loadEpicProject resolves {id} inside the request's workspace. It is the
// access check every epic endpoint runs: a project of another workspace is a
// 404, never a hint that it exists.
func (h *Handler) loadEpicProject(w http.ResponseWriter, r *http.Request) (db.Project, bool) {
	idUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "project id")
	if !ok {
		return db.Project{}, false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace id")
	if !ok {
		return db.Project{}, false
	}
	project, err := h.Queries.GetProjectInWorkspace(r.Context(), db.GetProjectInWorkspaceParams{
		ID: idUUID, WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "project not found")
		return db.Project{}, false
	}
	return project, true
}

// epicKindParam validates {kind} against the whitelist BEFORE any database
// access, so an unknown kind costs a string compare rather than a query.
func epicKindParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	kind := strings.TrimSpace(chi.URLParam(r, "kind"))
	if !epicKindValid(kind) {
		writeError(w, http.StatusBadRequest,
			fmt.Sprintf("invalid step %q; valid steps: %s", kind, strings.Join(epicStepOrder, ", ")))
		return "", false
	}
	return kind, true
}

// loadEpicSteps reads the whole pipeline of a project. `latest` skips
// generation claims; `approved` is the single approved row per kind.
func (h *Handler) loadEpicSteps(ctx context.Context, project db.Project) (epicSteps, string, error) {
	rows, err := h.Queries.ListEpicArtifacts(ctx, db.ListEpicArtifactsParams{
		ProjectID: project.ID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		return nil, "", err
	}
	steps := epicSteps{}
	for _, k := range epicStepOrder {
		steps[k] = EpicStepResponse{Kind: k}
	}
	epicIssueID := ""
	for _, row := range rows {
		if epicIssueID == "" && row.EpicIssueID.Valid {
			epicIssueID = uuidToString(row.EpicIssueID)
		}
		// A kind outside the pipeline can only come from a newer build writing
		// into this table; keep it out of the rail rather than 500.
		step, known := steps[row.Kind]
		if !known {
			continue
		}
		if epicIsClaim(row) {
			step.Generating = true
			steps[row.Kind] = step
			continue
		}
		resp := epicArtifactToResponse(row)
		if step.Latest == nil {
			latest := resp
			step.Latest = &latest
		}
		if row.State == epicStateApproved && step.Approved == nil {
			approved := resp
			step.Approved = &approved
		}
		steps[row.Kind] = step
	}
	return steps, epicIssueID, nil
}

// --- read ------------------------------------------------------------------

// GetProjectEpic: GET /api/projects/{id}/epic.
func (h *Handler) GetProjectEpic(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadEpicProject(w, r)
	if !ok {
		return
	}
	steps, epicIssueID, err := h.loadEpicSteps(r.Context(), project)
	if err != nil {
		slog.Warn("epic: list artifacts failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load the epic")
		return
	}
	writeJSON(w, http.StatusOK, EpicEnvelope{
		Steps:       steps,
		EpicIssueID: epicIssueID,
		NextKind:    epicNextKind(steps),
	})
}

// --- generate --------------------------------------------------------------

// GenerateProjectEpicStep: POST /api/projects/{id}/epic/steps/{kind}/generate.
func (h *Handler) GenerateProjectEpicStep(w http.ResponseWriter, r *http.Request) {
	kind, ok := epicKindParam(w, r)
	if !ok {
		return
	}
	project, ok := h.loadEpicProject(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		AgentID string `json:"agent_id"`
	}
	if r.Body != nil {
		// An empty body is the ordinary case: the workspace's Mika answers.
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	ctx := r.Context()
	steps, epicIssueID, err := h.loadEpicSteps(ctx, project)
	if err != nil {
		slog.Warn("epic: list artifacts failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load the epic")
		return
	}
	if allowed, missing := epicStepAllowed(kind, steps); !allowed {
		writeErrorCode(w, http.StatusConflict, ErrCodeEpicStepLocked,
			fmt.Sprintf("approve the %s step first", missing))
		return
	}
	if steps[kind].Generating {
		writeErrorCode(w, http.StatusConflict, ErrCodeEpicGenerating,
			"a run is already writing this step")
		return
	}

	agentID, ok := h.resolveEpicAgent(w, r, project, strings.TrimSpace(req.AgentID))
	if !ok {
		return
	}
	hostIssue, ok := h.resolveEpicHostIssue(w, r, project, epicIssueID)
	if !ok {
		return
	}

	actorType, actorID := h.resolveActor(r, userID, uuidToString(project.WorkspaceID))
	// Claim FIRST, like the PR walkthrough head fence: a second generate for
	// the same step is refused at the index rather than after it has already
	// spent a run.
	claim, err := h.Queries.ClaimEpicStepGeneration(ctx, db.ClaimEpicStepGenerationParams{
		WorkspaceID: project.WorkspaceID,
		ProjectID:   project.ID,
		Kind:        kind,
		EpicIssueID: hostIssue.ID,
		AuthorType:  actorType,
		AuthorID:    parseUUIDOrZero(actorID),
	})
	if err != nil {
		slog.Warn("epic: claim generation failed", append(logger.RequestAttrs(r), "error", err, "kind", kind)...)
		writeErrorCode(w, http.StatusConflict, ErrCodeEpicGenerating,
			"a run is already writing this step")
		return
	}

	task, err := h.TaskService.EnqueueCrossReviewRun(ctx, hostIssue, agentID, h.epicStepBrief(ctx, project, kind, steps), parseUUIDOrZero(requestUserID(r)))
	if err != nil {
		if derr := h.Queries.DeleteEpicArtifact(ctx, claim.ID); derr != nil {
			slog.Warn("epic: release claim failed", "artifact_id", uuidToString(claim.ID), "error", derr)
		}
		slog.Warn("epic: enqueue run failed", append(logger.RequestAttrs(r), "error", err, "kind", kind)...)
		if strings.Contains(err.Error(), "already exists") {
			writeErrorCode(w, http.StatusConflict, ErrCodeEpicGenerating,
				"this agent already has a run pending on the epic issue; wait for it to finish")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to start the epic run")
		return
	}
	if stamped, serr := h.TaskService.StampLeg(ctx, task, service.LegRoleEpicStep, db.AgentTaskQueue{}); serr != nil {
		slog.Warn("epic: stamp leg failed", "task_id", uuidToString(task.ID), "error", serr)
	} else {
		task = stamped
	}
	if err := h.Queries.SetEpicArtifactTask(ctx, db.SetEpicArtifactTaskParams{
		ID: claim.ID, GeneratedByTaskID: task.ID,
	}); err != nil {
		slog.Warn("epic: link task failed", "artifact_id", uuidToString(claim.ID), "error", err)
	}
	h.audit(ctx, project.WorkspaceID, actorType, actorID, AuditEpicStepGenerated, "project", project.ID,
		map[string]any{"kind": kind, "task_id": uuidToString(task.ID), "agent_id": uuidToString(agentID)}, nil)
	h.publishEpicEvent(EventEpicArtifactUpdated, project, kind, actorType, actorID)
	writeJSON(w, http.StatusAccepted, EpicGenerateResponse{TaskID: uuidToString(task.ID), Kind: kind})
}

// resolveEpicAgent picks the agent that writes the step: the explicit
// `agent_id` when the body carries one, otherwise the workspace's Mika. A
// workspace with neither is a 409, not a 500 — the caller is allowed, the
// workspace is not set up.
func (h *Handler) resolveEpicAgent(w http.ResponseWriter, r *http.Request, project db.Project, explicit string) (pgtype.UUID, bool) {
	if explicit != "" {
		agentID, ok := parseUUIDOrBadRequest(w, explicit, "agent_id")
		if !ok {
			return pgtype.UUID{}, false
		}
		agent, err := h.Queries.GetAgent(r.Context(), agentID)
		if err != nil || agent.WorkspaceID != project.WorkspaceID || agent.ArchivedAt.Valid {
			writeError(w, http.StatusBadRequest, "agent_id must be an active agent of this workspace")
			return pgtype.UUID{}, false
		}
		return agent.ID, true
	}
	mika, err := h.Queries.GetAgentBySystemKey(r.Context(), db.GetAgentBySystemKeyParams{
		WorkspaceID: project.WorkspaceID,
		SystemKey:   pgtype.Text{String: service.MikaSystemKey, Valid: true},
	})
	if err != nil {
		writeErrorCode(w, http.StatusConflict, ErrCodeEpicNoAgent,
			"this workspace has no default agent to write the epic; pass agent_id, or set up Mika first")
		return pgtype.UUID{}, false
	}
	return mika.ID, true
}

// resolveEpicHostIssue returns the issue the generation runs hang off, creating
// it on the first generate.
//
// The create is not locked. It does not need to be: IssueService.Create's
// duplicate guard already refuses a second active issue with the same
// (workspace, project, parent, title) tuple, and hands back the row that
// blocked it — which is exactly the host issue a racing request wanted.
func (h *Handler) resolveEpicHostIssue(w http.ResponseWriter, r *http.Request, project db.Project, known string) (db.Issue, bool) {
	ctx := r.Context()
	if known != "" {
		if id, err := parseUUIDChecked(known); err == nil {
			if issue, err := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
				ID: id, WorkspaceID: project.WorkspaceID,
			}); err == nil {
				return issue, true
			}
		}
	}
	platform, _, _ := middleware.ClientMetadataFromContext(ctx)
	res, err := h.IssueService.Create(ctx, service.IssueCreateParams{
		WorkspaceID: project.WorkspaceID,
		Title:       project.Title,
		Description: pgtype.Text{String: "Epic host issue: the runs that write this project's PRD, tech plan, wireframe and ticket breakdown are enqueued here.", Valid: true},
		Status:      "todo",
		Priority:    project.Priority,
		CreatorType: "member",
		CreatorID:   parseUUIDOrZero(requestUserID(r)),
		ProjectID:   project.ID,
		OriginType:  pgtype.Text{String: "epic", Valid: true},
		OriginID:    project.ID,
	}, service.IssueCreateOpts{Platform: platform})
	if errors.Is(err, service.ErrActiveDuplicate) && res.DuplicateIssue != nil {
		return *res.DuplicateIssue, true
	}
	if err != nil {
		slog.Warn("epic: create host issue failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create the epic issue")
		return db.Issue{}, false
	}
	return res.Issue, true
}

// epicStepBrief is the run's whole instruction set. The contract it states is
// the one references/epic-mode.md documents; if you change one, change the
// other.
func (h *Handler) epicStepBrief(ctx context.Context, project db.Project, kind string, steps epicSteps) string {
	var b strings.Builder
	b.WriteString("Write the ")
	b.WriteString(epicKindLabel(kind))
	b.WriteString(" for the project below. Read only — change nothing, run nothing, open nothing.\n\n")
	b.WriteString("PROJECT: ")
	b.WriteString(project.Title)
	b.WriteString("\n")
	if project.Description.Valid && strings.TrimSpace(project.Description.String) != "" {
		b.WriteString("DESCRIPTION:\n")
		b.WriteString(project.Description.String)
		b.WriteString("\n")
	}
	for _, prev := range epicStepOrder {
		if prev == kind {
			break
		}
		if approved := steps[prev].Approved; approved != nil {
			b.WriteString("\nAPPROVED ")
			b.WriteString(strings.ToUpper(prev))
			b.WriteString(":\n")
			b.WriteString(approved.Content)
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(epicKindInstruction(kind))
	b.WriteString("\n\nAnswer with exactly one fenced ```epic_step``` block containing:\n")
	if kind == epicKindTickets {
		b.WriteString("{\"kind\":\"tickets\",\"content\":\"…markdown summary…\",\"payload\":{\"tickets\":[{\"external_key\":\"T1\",\"title\":\"…\",\"description\":\"…\",\"depends_on\":[]}]}}\n")
		b.WriteString("`external_key` is yours to choose and must be unique in this list; `depends_on` names the external_key of every ticket that must land first. ")
		b.WriteString(fmt.Sprintf("At most %d tickets.\n", epicMaxTickets))
	} else {
		b.WriteString("{\"kind\":\"" + kind + "\",\"content\":\"…markdown…\",\"payload\":{}}\n")
	}
	b.WriteString("`content` is Markdown and must not be empty — an empty block is read as a failed run.\n")
	return b.String()
}

func epicKindLabel(kind string) string {
	switch kind {
	case epicKindPRD:
		return "product requirements document (PRD)"
	case epicKindTechPlan:
		return "technical plan"
	case epicKindWireframe:
		return "wireframe"
	case epicKindTickets:
		return "ticket breakdown"
	default:
		return kind
	}
}

func epicKindInstruction(kind string) string {
	switch kind {
	case epicKindPRD:
		return "State the problem, who has it, what success looks like, the scope, and what is explicitly out of scope."
	case epicKindTechPlan:
		return "State the approach, the components it touches, the data it changes, the risks, and the order of work. Stay inside the approved PRD's scope."
	case epicKindWireframe:
		return "Describe the screens as TEXT: an ASCII box layout or a Mermaid diagram in a fenced block inside `content`. No images."
	case epicKindTickets:
		return "Break the approved plan into independently shippable tickets. Each one names what changes and how it is verified."
	default:
		return ""
	}
}

// --- human edit ------------------------------------------------------------

// PutProjectEpicStep: PUT /api/projects/{id}/epic/steps/{kind}. A human edit is
// a new draft version — never an in-place rewrite, so an approved artifact
// stays readable as what was approved.
func (h *Handler) PutProjectEpicStep(w http.ResponseWriter, r *http.Request) {
	kind, ok := epicKindParam(w, r)
	if !ok {
		return
	}
	project, ok := h.loadEpicProject(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req struct {
		Content string          `json:"content"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, epicContentMaxBytes*2)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Content = strings.TrimSpace(req.Content)
	if req.Content == "" {
		writeError(w, http.StatusBadRequest, "content is required")
		return
	}
	if len(req.Content) > epicContentMaxBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("content exceeds %d bytes", epicContentMaxBytes))
		return
	}
	payload := []byte("{}")
	if len(req.Payload) > 0 && !json.Valid(req.Payload) {
		writeError(w, http.StatusBadRequest, "payload must be a JSON object")
		return
	} else if len(req.Payload) > 0 {
		payload = req.Payload
	}
	if kind == epicKindTickets {
		if _, err := parseEpicTickets(payload); err != nil {
			writeErrorCode(w, http.StatusBadRequest, ErrCodeEpicNoTickets, err.Error())
			return
		}
	}

	ctx := r.Context()
	steps, epicIssueID, err := h.loadEpicSteps(ctx, project)
	if err != nil {
		slog.Warn("epic: list artifacts failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load the epic")
		return
	}
	if allowed, missing := epicStepAllowed(kind, steps); !allowed {
		writeErrorCode(w, http.StatusConflict, ErrCodeEpicStepLocked,
			fmt.Sprintf("approve the %s step first", missing))
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(project.WorkspaceID))
	created, reopened, err := h.publishEpicArtifact(ctx, project, kind, req.Content, payload, epicIssueID, pgtype.UUID{}, actorType, parseUUIDOrZero(actorID))
	if err != nil {
		slog.Warn("epic: create artifact failed", append(logger.RequestAttrs(r), "error", err, "kind", kind)...)
		writeError(w, http.StatusInternalServerError, "failed to save the step")
		return
	}
	h.audit(ctx, project.WorkspaceID, actorType, actorID, AuditEpicStepEdited, "project", project.ID,
		map[string]any{"kind": kind, "version": created.Version, "reopened": reopened}, nil)
	h.publishEpicEvent(EventEpicArtifactUpdated, project, kind, actorType, actorID)
	writeJSON(w, http.StatusOK, EpicStepWriteResponse{
		Artifact:       epicArtifactToResponse(created),
		ReopenedSteps:  reopened,
		EpicIssueID:    epicIssueID,
		SupersededPrev: true,
	})
}

// publishEpicArtifact writes a new draft version of one step and settles what
// that publication invalidates: every earlier row of the same kind, and every
// LATER step — those were approved against a premise that just changed.
// Returns the steps it reopened so the caller can say so.
func (h *Handler) publishEpicArtifact(
	ctx context.Context, project db.Project, kind, content string, payload []byte,
	epicIssueID string, taskID pgtype.UUID, actorType string, actorID pgtype.UUID,
) (db.EpicArtifact, []string, error) {
	created, err := h.Queries.CreateEpicArtifact(ctx, db.CreateEpicArtifactParams{
		WorkspaceID:       project.WorkspaceID,
		ProjectID:         project.ID,
		Kind:              kind,
		Content:           content,
		Payload:           payload,
		EpicIssueID:       parseUUIDOrZero(epicIssueID),
		GeneratedByTaskID: taskID,
		AuthorType:        actorType,
		AuthorID:          actorID,
	})
	if err != nil {
		return db.EpicArtifact{}, nil, err
	}
	if err := h.Queries.SupersedeOtherEpicArtifacts(ctx, db.SupersedeOtherEpicArtifactsParams{
		ProjectID: project.ID, Kind: kind, ID: created.ID,
	}); err != nil {
		slog.Warn("epic: supersede same-kind versions failed", "project_id", uuidToString(project.ID), "kind", kind, "error", err)
	}
	later := epicLaterKinds(kind)
	if len(later) > 0 {
		if err := h.Queries.SupersedeEpicArtifactKinds(ctx, db.SupersedeEpicArtifactKindsParams{
			ProjectID: project.ID, Kinds: later,
		}); err != nil {
			slog.Warn("epic: supersede later steps failed", "project_id", uuidToString(project.ID), "kind", kind, "error", err)
		}
	}
	return created, later, nil
}

// --- approve ---------------------------------------------------------------

// ApproveProjectEpicStep: POST /api/projects/{id}/epic/steps/{kind}/approve.
func (h *Handler) ApproveProjectEpicStep(w http.ResponseWriter, r *http.Request) {
	kind, ok := epicKindParam(w, r)
	if !ok {
		return
	}
	project, ok := h.loadEpicProject(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	steps, _, err := h.loadEpicSteps(ctx, project)
	if err != nil {
		slog.Warn("epic: list artifacts failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load the epic")
		return
	}
	if allowed, missing := epicStepAllowed(kind, steps); !allowed {
		writeErrorCode(w, http.StatusConflict, ErrCodeEpicStepLocked,
			fmt.Sprintf("approve the %s step first", missing))
		return
	}
	latest := steps[kind].Latest
	if latest == nil || latest.State != epicStateDraft {
		writeErrorCode(w, http.StatusConflict, ErrCodeEpicNoDraft,
			"this step has no draft to approve")
		return
	}
	artifactID, err := parseUUIDChecked(latest.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve the step")
		return
	}
	approved, err := h.Queries.ApproveEpicArtifact(ctx, db.ApproveEpicArtifactParams{
		ID: artifactID, ApprovedBy: parseUUIDOrZero(userID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeErrorCode(w, http.StatusConflict, ErrCodeEpicNoDraft,
			"this step has no draft to approve")
		return
	}
	if err != nil {
		slog.Warn("epic: approve failed", append(logger.RequestAttrs(r), "error", err, "kind", kind)...)
		writeError(w, http.StatusInternalServerError, "failed to approve the step")
		return
	}
	actorType, actorID := h.resolveActor(r, userID, uuidToString(project.WorkspaceID))
	h.audit(ctx, project.WorkspaceID, actorType, actorID, AuditEpicStepApproved, "project", project.ID,
		map[string]any{"kind": kind, "version": approved.Version}, nil)
	h.publishEpicEvent(EventEpicArtifactApproved, project, kind, actorType, actorID)
	writeJSON(w, http.StatusOK, epicArtifactToResponse(approved))
}

// --- apply -----------------------------------------------------------------

// parseEpicTickets reads the ticket list out of a tickets payload and states
// every reason it is unusable. It is the boundary validation for LLM output,
// so it never trusts a field's presence or shape.
func parseEpicTickets(payload []byte) ([]EpicTicket, error) {
	var wrapper struct {
		Tickets []EpicTicket `json:"tickets"`
	}
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &wrapper); err != nil {
			return nil, errors.New("payload.tickets must be an array of tickets")
		}
	}
	if len(wrapper.Tickets) == 0 {
		return nil, errors.New("payload.tickets is empty; the tickets step must list at least one ticket")
	}
	seen := map[string]bool{}
	out := make([]EpicTicket, 0, len(wrapper.Tickets))
	for i := range wrapper.Tickets {
		tk := wrapper.Tickets[i]
		tk.ExternalKey = strings.TrimSpace(tk.ExternalKey)
		tk.Title = strings.TrimSpace(tk.Title)
		if tk.ExternalKey == "" {
			return nil, fmt.Errorf("tickets[%d].external_key is required", i)
		}
		if tk.Title == "" {
			return nil, fmt.Errorf("tickets[%d].title is required", i)
		}
		if seen[tk.ExternalKey] {
			return nil, fmt.Errorf("duplicate ticket external_key %q", tk.ExternalKey)
		}
		seen[tk.ExternalKey] = true
		out = append(out, tk)
	}
	for _, tk := range out {
		for _, dep := range tk.DependsOn {
			dep = strings.TrimSpace(dep)
			if dep == "" {
				continue
			}
			if !seen[dep] {
				return nil, fmt.Errorf("ticket %q depends on %q, which is not in this list", tk.ExternalKey, dep)
			}
			if dep == tk.ExternalKey {
				return nil, fmt.Errorf("ticket %q depends on itself", tk.ExternalKey)
			}
		}
	}
	return out, nil
}

// epicAppliedMap reads payload.applied — external_key -> issue id — which is
// what makes apply idempotent across replays.
func epicAppliedMap(payload []byte) map[string]string {
	var wrapper struct {
		Applied map[string]string `json:"applied"`
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &wrapper)
	}
	if wrapper.Applied == nil {
		return map[string]string{}
	}
	return wrapper.Applied
}

// ApplyProjectEpicTickets: POST /api/projects/{id}/epic/steps/{kind}/apply.
//
// The route takes {kind} rather than a literal `tickets` segment so it cannot
// collide with `{kind}/generate` in the router's trie; only `tickets` is
// accepted, and any other step is a 400.
//
// ponytail: compensation, not one SQL transaction — IssueService.Create
// commits and enqueues per issue, so a shared tx would leak half-made runs.
// Same trade the plan gate (K11) makes, and the observable behaviour is the
// same: a failure part-way deletes what it created.
func (h *Handler) ApplyProjectEpicTickets(w http.ResponseWriter, r *http.Request) {
	kind, ok := epicKindParam(w, r)
	if !ok {
		return
	}
	if kind != epicKindTickets {
		writeError(w, http.StatusBadRequest, "only the tickets step can be applied")
		return
	}
	project, ok := h.loadEpicProject(w, r)
	if !ok {
		return
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	steps, epicIssueID, err := h.loadEpicSteps(ctx, project)
	if err != nil {
		slog.Warn("epic: list artifacts failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to load the epic")
		return
	}
	if allowed, missing := epicStepAllowed(kind, steps); !allowed {
		writeErrorCode(w, http.StatusConflict, ErrCodeEpicStepLocked,
			fmt.Sprintf("approve the %s step first", missing))
		return
	}
	approved := steps[kind].Approved
	if approved == nil {
		writeErrorCode(w, http.StatusConflict, ErrCodeEpicNotApproved,
			"approve the tickets step before applying it")
		return
	}
	tickets, err := parseEpicTickets(approved.Payload)
	if err != nil {
		writeErrorCode(w, http.StatusUnprocessableEntity, ErrCodeEpicNoTickets, err.Error())
		return
	}
	if len(tickets) > epicMaxTickets {
		writeErrorCode(w, http.StatusUnprocessableEntity, ErrCodeTooManyTickets,
			fmt.Sprintf("this breakdown has %d tickets; at most %d can be applied", len(tickets), epicMaxTickets))
		return
	}
	hostIssue, ok := h.resolveEpicHostIssue(w, r, project, epicIssueID)
	if !ok {
		return
	}
	artifactID, err := parseUUIDChecked(approved.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to apply the tickets")
		return
	}

	actorType, actorID := h.resolveActor(r, userID, uuidToString(project.WorkspaceID))
	out, err := h.applyEpicTickets(ctx, r, project, hostIssue, artifactID, approved.Payload, tickets, actorType, actorID)
	if err != nil {
		var ae *epicApplyError
		if errors.As(err, &ae) {
			writeErrorCode(w, ae.status, ae.code, ae.msg)
			return
		}
		slog.Warn("epic: apply tickets failed", append(logger.RequestAttrs(r), "error", err)...)
		writeErrorCode(w, http.StatusInternalServerError, ErrCodeEpicApplyFailed, "failed to apply the tickets")
		return
	}
	h.audit(ctx, project.WorkspaceID, actorType, actorID, AuditEpicTicketsApply, "project", project.ID,
		map[string]any{"created": len(out.Created), "existing": len(out.Existing), "dependencies": out.Dependencies}, nil)
	h.publishEpicEvent(EventEpicArtifactUpdated, project, kind, actorType, actorID)
	writeJSON(w, http.StatusOK, out)
}

type epicApplyError struct {
	status int
	code   string
	msg    string
}

func (e *epicApplyError) Error() string { return e.msg }

func (h *Handler) applyEpicTickets(
	ctx context.Context, r *http.Request, project db.Project, hostIssue db.Issue,
	artifactID pgtype.UUID, payload []byte, tickets []EpicTicket, actorType, actorID string,
) (EpicApplyResponse, error) {
	applied := epicAppliedMap(payload)
	prefix := h.getIssuePrefix(ctx, project.WorkspaceID)
	fill := h.newStatusCategoryFiller(ctx, project.WorkspaceID)
	platform, _, _ := middleware.ClientMetadataFromContext(ctx)

	byKey := map[string]pgtype.UUID{}
	out := EpicApplyResponse{Created: []IssueResponse{}, Existing: []string{}, EpicIssueID: uuidToString(hostIssue.ID)}
	var fresh []db.Issue
	// Compensation: everything this call created is removed when a later step
	// fails, so a refused dependency leaves the project exactly as it was.
	rollback := func() {
		for _, c := range fresh {
			if err := h.Queries.DeleteIssue(ctx, db.DeleteIssueParams{ID: c.ID, WorkspaceID: c.WorkspaceID}); err != nil {
				slog.Warn("epic: rollback delete failed", "issue_id", uuidToString(c.ID), "error", err)
			}
		}
	}

	for _, tk := range tickets {
		if existing, ok := applied[tk.ExternalKey]; ok && existing != "" {
			id, err := parseUUIDChecked(existing)
			if err == nil {
				if _, gerr := h.Queries.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{
					ID: id, WorkspaceID: project.WorkspaceID,
				}); gerr == nil {
					byKey[tk.ExternalKey] = id
					out.Existing = append(out.Existing, tk.ExternalKey)
					continue
				}
			}
			// The recorded issue is gone (deleted by a human). Re-create it
			// rather than leaving a dangling key in the manifest.
		}
		res, err := h.IssueService.Create(ctx, service.IssueCreateParams{
			WorkspaceID:    project.WorkspaceID,
			Title:          tk.Title,
			Description:    pgtype.Text{String: tk.Description, Valid: tk.Description != ""},
			Status:         "todo",
			Priority:       project.Priority,
			CreatorType:    "member",
			CreatorID:      parseUUIDOrZero(requestUserID(r)),
			ParentIssueID:  hostIssue.ID,
			ProjectID:      project.ID,
			OriginType:     pgtype.Text{String: "epic", Valid: true},
			OriginID:       project.ID,
			AllowDuplicate: true,
		}, service.IssueCreateOpts{
			ActorID:  actorID,
			Platform: platform,
			BroadcastPayload: func(child db.Issue, _ []db.Attachment, _ []db.IssueLabel) map[string]any {
				resp := issueToResponse(child, prefix)
				fill(&resp)
				return map[string]any{"issue": resp}
			},
		})
		if err != nil {
			rollback()
			return EpicApplyResponse{}, fmt.Errorf("create ticket %q: %w", tk.ExternalKey, err)
		}
		fresh = append(fresh, res.Issue)
		byKey[tk.ExternalKey] = res.Issue.ID
		applied[tk.ExternalKey] = uuidToString(res.Issue.ID)
		resp := issueToResponse(res.Issue, prefix)
		fill(&resp)
		out.Created = append(out.Created, resp)
	}

	// Dependencies: `depends_on` names what must land first, so the named
	// ticket BLOCKS this one. The anti-cycle check is the shared one the
	// dependency endpoint uses, so apply cannot create what it refuses.
	var touched []pgtype.UUID
	for _, tk := range tickets {
		to := byKey[tk.ExternalKey]
		for _, dep := range tk.DependsOn {
			dep = strings.TrimSpace(dep)
			from, ok := byKey[dep]
			if dep == "" || !ok {
				continue
			}
			if _, err := h.Queries.GetIssueDependency(ctx, db.GetIssueDependencyParams{
				IssueID: from, DependsOnIssueID: to, Type: dependencyBlocks,
			}); err == nil {
				continue // already linked by an earlier apply
			}
			cycles, err := h.blockingEdgeWouldCycle(ctx, from, to)
			if err != nil {
				rollback()
				return EpicApplyResponse{}, fmt.Errorf("check dependency %q -> %q: %w", dep, tk.ExternalKey, err)
			}
			if cycles {
				rollback()
				return EpicApplyResponse{}, &epicApplyError{
					status: http.StatusConflict,
					code:   "epic_ticket_cycle",
					msg:    fmt.Sprintf("ticket %q depends on %q, which would create a cycle", tk.ExternalKey, dep),
				}
			}
			if _, err := h.Queries.CreateIssueDependency(ctx, db.CreateIssueDependencyParams{
				IssueID: from, DependsOnIssueID: to, Type: dependencyBlocks,
			}); err != nil {
				rollback()
				return EpicApplyResponse{}, fmt.Errorf("link ticket %q after %q: %w", tk.ExternalKey, dep, err)
			}
			out.Dependencies++
			touched = append(touched, from, to)
		}
	}

	// The manifest is written last: a failure before this point rolled the
	// issues back, so a replay must not find them recorded as applied.
	var wrapper map[string]any
	if err := json.Unmarshal(payload, &wrapper); err != nil || wrapper == nil {
		wrapper = map[string]any{}
	}
	wrapper["applied"] = applied
	if raw, err := json.Marshal(wrapper); err == nil {
		if _, err := h.Queries.SetEpicArtifactPayload(ctx, db.SetEpicArtifactPayloadParams{
			ID: artifactID, Payload: raw,
		}); err != nil {
			slog.Warn("epic: record applied tickets failed", "artifact_id", uuidToString(artifactID), "error", err)
		}
	}
	if len(touched) > 0 {
		h.publishIssueDependencyChange(r, actorType, actorID, project.WorkspaceID, touched...)
	}
	return out, nil
}

// --- completion hooks ------------------------------------------------------

// settleEpicStepRun is the CompleteTask hook. The leg role is the cheap guard:
// every other completed run in the system pays one string compare.
func (h *Handler) settleEpicStepRun(ctx context.Context, task db.AgentTaskQueue, output string) {
	if task.LegRole != service.LegRoleEpicStep {
		return
	}
	claim, err := h.Queries.GetEpicArtifactByTask(ctx, task.ID)
	if err != nil {
		return
	}
	project, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
		ID: claim.ProjectID, WorkspaceID: claim.WorkspaceID,
	})
	if err != nil {
		h.failEpicClaim(ctx, claim, "the project this run was writing for is gone")
		return
	}
	report, ok := h.parseEpicStepBlock(ctx, task, output)
	if !ok {
		h.failEpicClaim(ctx, claim, "the run ended without a readable epic_step block")
		return
	}
	// The block names its own kind. A block for a different step than the one
	// claimed is not settled into the claim — that would file a tech plan as a
	// PRD — so it is a failed generation.
	if report.Kind != "" && report.Kind != claim.Kind {
		h.failEpicClaim(ctx, claim, fmt.Sprintf("the run answered with a %q block for the %q step", report.Kind, claim.Kind))
		return
	}
	content := strings.TrimSpace(report.Content)
	if content == "" {
		h.failEpicClaim(ctx, claim, "the epic_step block carried no content")
		return
	}
	if len(content) > epicContentMaxBytes {
		content = util.TruncateUTF8Bytes(content, epicContentMaxBytes)
	}
	payload := []byte("{}")
	if len(report.Payload) > 0 && json.Valid(report.Payload) {
		payload = report.Payload
	}
	if claim.Kind == epicKindTickets {
		if _, err := parseEpicTickets(payload); err != nil {
			h.failEpicClaim(ctx, claim, "the tickets block is unusable: "+err.Error())
			return
		}
	}

	settled, err := h.Queries.SettleEpicArtifact(ctx, db.SettleEpicArtifactParams{
		ID: claim.ID, Content: content, Payload: payload,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return // already settled: idempotent replay
	}
	if err != nil {
		slog.Warn("epic: settle failed", "artifact_id", uuidToString(claim.ID), "error", err)
		return
	}
	if err := h.Queries.SupersedeOtherEpicArtifacts(ctx, db.SupersedeOtherEpicArtifactsParams{
		ProjectID: settled.ProjectID, Kind: settled.Kind, ID: settled.ID,
	}); err != nil {
		slog.Warn("epic: supersede same-kind versions failed", "project_id", uuidToString(settled.ProjectID), "error", err)
	}
	if later := epicLaterKinds(settled.Kind); len(later) > 0 {
		if err := h.Queries.SupersedeEpicArtifactKinds(ctx, db.SupersedeEpicArtifactKindsParams{
			ProjectID: settled.ProjectID, Kinds: later,
		}); err != nil {
			slog.Warn("epic: supersede later steps failed", "project_id", uuidToString(settled.ProjectID), "error", err)
		}
	}
	h.publishEpicEvent(EventEpicArtifactUpdated, project, settled.Kind, "agent", uuidToString(task.AgentID))
}

// failEpicStepRun is the FailTask hook: a crashed run must release its claim,
// or the step would show `generating` forever.
func (h *Handler) failEpicStepRun(ctx context.Context, task db.AgentTaskQueue, reason string) {
	if task.LegRole != service.LegRoleEpicStep {
		return
	}
	claim, err := h.Queries.GetEpicArtifactByTask(ctx, task.ID)
	if err != nil {
		return
	}
	h.failEpicClaim(ctx, claim, reason)
}

// failEpicClaim drops an unsettled claim, records why, and tells the client so
// the rail can offer a retry instead of an eternal skeleton. A settled row is
// never touched: DeleteEpicArtifact matches only empty content.
func (h *Handler) failEpicClaim(ctx context.Context, claim db.EpicArtifact, reason string) {
	if err := h.Queries.DeleteEpicArtifact(ctx, claim.ID); err != nil {
		slog.Warn("epic: release claim failed", "artifact_id", uuidToString(claim.ID), "error", err)
		return
	}
	if len(reason) > 1000 {
		reason = util.TruncateUTF8Bytes(reason, 1000)
	}
	h.audit(ctx, claim.WorkspaceID, "system", "", AuditEpicStepFailed, "project", claim.ProjectID,
		map[string]any{"kind": claim.Kind, "reason": reason, "task_id": uuidToString(claim.GeneratedByTaskID)}, nil)
	h.publish(EventEpicArtifactFailed, uuidToString(claim.WorkspaceID), "system", "", map[string]any{
		"project_id": uuidToString(claim.ProjectID),
		"kind":       claim.Kind,
		"reason":     reason,
	})
}

type epicStepReport struct {
	Kind    string          `json:"kind"`
	Content string          `json:"content"`
	Payload json.RawMessage `json:"payload"`
}

// parseEpicStepBlock reads the fenced block off the run's answer, falling back
// to its streamed text messages when the final answer does not carry it — the
// same fallback the walkthrough (F05) and the drift report (K56) use, for the
// same reason: some runtimes end on a tool result rather than the prose.
func (h *Handler) parseEpicStepBlock(ctx context.Context, task db.AgentTaskQueue, output string) (epicStepReport, bool) {
	text := output
	if !epicStepFence.MatchString(text) {
		if msgs, err := h.Queries.ListTaskMessages(ctx, task.ID); err == nil {
			var b strings.Builder
			for _, m := range msgs {
				if m.Type == "text" && m.Content.Valid {
					b.WriteString(m.Content.String + "\n\n")
				}
			}
			if epicStepFence.MatchString(b.String()) {
				text = b.String()
			}
		}
	}
	m := epicStepFence.FindStringSubmatch(text)
	if m == nil {
		return epicStepReport{}, false
	}
	var report epicStepReport
	if err := json.Unmarshal([]byte(m[1]), &report); err != nil {
		return epicStepReport{}, false
	}
	return report, true
}

// --- realtime --------------------------------------------------------------

func (h *Handler) publishEpicEvent(event string, project db.Project, kind, actorType, actorID string) {
	h.publish(event, uuidToString(project.WorkspaceID), actorType, actorID, map[string]any{
		"project_id": uuidToString(project.ID),
		"kind":       kind,
	})
}
