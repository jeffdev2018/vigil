package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/permissionprofile"
)

// Run-scoped secrets (K09). An agent env key listed in
// agent.scoped_env_keys never reaches a run in clear: the claim replaces its
// value with an opaque `mss_` token bound to this run, the daemon's MCP
// broker resolves the token on the way out (never the agent process, never
// the daemon disk), and every terminal status revokes what is left.

const (
	// RunSecretTTL caps a token's life even when the run outlives it.
	// ponytail: fixed cap; make it a workspace setting when long runs need it.
	RunSecretTTL          = 30 * time.Minute
	runSecretTokenPrefix  = "mss_"
	AuditRunSecretIssued  = "run_secret.issued"
	AuditRunSecretRevoked = "run_secret.revoked"
	ErrCodeSecretRevoked  = "secret_revoked"
	ErrCodeSecretExpired  = "secret_expired"
)

type RunSecretResponse struct {
	ID        string  `json:"id"`
	TaskID    string  `json:"task_id"`
	Key       string  `json:"key"`
	Status    string  `json:"status"` // active | revoked | expired
	ExpiresAt string  `json:"expires_at"`
	RevokedAt *string `json:"revoked_at"`
	Reason    *string `json:"revoke_reason"`
	CreatedAt string  `json:"created_at"`
}

func runSecretStatus(s db.RunScopedSecret, now time.Time) string {
	switch {
	case s.RevokedAt.Valid:
		return "revoked"
	case s.ExpiresAt.Valid && !now.Before(s.ExpiresAt.Time):
		return "expired"
	}
	return "active"
}

func runSecretToResponse(s db.RunScopedSecret) RunSecretResponse {
	return RunSecretResponse{
		ID: uuidToString(s.ID), TaskID: uuidToString(s.TaskID), Key: s.Key, Status: runSecretStatus(s, time.Now()),
		ExpiresAt: timestampToString(s.ExpiresAt), RevokedAt: timestampToPtr(s.RevokedAt), Reason: textToPtr(s.RevokeReason), CreatedAt: timestampToString(s.CreatedAt),
	}
}

func newRunSecretToken() (token, hash string, err error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token = runSecretTokenPrefix + hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}

func scopedEnvKeys(agent db.Agent) []string {
	return jsonStrings(agent.ScopedEnvKeys)
}

// issueRunSecrets swaps every scoped key's value for a fresh token. Keys
// the permission profile hides are dropped instead: no token, no value.
func (h *Handler) issueRunSecrets(ctx context.Context, task db.AgentTaskQueue, agent db.Agent, profile *permissionprofile.Profile, env map[string]string) map[string]string {
	scoped := scopedEnvKeys(agent)
	if len(scoped) == 0 || len(env) == 0 {
		return env
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
	}
	var issued []string
	for _, key := range scoped {
		if _, ok := out[key]; !ok {
			continue
		}
		if profile != nil && profile.HidesSecret(key) {
			delete(out, key)
			continue
		}
		token, hash, err := newRunSecretToken()
		if err != nil {
			slog.Error("run secret: token generation failed; key withheld", "task_id", uuidToString(task.ID), "key", key, "error", err)
			delete(out, key)
			continue
		}
		if _, err := h.Queries.CreateRunScopedSecret(ctx, db.CreateRunScopedSecretParams{
			ID: dbid.NewV7(), WorkspaceID: agent.WorkspaceID, TaskID: task.ID, AgentID: agent.ID, Key: key, TokenHash: hash,
			ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(RunSecretTTL), Valid: true},
		}); err != nil {
			slog.Error("run secret: issue failed; key withheld", "task_id", uuidToString(task.ID), "key", key, "error", err)
			delete(out, key)
			continue
		}
		out[key] = token
		issued = append(issued, key)
	}
	if len(issued) > 0 {
		sort.Strings(issued)
		h.audit(ctx, agent.WorkspaceID, "agent", uuidToString(agent.ID), AuditRunSecretIssued, "task", task.ID, map[string]any{"keys": issued, "ttl_minutes": int(RunSecretTTL.Minutes())}, nil)
	}
	return out
}

