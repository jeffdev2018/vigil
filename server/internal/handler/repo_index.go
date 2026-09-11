package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Shared semantic repo index (K47). Two surfaces live here:
//
//   - the daemon write path (diff / upsert / prune), which is how the index is
//     built at all — the server has no checkout, the daemon does;
//   - the workspace settings surface, which is the workspace's opt-in per
//     repository plus what it can see of the result.
//
// The read path is not an endpoint: hints ride the claim response, assembled in
// daemon.go next to the workspace Brain notes.

// AuditRepoIndexSettings records a change to a repository's opt-in.
const AuditRepoIndexSettings = "repo_index_settings"

// repoIndexRequestLimit bounds an upsert body. 200 chunks of at most ~8k runes
// plus their paths fits comfortably; the ceiling exists so a malformed or
// hostile daemon cannot make the server buffer an unbounded body.
const repoIndexRequestLimit = 8 << 20

// repoIndexClaimTopK is how many hints one run receives. Eight excerpts of at
// most 600 runes is about a page of brief: enough to name the right files,
// short enough that the run still has to open them.
const repoIndexClaimTopK = 8

// repoIndexer builds the service with this deployment's embeddings client.
// A nil or disabled LLM client keeps the index lexical rather than turning it
// off — see service/repo_index.go.
func (h *Handler) repoIndexer() *service.RepoIndexer {
	x := &service.RepoIndexer{Queries: h.Queries, TxStarter: h.TxStarter}
	if h.LLM != nil {
		x.Embedder = h.LLM
	}
	return x
}

// --- Daemon write path -----------------------------------------------------

type repoIndexFileHash struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
}

type repoIndexDiffRequest struct {
	RepoIdentifier string              `json:"repo_identifier"`
	Files          []repoIndexFileHash `json:"files"`
}

type repoIndexDiffResponse struct {
	Missing []string `json:"missing"`
	Stale   []string `json:"stale"`
}

