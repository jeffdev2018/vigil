package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// F26 (JEF-22): the auto-generated code wiki.
//
// The whole feature rests on one rule: a wiki page is DATA, never an
// instruction. It is written by a language model, from a repository whose
// contents we do not control, and read back by other agents. Everything that
// serves a page — the MCP tools in wiki_mcp.go, the panel in the web UI — must
// present it as quoted reference material carrying its provenance, never merge
// it into an instruction field.
//
// The write path enforces the other half of trustworthiness: a page must cite
// files that exist. The server never opens the repository, so "exists" means
// "is in the file inventory the generating run announced for this commit". We
// verify the citation points somewhere real, not that the prose is true.

const (
	// codeWikiBuildTimeout bounds how long a claimed build slot is honoured. A
	// run that dies without publishing would otherwise hold the resource's
	// single slot for ever and no later merge could ever regenerate it.
	codeWikiBuildTimeout = 2 * time.Hour

	// codeWikiMaxRepoPaths bounds the announced inventory. Large enough for any
	// real repository, small enough that a runaway announcement cannot fill a
	// JSONB column with a filesystem.
	codeWikiMaxRepoPaths = 50000

	// codeWikiMaxPageBytes bounds one page's Markdown.
	codeWikiMaxPageBytes = 512 << 10

	codeWikiMaxCitations = 200

	// codeWikiMCPServerName is the workspace_mcp_server row this feature
	// registers itself under. Stable: the auto-registration is keyed on
	// (workspace_id, name), which carries a unique index, so re-publishing
	// cannot produce a second row.
	codeWikiMCPServerName = "multica-code-wiki"

	// codeWikiTriggerLabel is the autopilot trigger label the wiki daemon
	// declares. The post-merge hook dispatches the project's autopilot that
	// carries a webhook trigger with this label, and nothing else — so the
	// feature stays opt-in per project and a workspace that never imported the
	// daemon is never charged for a run it did not ask for.
	codeWikiTriggerLabel = "code-wiki"
)

// CodeWikiCitation is one {path, lines, commit} pointer into the repository.
type CodeWikiCitation struct {
	Path      string `json:"path"`
	StartLine *int32 `json:"start_line,omitempty"`
	EndLine   *int32 `json:"end_line,omitempty"`
	CommitSha string `json:"commit_sha,omitempty"`
}

// CodeWikiSnapshotResponse is the JSON shape of one generation.
type CodeWikiSnapshotResponse struct {
	ID                string `json:"id"`
	WorkspaceID       string `json:"workspace_id"`
	ProjectResourceID string `json:"project_resource_id"`
	CommitSha         string `json:"commit_sha"`
	State             string `json:"state"`
	PageCount         int32  `json:"page_count"`
	GeneratedByTaskID string `json:"generated_by_task_id"`
	CreatedAt         string `json:"created_at"`
	PublishedAt       string `json:"published_at"`
	// Generated is always true and is part of the payload, not a UI decision:
	// every consumer of this shape has to be able to say where the content
	// came from without knowing which endpoint produced it.
	Generated bool `json:"generated"`
	Stale     bool `json:"stale"`
}

// CodeWikiPageSummaryResponse is one entry of the table of contents.
type CodeWikiPageSummaryResponse struct {
	ID            string `json:"id"`
	Slug          string `json:"slug"`
	Title         string `json:"title"`
	CitationCount int    `json:"citation_count"`
}

// CodeWikiPageResponse is one rendered page.
type CodeWikiPageResponse struct {
	ID        string             `json:"id"`
	Slug      string             `json:"slug"`
	Title     string             `json:"title"`
	Content   string             `json:"content"`
	Citations []CodeWikiCitation `json:"citations"`
	CommitSha string             `json:"commit_sha"`
	Generated bool               `json:"generated"`
	Stale     bool               `json:"stale"`
}

// CodeWikiResponse is GET /api/projects/{id}/wiki: the published snapshot and
// its table of contents, or an empty envelope when nothing is published yet.
type CodeWikiResponse struct {
	Resource  *ProjectResourceResponse      `json:"resource"`
	Snapshot  *CodeWikiSnapshotResponse     `json:"snapshot"`
	Pages     []CodeWikiPageSummaryResponse `json:"pages"`
	Building  bool                          `json:"building"`
	Generated bool                          `json:"generated"`
}