// revokeRunSecrets is called at every terminal status and by the manual
// endpoint. Idempotent: an already revoked row is left alone.
func (h *Handler) revokeRunSecrets(ctx context.Context, taskID pgtype.UUID, reason, actorType, actorID string) int {
	rows, err := h.Queries.RevokeRunScopedSecretsByTask(ctx, db.RevokeRunScopedSecretsByTaskParams{TaskID: taskID, RevokeReason: pgtype.Text{String: reason, Valid: true}})
	if err != nil {
		slog.Warn("run secret: revoke failed", "task_id", uuidToString(taskID), "error", err)
		return 0
	}
	if len(rows) > 0 {
		keys := make([]string, 0, len(rows))
		for _, s := range rows {
			keys = append(keys, s.Key)
		}
		h.audit(ctx, rows[0].WorkspaceID, actorType, actorID, AuditRunSecretRevoked, "task", taskID, map[string]any{"keys": keys, "reason": reason}, nil)
	}
	return len(rows)
}

// ListTaskRunSecrets: GET /api/tasks/{taskId}/secrets — keys and status, never values.
func (h *Handler) ListTaskRunSecrets(w http.ResponseWriter, r *http.Request) {
	task, ok := h.gateTask(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListRunScopedSecretsByTask(r.Context(), task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list run secrets")
		return
	}
	out := make([]RunSecretResponse, 0, len(rows))
	for _, s := range rows {
		out = append(out, runSecretToResponse(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"secrets": out})
}

// ListIssueRunSecrets: GET /api/issues/{id}/run-secrets — every run of the issue.
func (h *Handler) ListIssueRunSecrets(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForUser(w, r, chi.URLParam(r, "id"))
	if !ok {
		return
	}
	rows, err := h.Queries.ListRunScopedSecretsByIssue(r.Context(), issue.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list run secrets")
		return
	}
	out := make([]RunSecretResponse, 0, len(rows))
	for _, s := range rows {
		out = append(out, runSecretToResponse(s))
	}
	writeJSON(w, http.StatusOK, map[string]any{"secrets": out})
}

// RevokeTaskRunSecrets: POST /api/tasks/{taskId}/secrets/revoke-all — the
// run itself (at exit) or an admin (early).
func (h *Handler) RevokeTaskRunSecrets(w http.ResponseWriter, r *http.Request) {
	task, ok := h.gateTask(w, r)
	if !ok {
		return
	}
	agent, err := h.Queries.GetAgent(r.Context(), task.AgentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	actorType, actorID, reason := "agent", uuidToString(agent.ID), "run_revoked"
	if !isMachineCredentialActor(r) {
		if _, ok := h.requireWorkspaceRole(w, r, uuidToString(agent.WorkspaceID), "task not found", "owner", "admin"); !ok {
			return
		}
		actorType, actorID, reason = "member", requestUserID(r), "manual"
	}
	n := h.revokeRunSecrets(r.Context(), task.ID, reason, actorType, actorID)
	writeJSON(w, http.StatusOK, map[string]any{"revoked": n})
}

// ResolveRunSecret: POST /api/tasks/{taskId}/secrets/resolve {token} — only
// the run's own credential may ask, and only for its own tokens. This is
// the single place a value leaves the server.
func (h *Handler) ResolveRunSecret(w http.ResponseWriter, r *http.Request) {
	if !isMachineCredentialActor(r) {
		writeError(w, http.StatusForbidden, "only a run may resolve its secrets")
		return
	}
	task, ok := h.gateTask(w, r)
	if !ok {
		return
	}
	var req struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !strings.HasPrefix(req.Token, runSecretTokenPrefix) {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}
	sum := sha256.Sum256([]byte(req.Token))
	row, err := h.Queries.GetRunScopedSecretByHash(r.Context(), db.GetRunScopedSecretByHashParams{TaskID: task.ID, TokenHash: hex.EncodeToString(sum[:])})
	if err != nil {
		writeError(w, http.StatusNotFound, "unknown secret token for this run")
		return
	}
	switch runSecretStatus(row, time.Now()) {
	case "revoked":
		writeErrorCode(w, http.StatusForbidden, ErrCodeSecretRevoked, "this run's secret "+row.Key+" was revoked; the run must not retry it")
		return
	case "expired":
		writeErrorCode(w, http.StatusForbidden, ErrCodeSecretExpired, "this run's secret "+row.Key+" expired after "+RunSecretTTL.String())
		return
	}
	agent, err := h.Queries.GetAgent(r.Context(), task.AgentID)
	if err != nil {
		writeError(w, http.StatusNotFound, "agent not found")
		return
	}
	value, ok := unmarshalCustomEnv(agent)[row.Key]
	if !ok {
		writeError(w, http.StatusNotFound, "the secret source no longer exists")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": row.Key, "value": value})
}