// DiffRepoIndex: POST /api/daemon/workspaces/{workspaceId}/repo-index/diff.
//
// The daemon sends what it found on the repo's default branch; the server
// answers which of those files it has no chunks for and which changed. Only
// those are read, chunked and uploaded, so a repo that did not move since the
// last run costs one request and no file I/O.
func (h *Handler) DiffRepoIndex(w http.ResponseWriter, r *http.Request) {
	var req repoIndexDiffRequest
	if !decodeRepoIndexBody(w, r, &req) {
		return
	}
	wsUUID, repo, ok := h.repoIndexDaemonScope(w, r, req.RepoIdentifier)
	if !ok {
		return
	}
	files := make(map[string]string, len(req.Files))
	for _, f := range req.Files {
		path := strings.TrimSpace(f.Path)
		if path == "" {
			continue
		}
		files[path] = f.Hash
	}
	missing, stale, err := h.repoIndexer().DiffFiles(r.Context(), wsUUID, repo, files)
	if err != nil {
		slog.Error("repo index: diff failed", "workspace_id", uuidToString(wsUUID), "repo", repo, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to diff the repository index")
		return
	}
	writeJSON(w, http.StatusOK, repoIndexDiffResponse{Missing: missing, Stale: stale})
}

type repoIndexUpsertRequest struct {
	RepoIdentifier string                   `json:"repo_identifier"`
	Commit         string                   `json:"commit"`
	Chunks         []service.RepoIndexChunk `json:"chunks"`
}

type repoIndexUpsertResponse struct {
	Stored int `json:"stored"`
}

// UpsertRepoIndex: POST /api/daemon/workspaces/{workspaceId}/repo-index/upsert.
func (h *Handler) UpsertRepoIndex(w http.ResponseWriter, r *http.Request) {
	var req repoIndexUpsertRequest
	if !decodeRepoIndexBody(w, r, &req) {
		return
	}
	wsUUID, repo, ok := h.repoIndexDaemonScope(w, r, req.RepoIdentifier)
	if !ok {
		return
	}
	if len(req.Chunks) > service.RepoIndexUpsertBatch {
		writeError(w, http.StatusBadRequest, "too many chunks in one batch")
		return
	}
	chunks := make([]service.RepoIndexChunk, 0, len(req.Chunks))
	for _, chunk := range req.Chunks {
		chunk.FilePath = strings.TrimSpace(chunk.FilePath)
		// The table's CHECK refuses an empty path; drop it here so one bad entry
		// does not fail a batch that is otherwise good.
		if chunk.FilePath == "" || strings.TrimSpace(chunk.Content) == "" {
			continue
		}
		chunks = append(chunks, chunk)
	}
	stored, err := h.repoIndexer().UpsertChunks(r.Context(), wsUUID, repo, strings.TrimSpace(req.Commit), chunks)
	if err != nil {
		slog.Error("repo index: upsert failed", "workspace_id", uuidToString(wsUUID), "repo", repo, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to store the repository index chunks")
		return
	}
	writeJSON(w, http.StatusOK, repoIndexUpsertResponse{Stored: stored})
}

type repoIndexPruneRequest struct {
	RepoIdentifier string `json:"repo_identifier"`
	// Commit the pass verified every remaining file at. Optional: an older
	// daemon that omits it leaves the stored commits alone, which costs some
	// hints a spurious "older commit" label but never mislabels a current
	// chunk as fresh.
	Commit       string   `json:"commit"`
	PresentPaths []string `json:"present_paths"`
}

// PruneRepoIndex: POST /api/daemon/workspaces/{workspaceId}/repo-index/prune.
//
// Closes an indexing pass by dropping the chunks of files that no longer exist.
// The service refuses an empty present list; that refusal is a 400 here because
// it means the daemon's walk produced nothing, which is a bug on its side, not
// a server error.
func (h *Handler) PruneRepoIndex(w http.ResponseWriter, r *http.Request) {
	var req repoIndexPruneRequest
	if !decodeRepoIndexBody(w, r, &req) {
		return
	}
	wsUUID, repo, ok := h.repoIndexDaemonScope(w, r, req.RepoIdentifier)
	if !ok {
		return
	}
	paths := make([]string, 0, len(req.PresentPaths))
	for _, p := range req.PresentPaths {
		if p = strings.TrimSpace(p); p != "" {
			paths = append(paths, p)
		}
	}
	if err := h.repoIndexer().Prune(r.Context(), wsUUID, repo, req.Commit, paths); err != nil {
		if len(paths) == 0 {
			writeError(w, http.StatusBadRequest, "present_paths must not be empty")
			return
		}
		slog.Error("repo index: prune failed", "workspace_id", uuidToString(wsUUID), "repo", repo, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to prune the repository index")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// repoIndexDaemonScope authorises one daemon write request: the workspace from
// the path, and the workspace's opt-in for the repository named in the body.
//
// The opt-in check is here rather than in each handler because it is the whole
// consent model: a daemon that indexes a repo the workspace did not enable has
// copied source code into the database without being asked, and a check that
// lives in three places is a check that will eventually live in two.
func (h *Handler) repoIndexDaemonScope(w http.ResponseWriter, r *http.Request, repoIdentifier string) (pgtype.UUID, string, bool) {
	workspaceID := strings.TrimSpace(chi.URLParam(r, "workspaceId"))
	if !h.requireDaemonWorkspaceAccess(w, r, workspaceID) {
		return pgtype.UUID{}, "", false
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return pgtype.UUID{}, "", false
	}
	repo := strings.TrimSpace(repoIdentifier)
	if repo == "" {
		writeError(w, http.StatusBadRequest, "repo_identifier is required")
		return pgtype.UUID{}, "", false
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return pgtype.UUID{}, "", false
	}
	if !service.RepoIndexEnabled(ws.Settings, repo) {
		writeError(w, http.StatusConflict, service.ErrRepoIndexDisabled.Error())
		return pgtype.UUID{}, "", false
	}
	return wsUUID, repo, true
}

// --- Settings surface ------------------------------------------------------

type repoIndexRepoStatus struct {
	RepoIdentifier    string `json:"repo_identifier"`
	Enabled           bool   `json:"enabled"`
	ChunkCount        int64  `json:"chunk_count"`
	FileCount         int64  `json:"file_count"`
	LastIndexedCommit string `json:"last_indexed_commit"`
	LastIndexedAt     string `json:"last_indexed_at,omitempty"`
	// UnusableEmbeddingCount surfaces chunks whose vector the current
	// embedding model cannot compare against, so a model change is visible as
	// "re-index this repo" instead of as ranking that quietly got worse.
	UnusableEmbeddingCount int64 `json:"unusable_embedding_count"`
}

type repoIndexSettingsResponse struct {
	Repos []repoIndexRepoStatus `json:"repos"`
	// EmbeddingsEnabled tells the UI whether this deployment ranks by meaning or
	// by keywords, so the block can say which without the frontend guessing from
	// an unrelated flag.
	EmbeddingsEnabled bool `json:"embeddings_enabled"`
}

// GetRepoIndexSettings: GET /api/repo-index/settings.
//
// The repository list is derived, not stored: it is every repo the workspace's
// projects and workspace settings actually reference. A repo that was removed
// from every project stops being offered here even if its opt-in row lingers.
func (h *Handler) GetRepoIndexSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r)
	if !ok {
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	cfg := service.RepoIndexSettingsFromSettings(ws.Settings)
	indexer := h.repoIndexer()

	out := repoIndexSettingsResponse{Repos: []repoIndexRepoStatus{}}
	if h.LLM != nil {
		out.EmbeddingsEnabled = h.LLM.EmbeddingsEnabled()
	}
	for _, repo := range h.workspaceRepoIdentifiers(r, wsUUID, ws.Repos, cfg) {
		status := repoIndexRepoStatus{RepoIdentifier: repo, Enabled: cfg[repo].Enabled}
		if stats, err := indexer.Stats(r.Context(), wsUUID, repo); err == nil {
			status.ChunkCount = stats.ChunkCount
			status.FileCount = stats.FileCount
			status.LastIndexedCommit = stats.LastIndexedCommit
			status.LastIndexedAt = stats.LastIndexedAt
			status.UnusableEmbeddingCount = stats.UnusableEmbeddingCount
		} else {
			slog.Warn("repo index: stats failed", "workspace_id", uuidToString(wsUUID), "repo", repo, "error", err)
		}
		out.Repos = append(out.Repos, status)
	}
	writeJSON(w, http.StatusOK, out)
}

type repoIndexSettingsRequest struct {
	RepoIdentifier string `json:"repo_identifier"`
	Enabled        bool   `json:"enabled"`
}

// PutRepoIndexSettings: PUT /api/repo-index/settings {repo_identifier, enabled}.
//
// Turning a repository OFF purges its chunks. The toggle is the workspace's
// answer to "may this code be stored here at all", so leaving the rows behind
// would make "off" mean something weaker than what the UI says it means.
func (h *Handler) PutRepoIndexSettings(w http.ResponseWriter, r *http.Request) {
	wsUUID, ok := h.permissionProfileScope(w, r, "owner", "admin")
	if !ok {
		return
	}
	var req repoIndexSettingsRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	repo := strings.TrimSpace(req.RepoIdentifier)
	if repo == "" {
		writeError(w, http.StatusBadRequest, "repo_identifier is required")
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}

	cfg := service.RepoIndexSettingsFromSettings(ws.Settings)
	cfg[repo] = service.RepoIndexSettings{Enabled: req.Enabled}
	// Merged server-side (MergeWorkspaceSettings): a read-modify-write of the
	// whole settings blob lost the writes of any concurrent settings PUT on
	// a different key (this endpoint's own repo_index map is still a
	// read-modify-write across repos, unchanged from before).
	raw, err := json.Marshal(map[string]any{"repo_index": cfg})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the repository index setting")
		return
	}
	if _, err := h.Queries.MergeWorkspaceSettings(r.Context(), db.MergeWorkspaceSettingsParams{ID: wsUUID, Settings: raw}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the repository index setting")
		return
	}

	// Purge AFTER the setting is persisted: if the write failed the index is
	// still live and still consented to, so deleting first would destroy data
	// the workspace never asked to remove.
	if !req.Enabled {
		if err := h.repoIndexer().PurgeRepo(r.Context(), wsUUID, repo); err != nil {
			slog.Error("repo index: purge on disable failed", "workspace_id", uuidToString(wsUUID), "repo", repo, "error", err)
		}
	}

	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditRepoIndexSettings, "workspace", wsUUID,
		map[string]any{"repo_identifier": repo, "enabled": req.Enabled}, nil)

	status := repoIndexRepoStatus{RepoIdentifier: repo, Enabled: req.Enabled}
	if stats, err := h.repoIndexer().Stats(r.Context(), wsUUID, repo); err == nil {
		status.ChunkCount = stats.ChunkCount
		status.FileCount = stats.FileCount
		status.LastIndexedCommit = stats.LastIndexedCommit
		status.LastIndexedAt = stats.LastIndexedAt
		status.UnusableEmbeddingCount = stats.UnusableEmbeddingCount
	}
	writeJSON(w, http.StatusOK, status)
}

// workspaceRepoIdentifiers lists every repository URL this workspace knows:
// the workspace-level repos plus the github_repo resources of its projects,
// plus any repo that already carries an opt-in (so a setting is never orphaned
// out of the UI and left silently on).
func (h *Handler) workspaceRepoIdentifiers(r *http.Request, wsUUID pgtype.UUID, workspaceRepos []byte, cfg map[string]service.RepoIndexSettings) []string {
	seen := map[string]struct{}{}
	for _, repo := range parseWorkspaceRepos(workspaceRepos) {
		if repo.URL != "" {
			seen[repo.URL] = struct{}{}
		}
	}
	refs, err := h.Queries.ListWorkspaceRepoResourceRefs(r.Context(), wsUUID)
	if err != nil {
		slog.Warn("repo index: list project repo resources failed", "workspace_id", uuidToString(wsUUID), "error", err)
	}
	for _, ref := range refs {
		var payload struct {
			URL string `json:"url"`
		}
		if json.Unmarshal(ref, &payload) == nil {
			if url := strings.TrimSpace(payload.URL); url != "" {
				seen[url] = struct{}{}
			}
		}
	}
	for repo := range cfg {
		seen[repo] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for repo := range seen {
		out = append(out, repo)
	}
	sort.Strings(out)
	return out
}

// --- shared plumbing -------------------------------------------------------

func decodeRepoIndexBody(w http.ResponseWriter, r *http.Request, out any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, repoIndexRequestLimit)).Decode(out); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

// RepoIndexHintContext is one repo-index hit as the claim response carries it.
// Mirror type: daemon/execenv.RepoIndexHintForEnv, same JSON names.
type RepoIndexHintContext struct {
	RepoIdentifier string  `json:"repo_identifier"`
	FilePath       string  `json:"file_path"`
	Symbol         string  `json:"symbol,omitempty"`
	StartLine      int     `json:"start_line"`
	EndLine        int     `json:"end_line"`
	Snippet        string  `json:"snippet"`
	Score          float64 `json:"score"`
	Stale          bool    `json:"stale,omitempty"`
}

// repoIndexHintsForClaim retrieves the hints one claimed task gets.
//
// Non-blocking by contract, like the workspace Brain notes it sits next to: a
// failed read costs the run its orientation section, never its dispatch. Repos
// are those of the task's resolved context, filtered to the ones the workspace
// opted in to; a workspace with no opted-in repo does no work here at all.
func (h *Handler) repoIndexHintsForClaim(
	ctx context.Context,
	workspaceID pgtype.UUID,
	settings []byte,
	repos []RepoData,
	query string,
) (hints []RepoIndexHintContext, enabled []string) {
	query = strings.TrimSpace(query)
	cfg := service.RepoIndexSettingsFromSettings(settings)
	if len(cfg) == 0 || len(repos) == 0 {
		return nil, nil
	}
	indexer := h.repoIndexer()
	seen := map[string]struct{}{}
	for _, repo := range repos {
		url := strings.TrimSpace(repo.URL)
		if url == "" || !cfg[url].Enabled {
			continue
		}
		if _, dup := seen[url]; dup {
			continue
		}
		seen[url] = struct{}{}
		enabled = append(enabled, url)
		if query == "" {
			continue
		}
		found, err := indexer.Query(ctx, workspaceID, url, query, repoIndexClaimTopK)
		if err != nil {
			slog.Warn("repo index: claim query failed; continuing without hints",
				"workspace_id", uuidToString(workspaceID), "repo", url, "error", err)
			continue
		}
		for _, hint := range found {
			hints = append(hints, RepoIndexHintContext{
				RepoIdentifier: hint.RepoIdentifier,
				FilePath:       hint.FilePath,
				Symbol:         hint.Symbol,
				StartLine:      hint.StartLine,
				EndLine:        hint.EndLine,
				Snippet:        hint.Snippet,
				Score:          hint.Score,
				Stale:          hint.Stale,
			})
		}
	}
	sort.Strings(enabled)
	// Across repos the per-repo scores are comparable (same formula, same
	// query), so one global ordering is honest and the run reads the best hits
	// first whichever repo they came from.
	sort.SliceStable(hints, func(i, j int) bool { return hints[i].Score > hints[j].Score })
	if len(hints) > repoIndexClaimTopK {
		hints = hints[:repoIndexClaimTopK]
	}
	return hints, enabled
}

// repoIndexClaimQuery is what a claimed issue is searched WITH: its title plus
// the head of its description. The head, not the whole body — a long issue's
// tail is discussion and acceptance criteria, which match every file in the
// repository equally and only dilute the ranking.
func repoIndexClaimQuery(title, description string) string {
	title = strings.TrimSpace(title)
	description = strings.TrimSpace(description)
	if description == "" {
		return title
	}
	return strings.TrimSpace(title + "\n" + truncateRepoIndexQuery(description))
}

func truncateRepoIndexQuery(s string) string {
	const budget = 500
	runes := []rune(s)
	if len(runes) <= budget {
		return s
	}
	return string(runes[:budget])
}