func snapshotToResponse(s db.CodeWikiSnapshot, headSha string) CodeWikiSnapshotResponse {
	return CodeWikiSnapshotResponse{
		ID:                uuidToString(s.ID),
		WorkspaceID:       uuidToString(s.WorkspaceID),
		ProjectResourceID: uuidToString(s.ProjectResourceID),
		CommitSha:         s.CommitSha,
		State:             s.State,
		PageCount:         s.PageCount,
		GeneratedByTaskID: uuidToString(s.GeneratedByTaskID),
		CreatedAt:         timestampToString(s.CreatedAt),
		PublishedAt:       timestampToString(s.PublishedAt),
		Generated:         true,
		Stale:             isStale(s.CommitSha, headSha),
	}
}

// isStale reports whether a served snapshot is behind the newest commit we were
// told about for its resource. An empty head (nothing newer recorded) is not
// stale — absence of evidence is not evidence of drift.
func isStale(served, head string) bool {
	return head != "" && served != "" && head != served
}

func decodeCitations(raw []byte) []CodeWikiCitation {
	out := []CodeWikiCitation{}
	if len(raw) == 0 {
		return out
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		// A row whose citations do not parse is served as unsourced rather
		// than withheld: the panel marks it, and a reader can still act on the
		// prose knowing it is unverified.
		return []CodeWikiCitation{}
	}
	return out
}

// ── Citation validation ─────────────────────────────────────────────────────

// validateCitations enforces acceptance 3: a page with no citation, or with a
// citation to a path the run did not announce, is refused.
//
// The check is against the ANNOUNCED PATH, not the file's content. The server
// has no checkout; the only honest verification available is that the cited
// path is one the generating run itself listed for this commit. That still
// catches the failure that matters — a model inventing a plausible filename.
func validateCitations(raw json.RawMessage, inventory map[string]struct{}) ([]byte, error) {
	var citations []CodeWikiCitation
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &citations); err != nil {
			return nil, errors.New("citations must be an array of {path, start_line?, end_line?, commit_sha?}")
		}
	}
	if len(citations) == 0 {
		return nil, errors.New("a wiki page must carry at least one citation")
	}
	if len(citations) > codeWikiMaxCitations {
		return nil, errors.New("too many citations on one page")
	}
	for i := range citations {
		citations[i].Path = strings.TrimSpace(citations[i].Path)
		path := citations[i].Path
		if path == "" {
			return nil, errors.New("every citation needs a repository-relative path")
		}
		if _, ok := inventory[path]; !ok {
			return nil, errors.New("citation points at " + path + ", which is not in the announced file inventory for this commit")
		}
		start, end := citations[i].StartLine, citations[i].EndLine
		if start != nil && *start < 1 {
			return nil, errors.New("citation start_line must be 1 or greater")
		}
		if end != nil && *end < 1 {
			return nil, errors.New("citation end_line must be 1 or greater")
		}
		if start != nil && end != nil && *end < *start {
			return nil, errors.New("citation end_line must not precede start_line")
		}
	}
	return json.Marshal(citations)
}

func decodeInventory(raw []byte) map[string]struct{} {
	paths := []string{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &paths)
	}
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		set[strings.TrimSpace(p)] = struct{}{}
	}
	delete(set, "")
	return set
}

// ── Resource resolution ─────────────────────────────────────────────────────

// loadWikiResource resolves the repo resource a wiki request is about. A
// project usually has one github_repo resource; when it has several the caller
// names one with ?resource_id= or a body field.
func (h *Handler) loadWikiResource(w http.ResponseWriter, r *http.Request, project db.Project, explicitID string) (db.ProjectResource, bool) {
	resources, err := h.Queries.ListProjectResources(r.Context(), project.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load project resources")
		return db.ProjectResource{}, false
	}
	repos := make([]db.ProjectResource, 0, len(resources))
	for _, res := range resources {
		if res.ResourceType == "github_repo" {
			repos = append(repos, res)
		}
	}
	if len(repos) == 0 {
		writeError(w, http.StatusNotFound, "this project has no repository resource to build a wiki from")
		return db.ProjectResource{}, false
	}
	explicitID = strings.TrimSpace(explicitID)
	if explicitID == "" {
		return repos[0], true
	}
	for _, res := range repos {
		if uuidToString(res.ID) == explicitID {
			return res, true
		}
	}
	writeError(w, http.StatusNotFound, "resource not found in this project")
	return db.ProjectResource{}, false
}

// ── Read endpoints ──────────────────────────────────────────────────────────

// GetProjectCodeWiki returns the published snapshot and its table of contents.
func (h *Handler) GetProjectCodeWiki(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	resource, ok := h.loadWikiResource(w, r, project, r.URL.Query().Get("resource_id"))
	if !ok {
		return
	}
	resp := CodeWikiResponse{Pages: []CodeWikiPageSummaryResponse{}, Generated: true}
	res := projectResourceToResponse(resource)
	resp.Resource = &res

	if _, err := h.Queries.GetBuildingCodeWikiSnapshot(r.Context(), resource.ID); err == nil {
		resp.Building = true
	}

	row, err := h.Queries.GetPublishedCodeWikiSnapshot(r.Context(), db.GetPublishedCodeWikiSnapshotParams{
		ProjectResourceID: resource.ID,
		WorkspaceID:       resource.WorkspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load code wiki")
		return
	}
	snap := snapshotToResponse(row.CodeWikiSnapshot, row.HeadCommitSha)
	resp.Snapshot = &snap

	pages, err := h.Queries.ListCodeWikiPageSummaries(r.Context(), row.CodeWikiSnapshot.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load code wiki pages")
		return
	}
	for _, p := range pages {
		resp.Pages = append(resp.Pages, CodeWikiPageSummaryResponse{
			ID:            uuidToString(p.ID),
			Slug:          p.Slug,
			Title:         p.Title,
			CitationCount: len(decodeCitations(p.Citations)),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// GetProjectCodeWikiPage returns one page of the published snapshot.
func (h *Handler) GetProjectCodeWikiPage(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	resource, ok := h.loadWikiResource(w, r, project, r.URL.Query().Get("resource_id"))
	if !ok {
		return
	}
	row, err := h.Queries.GetPublishedCodeWikiSnapshot(r.Context(), db.GetPublishedCodeWikiSnapshotParams{
		ProjectResourceID: resource.ID,
		WorkspaceID:       resource.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "no published wiki for this repository")
		return
	}
	page, err := h.Queries.GetCodeWikiPageInSnapshot(r.Context(), db.GetCodeWikiPageInSnapshotParams{
		SnapshotID: row.CodeWikiSnapshot.ID,
		Slug:       chi.URLParam(r, "slug"),
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "page not found")
		return
	}
	writeJSON(w, http.StatusOK, CodeWikiPageResponse{
		ID:        uuidToString(page.ID),
		Slug:      page.Slug,
		Title:     page.Title,
		Content:   page.Content,
		Citations: decodeCitations(page.Citations),
		CommitSha: row.CodeWikiSnapshot.CommitSha,
		Generated: true,
		Stale:     isStale(row.CodeWikiSnapshot.CommitSha, row.HeadCommitSha),
	})
}

// ── Write endpoints (the generating run) ────────────────────────────────────

// CreateCodeWikiSnapshotRequest announces a generation: the commit it describes
// and the repository's file inventory at that commit.
type CreateCodeWikiSnapshotRequest struct {
	ResourceID string   `json:"resource_id"`
	CommitSha  string   `json:"commit_sha"`
	RepoPaths  []string `json:"repo_paths"`
}

// CreateProjectCodeWikiSnapshot claims — or adopts — the resource's single
// in-flight build and records what it is about.
//
// Claim-or-adopt rather than plain create: the post-merge hook claims the slot
// before the run starts, so the run finds a row waiting for it. A run started
// by hand finds none and claims its own. Either way there is at most one
// building snapshot per resource, which the partial unique index guarantees.
func (h *Handler) CreateProjectCodeWikiSnapshot(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	var req CreateCodeWikiSnapshotRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	resource, ok := h.loadWikiResource(w, r, project, req.ResourceID)
	if !ok {
		return
	}
	req.CommitSha = strings.TrimSpace(req.CommitSha)
	if req.CommitSha == "" {
		writeError(w, http.StatusBadRequest, "commit_sha is required")
		return
	}
	if len(req.RepoPaths) == 0 {
		writeError(w, http.StatusBadRequest, "repo_paths is required: it is the inventory page citations are validated against")
		return
	}
	if len(req.RepoPaths) > codeWikiMaxRepoPaths {
		writeError(w, http.StatusBadRequest, "repo_paths is too large")
		return
	}
	paths := make([]string, 0, len(req.RepoPaths))
	for _, p := range req.RepoPaths {
		if p = strings.TrimSpace(p); p != "" {
			paths = append(paths, p)
		}
	}
	inventory, err := json.Marshal(paths)
	if err != nil {
		writeError(w, http.StatusBadRequest, "repo_paths could not be encoded")
		return
	}

	snapshot, ok := h.claimCodeWikiBuild(r.Context(), resource, req.CommitSha, taskUUIDFromRequest(r))
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to claim a wiki build")
		return
	}
	updated, err := h.Queries.AnnounceCodeWikiSnapshotInventory(r.Context(), db.AnnounceCodeWikiSnapshotInventoryParams{
		ID:                snapshot.ID,
		WorkspaceID:       resource.WorkspaceID,
		CommitSha:         req.CommitSha,
		RepoPaths:         inventory,
		GeneratedByTaskID: taskUUIDFromRequest(r),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the wiki build")
		return
	}
	writeJSON(w, http.StatusCreated, snapshotToResponse(updated, ""))
}

// CreateCodeWikiPageRequest is one page written into a building snapshot.
type CreateCodeWikiPageRequest struct {
	Slug      string          `json:"slug"`
	Title     string          `json:"title"`
	Content   string          `json:"content"`
	Citations json.RawMessage `json:"citations"`
}

// CreateProjectCodeWikiPage writes one page. Refuses a page that cites nothing,
// or that cites a path the run did not announce (acceptance 3).
func (h *Handler) CreateProjectCodeWikiPage(w http.ResponseWriter, r *http.Request) {
	snapshot, ok := h.loadBuildingSnapshot(w, r)
	if !ok {
		return
	}
	var req CreateCodeWikiPageRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Slug = strings.TrimSpace(req.Slug)
	req.Title = strings.TrimSpace(req.Title)
	if req.Slug == "" {
		writeError(w, http.StatusBadRequest, "slug is required")
		return
	}
	if req.Title == "" {
		writeError(w, http.StatusBadRequest, "title is required")
		return
	}
	if len(req.Content) > codeWikiMaxPageBytes {
		writeError(w, http.StatusBadRequest, "page content is too large")
		return
	}
	citations, err := validateCitations(req.Citations, decodeInventory(snapshot.RepoPaths))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	page, err := h.Queries.UpsertCodeWikiPage(r.Context(), db.UpsertCodeWikiPageParams{
		WorkspaceID:       snapshot.WorkspaceID,
		SnapshotID:        snapshot.ID,
		ProjectResourceID: snapshot.ProjectResourceID,
		Slug:              req.Slug,
		Title:             req.Title,
		Content:           req.Content,
		Citations:         citations,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write the wiki page")
		return
	}
	writeJSON(w, http.StatusCreated, CodeWikiPageResponse{
		ID:        uuidToString(page.ID),
		Slug:      page.Slug,
		Title:     page.Title,
		Content:   page.Content,
		Citations: decodeCitations(page.Citations),
		CommitSha: snapshot.CommitSha,
		Generated: true,
	})
}

// PublishProjectCodeWikiSnapshot flips a build to published in one statement
// (acceptance 2), then registers the MCP server for the workspace.
func (h *Handler) PublishProjectCodeWikiSnapshot(w http.ResponseWriter, r *http.Request) {
	snapshot, ok := h.loadBuildingSnapshot(w, r)
	if !ok {
		return
	}
	published, err := h.Queries.PublishCodeWikiSnapshot(r.Context(), db.PublishCodeWikiSnapshotParams{
		SnapshotID:  snapshot.ID,
		WorkspaceID: snapshot.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to publish the wiki snapshot")
		return
	}
	// Trim history to the current snapshot plus the one before it. Best effort:
	// the publication already happened and a failed prune is a disk-space
	// problem, not a correctness one.
	if err := h.Queries.DeleteCodeWikiPagesOutsideNewest(r.Context(), snapshot.ProjectResourceID); err != nil {
		slog.Warn("code wiki: prune pages failed", "err", err)
	} else if err := h.Queries.DeleteCodeWikiSnapshotsOutsideNewest(r.Context(), snapshot.ProjectResourceID); err != nil {
		slog.Warn("code wiki: prune snapshots failed", "err", err)
	}
	h.ensureCodeWikiMCPRegistration(r.Context(), snapshot.WorkspaceID, r.Header.Get("X-Agent-ID"))
	writeJSON(w, http.StatusOK, snapshotToResponse(published, ""))
}

// RefreshProjectCodeWiki is the human "Generate" action. It goes through the
// same claim-then-dispatch path as a merge, so the burst rule holds whichever
// way a run is started.
func (h *Handler) RefreshProjectCodeWiki(w http.ResponseWriter, r *http.Request) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	if !h.requireProjectWrite(w, r, project.ID) {
		return
	}
	resource, ok := h.loadWikiResource(w, r, project, r.URL.Query().Get("resource_id"))
	if !ok {
		return
	}
	started, reason := h.enqueueCodeWikiRefresh(r.Context(), resource, project, "")
	writeJSON(w, http.StatusAccepted, map[string]any{
		"started": started,
		"reason":  reason,
	})
}

// loadBuildingSnapshot resolves {sid} and refuses anything not still building —
// a published snapshot is immutable, which is what makes publication atomic.
func (h *Handler) loadBuildingSnapshot(w http.ResponseWriter, r *http.Request) (db.CodeWikiSnapshot, bool) {
	project, ok := h.loadProjectForResource(w, r, chi.URLParam(r, "id"))
	if !ok {
		return db.CodeWikiSnapshot{}, false
	}
	snapshotID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "sid"), "snapshot id")
	if !ok {
		return db.CodeWikiSnapshot{}, false
	}
	snapshot, err := h.Queries.GetCodeWikiSnapshot(r.Context(), db.GetCodeWikiSnapshotParams{
		ID: snapshotID, WorkspaceID: project.WorkspaceID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "wiki snapshot not found")
		return db.CodeWikiSnapshot{}, false
	}
	if snapshot.State != "building" {
		writeError(w, http.StatusConflict, "this wiki snapshot is already "+snapshot.State)
		return db.CodeWikiSnapshot{}, false
	}
	return snapshot, true
}

func taskUUIDFromRequest(r *http.Request) pgtype.UUID {
	id, err := util.ParseUUID(r.Header.Get("X-Task-ID"))
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}

// ── Claim and dispatch ──────────────────────────────────────────────────────

// claimCodeWikiBuild returns the resource's in-flight build, creating it when
// there is none. The second return is false only on a real database failure.
//
// `created` is what callers use to decide whether to dispatch a run: exactly
// one caller in a burst gets it.
func (h *Handler) claimCodeWikiBuild(ctx context.Context, resource db.ProjectResource, commitSha string, taskID pgtype.UUID) (db.CodeWikiSnapshot, bool) {
	snapshot, created, err := h.claimCodeWikiBuildSlot(ctx, resource, commitSha, taskID)
	if err != nil {
		return db.CodeWikiSnapshot{}, false
	}
	_ = created
	return snapshot, true
}

func (h *Handler) claimCodeWikiBuildSlot(ctx context.Context, resource db.ProjectResource, commitSha string, taskID pgtype.UUID) (db.CodeWikiSnapshot, bool, error) {
	// Release a slot held by a run that never finished, so one crash does not
	// freeze the resource's wiki permanently.
	if _, err := h.Queries.AbandonStaleCodeWikiBuilds(ctx, pgtype.Timestamptz{
		Time: time.Now().UTC().Add(-codeWikiBuildTimeout), Valid: true,
	}); err != nil {
		slog.Warn("code wiki: abandoning stale builds failed", "err", err)
	}
	snapshot, err := h.Queries.ClaimCodeWikiSnapshotBuild(ctx, db.ClaimCodeWikiSnapshotBuildParams{
		WorkspaceID:       resource.WorkspaceID,
		ProjectResourceID: resource.ID,
		CommitSha:         commitSha,
		GeneratedByTaskID: taskID,
	})
	if err == nil {
		return snapshot, true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return db.CodeWikiSnapshot{}, false, err
	}
	// The unique index refused the insert: a build is already in flight. This
	// is the burst case, and the correct answer is "adopt it, dispatch nothing".
	existing, err := h.Queries.GetBuildingCodeWikiSnapshot(ctx, resource.ID)
	if err != nil {
		return db.CodeWikiSnapshot{}, false, err
	}
	return existing, false, nil
}

// enqueueCodeWikiRefresh claims the build slot and, only when the claim was
// this caller's, dispatches the project's wiki autopilot.
//
// This is the single answer to acceptance 1. The autopilot layer cannot give it
// to us: its per-autopilot concurrency policy was removed years ago (it never
// cancelled anything) and its idempotency keys are scoped to an entire quota
// period, which would collapse every merge of the month into one run. The
// partial unique index on the build slot collapses exactly a burst and nothing
// wider.
func (h *Handler) enqueueCodeWikiRefresh(ctx context.Context, resource db.ProjectResource, project db.Project, commitSha string) (bool, string) {
	if h.AutopilotService == nil {
		return false, "autopilot dispatch is not configured on this server"
	}
	autopilot, trigger, found := h.findCodeWikiAutopilot(ctx, resource.WorkspaceID, project.ID)
	if !found {
		return false, "no code wiki daemon is installed on this project"
	}
	snapshot, created, err := h.claimCodeWikiBuildSlot(ctx, resource, commitSha, pgtype.UUID{})
	if err != nil {
		slog.Warn("code wiki: claim failed", "err", err, "resource_id", uuidToString(resource.ID))
		return false, "could not claim a wiki build"
	}
	if !created {
		return false, "a wiki generation is already running for this repository"
	}
	payload, err := json.Marshal(map[string]any{
		"event":               "code_wiki.refresh",
		"snapshot_id":         uuidToString(snapshot.ID),
		"project_resource_id": uuidToString(resource.ID),
		"project_id":          uuidToString(project.ID),
		"commit_sha":          commitSha,
		"resource_ref":        json.RawMessage(resource.ResourceRef),
	})
	if err != nil {
		payload = nil
	}
	if _, err := h.AutopilotService.DispatchAutopilot(ctx, autopilot, trigger.ID, "webhook", payload); err != nil {
		// The dispatch errored, so no run exists and none ever will. Release
		// the slot immediately rather than making the next merge wait out the
		// abandonment timeout.
		//
		// A run the admission gate SKIPS is deliberately not treated this way.
		// It holds the slot until the timeout, which is what keeps "one run per
		// burst" true even when the autopilot is misconfigured: releasing on a
		// skip would let each of ten near-simultaneous merges record its own
		// skipped run.
		slog.Warn("code wiki: dispatch failed", "err", err, "autopilot_id", uuidToString(autopilot.ID))
		if _, ferr := h.Queries.FailCodeWikiSnapshot(ctx, db.FailCodeWikiSnapshotParams{
			ID: snapshot.ID, WorkspaceID: snapshot.WorkspaceID,
		}); ferr != nil {
			slog.Warn("code wiki: releasing the slot after a failed dispatch failed", "err", ferr)
		}
		return false, "could not start the wiki run"
	}
	return true, ""
}

// findCodeWikiAutopilot returns the project's active autopilot carrying an
// enabled webhook trigger labelled `code-wiki`. That label is what the wiki
// daemon declares, so installing the daemon is what opts a project in.
func (h *Handler) findCodeWikiAutopilot(ctx context.Context, workspaceID, projectID pgtype.UUID) (db.Autopilot, db.AutopilotTrigger, bool) {
	row, err := h.Queries.FindCodeWikiAutopilotForProject(ctx, db.FindCodeWikiAutopilotForProjectParams{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		Label:       pgtype.Text{String: codeWikiTriggerLabel, Valid: true},
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("code wiki: looking up the wiki autopilot failed", "err", err)
		}
		return db.Autopilot{}, db.AutopilotTrigger{}, false
	}
	return row.Autopilot, row.AutopilotTrigger, true
}

// ── Auto-registration (acceptance 6) ────────────────────────────────────────

// ensureCodeWikiMCPRegistration makes the hosted wiki server reachable by the
// agents that generate it. Idempotent by construction: the library row is
// upserted on the (workspace_id, name) unique index and the per-agent binding
// on its own primary key, so publishing a hundred snapshots yields one row and
// one link.
func (h *Handler) ensureCodeWikiMCPRegistration(ctx context.Context, workspaceID pgtype.UUID, agentID string) {
	base := strings.TrimRight(h.cfg.PublicURL, "/")
	if base == "" {
		// The row is still written so the entry is visible and repairable in
		// the workspace's MCP settings, but say why it cannot be dialled: a
		// deployment with no public URL cannot mint one from request headers
		// without letting a misconfigured proxy choose the host.
		slog.Warn("code wiki: registering the MCP server without a public base URL; set it and republish to make the entry dialable")
	}
	config, err := json.Marshal(map[string]any{
		"type": "http",
		"url":  base + codeWikiMCPPath,
	})
	if err != nil {
		return
	}
	server, err := h.Queries.UpsertCodeWikiMcpServer(ctx, db.UpsertCodeWikiMcpServerParams{
		WorkspaceID: workspaceID,
		Name:        codeWikiMCPServerName,
		Config:      config,
	})
	if err != nil {
		slog.Warn("code wiki: registering the MCP server failed", "err", err)
		return
	}
	agentUUID, err := util.ParseUUID(agentID)
	if err != nil {
		return
	}
	if err := h.Queries.AddAgentMcpServer(ctx, db.AddAgentMcpServerParams{
		AgentID: agentUUID, ServerID: server.ID,
	}); err != nil {
		slog.Warn("code wiki: binding the MCP server to the agent failed", "err", err)
	}
}

// purgeCodeWikiForResource removes a repository's wiki when the resource that
// pointed at it is detached. No FK does this for us, by repository rule.
func (h *Handler) purgeCodeWikiForResource(ctx context.Context, resourceID pgtype.UUID) {
	if err := h.Queries.DeleteCodeWikiPagesByResource(ctx, resourceID); err != nil {
		slog.Warn("code wiki: purging pages failed", "err", err)
		return
	}
	if err := h.Queries.DeleteCodeWikiSnapshotsByResource(ctx, resourceID); err != nil {
		slog.Warn("code wiki: purging snapshots failed", "err", err)
	}
}

// ── Post-merge trigger (acceptance 1) ───────────────────────────────────────

// triggerCodeWikiForMergedPR is called once per workspace when a pull request
// mirrors as merged. It regenerates the wiki of every project in that workspace
// that tracks the repository the PR belongs to.
//
// Called on every merge event, including a burst of ten in two seconds. Only
// one run comes out of that burst: enqueueCodeWikiRefresh claims the resource's
// single build slot through a partial unique index, and dispatches only when
// the claim was its own.
func (h *Handler) triggerCodeWikiForMergedPR(ctx context.Context, workspaceID pgtype.UUID, owner, repo, commitSha string) {
	if owner == "" || repo == "" {
		return
	}
	resources, err := h.Queries.ListWorkspaceGithubRepoResources(ctx, workspaceID)
	if err != nil {
		slog.Warn("code wiki: listing repo resources failed", "err", err)
		return
	}
	for _, resource := range resources {
		if !repoRefMatchesGitHub(resource.ResourceRef, owner, repo) {
			continue
		}
		project, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
			ID: resource.ProjectID, WorkspaceID: workspaceID,
		})
		if err != nil {
			continue
		}
		if started, reason := h.enqueueCodeWikiRefresh(ctx, resource, project, commitSha); !started {
			slog.Debug("code wiki: merge did not start a run",
				"resource_id", uuidToString(resource.ID), "reason", reason)
		}
	}
}

// repoRefMatchesGitHub reports whether a github_repo resource ref points at
// owner/repo. The ref stores a URL in whichever form the user pasted, so the
// comparison is on the owner/name pair the URL encodes rather than on the
// string — https, ssh and scp shorthand for the same repository must all match.
func repoRefMatchesGitHub(ref []byte, owner, repo string) bool {
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(ref, &payload); err != nil {
		return false
	}
	gotOwner, gotRepo, ok := gitHubOwnerRepo(payload.URL)
	return ok && strings.EqualFold(gotOwner, owner) && strings.EqualFold(gotRepo, repo)
}

// gitHubOwnerRepo extracts owner and repository name from the URL forms
// project_resource accepts: https://host/owner/repo(.git), ssh://host/owner/repo
// and the scp shorthand git@host:owner/repo(.git).
func gitHubOwnerRepo(raw string) (string, string, bool) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "", "", false
	}
	if idx := strings.Index(s, "://"); idx >= 0 {
		s = s[idx+3:]
		if at := strings.Index(s, "@"); at >= 0 {
			s = s[at+1:]
		}
		if slash := strings.Index(s, "/"); slash >= 0 {
			s = s[slash+1:]
		} else {
			return "", "", false
		}
	} else if colon := strings.Index(s, ":"); colon >= 0 {
		// scp shorthand: git@github.com:owner/repo.git
		s = s[colon+1:]
	} else {
		return "", "", false
	}
	s = strings.TrimSuffix(strings.Trim(s, "/"), ".git")
	parts := strings.Split(s, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
