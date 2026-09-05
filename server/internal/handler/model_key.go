package handler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/modelkey"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// BYOK model keys (K48). A workspace or a project declares the API key its
// runs spend for a vendor; the claim resolves the active key of highest
// priority for the run's project then workspace and injects it into the
// run's environment when the agent does not set its own; usage rows carry
// the key that paid; a run failing on the vendor's authentication or quota
// retires the key, alerts the workspace's managers and retries once on the
// next key. The key value never leaves the server after creation.

const (
	InboxTypeModelKeyAlert = "model_key_alert"
	AuditModelKeyCreated   = "model_key.created"
	AuditModelKeyRetired   = "model_key.retired"
)

func (h *Handler) modelKeysConfigured() bool { return h.ModelKeySecretBox != nil }

func (h *Handler) sealModelKey(plaintext string) (string, error) {
	sealed, err := h.ModelKeySecretBox.Seal([]byte(plaintext))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (h *Handler) openModelKey(enc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	plain, err := h.ModelKeySecretBox.Open(raw)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// ModelKeyResponse never carries the key: a hint is all the UI gets.
type ModelKeyResponse struct {
	ID                string  `json:"id"`
	WorkspaceID       string  `json:"workspace_id"`
	Scope             string  `json:"scope"`
	ScopeID           *string `json:"scope_id"`
	Provider          string  `json:"provider"`
	Label             string  `json:"label"`
	KeyHint           string  `json:"key_hint"`
	Active            bool    `json:"active"`
	Priority          int32   `json:"priority"`
	DeactivatedReason string  `json:"deactivated_reason"`
	DeactivatedAt     *string `json:"deactivated_at"`
	CreatedBy         *string `json:"created_by"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

type ModelKeyUsageResponse struct {
	ModelKeyID       string `json:"model_key_id"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	TaskCount        int64  `json:"task_count"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	CostUsdTicks     int64  `json:"cost_usd_ticks"`
}

func modelKeyToResponse(k db.WorkspaceModelKey) ModelKeyResponse {
	return ModelKeyResponse{
		ID: uuidToString(k.ID), WorkspaceID: uuidToString(k.WorkspaceID), Scope: k.Scope, ScopeID: uuidToPtr(k.ScopeID), Provider: k.Provider,
		Label: k.Label, KeyHint: k.KeyHint, Active: k.Active, Priority: k.Priority, DeactivatedReason: k.DeactivatedReason,
		DeactivatedAt: timestampToPtr(k.DeactivatedAt), CreatedBy: uuidToPtr(k.CreatedBy), CreatedAt: timestampToString(k.CreatedAt), UpdatedAt: timestampToString(k.UpdatedAt),
	}
}

func (h *Handler) requireModelKeyWriter(w http.ResponseWriter, r *http.Request, workspaceID string) bool {
	member, ok := h.workspaceMember(w, r, workspaceID)
	if !ok {
		return false
	}
	if actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID); actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents cannot manage model keys")
		return false
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return false
	}
	return true
}

// GET /api/workspaces/{id}/model-keys — hints, status and usage per key.
func (h *Handler) ListModelKeys(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.workspaceMember(w, r, workspaceID); !ok {
		return
	}
	rows, err := h.Queries.ListModelKeys(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list model keys")
		return
	}
	keys := make([]ModelKeyResponse, 0, len(rows))
	for _, k := range rows {
		keys = append(keys, modelKeyToResponse(k))
	}
	usage := []ModelKeyUsageResponse{}
	if rows, err := h.Queries.ListModelKeyUsage(r.Context(), wsUUID); err == nil {
		for _, u := range rows {
			usage = append(usage, ModelKeyUsageResponse{ModelKeyID: uuidToString(u.ModelKeyID), Provider: u.Provider, Model: u.Model, TaskCount: u.TaskCount, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens, CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens, CostUsdTicks: u.CostUsdTicks})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys, "usage": usage, "vendors": modelkey.Vendors, "configured": h.modelKeysConfigured()})
}

type createModelKeyRequest struct {
	Scope    string `json:"scope"`
	ScopeID  string `json:"scope_id"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Key      string `json:"key"`
	Priority int32  `json:"priority"`
	// Replace retires the active keys of the same scope and vendor (a
	// rotation); without it a second active key is a conflict.
	Replace bool `json:"replace"`
}

// POST /api/workspaces/{id}/model-keys
func (h *Handler) CreateModelKey(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireModelKeyWriter(w, r, workspaceID) {
		return
	}
	if !h.modelKeysConfigured() {
		writeError(w, http.StatusConflict, "model keys are not configured on this server (MULTICA_MODEL_KEY_SECRET_KEY)")
		return
	}
	var req createModelKeyRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	key, err := h.createModelKey(r.Context(), wsUUID, requestUserID(r), req)
	if err != nil {
		var conflict modelKeyConflict
		if errors.As(err, &conflict) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error(), "code": "model_key_active_conflict"})
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, modelKeyToResponse(key))
}

type modelKeyConflict struct{ vendor string }

func (c modelKeyConflict) Error() string {
	return "an active key already exists for " + c.vendor + " in this scope; rotate it instead"
}

func (h *Handler) createModelKey(ctx context.Context, wsUUID pgtype.UUID, userID string, req createModelKeyRequest) (db.WorkspaceModelKey, error) {
	vendor, ok := modelkey.VendorByID(strings.TrimSpace(req.Provider))
	if !ok {
		return db.WorkspaceModelKey{}, errors.New("unknown provider")
	}
	key := strings.TrimSpace(req.Key)
	if len(key) < 8 || len(key) > 4096 || strings.ContainsAny(key, " \n\t") {
		return db.WorkspaceModelKey{}, errors.New("the key looks malformed")
	}
	var scopeID pgtype.UUID
	switch req.Scope {
	case "workspace":
	case "project":
		id, err := util.ParseUUID(req.ScopeID)
		if err != nil {
			return db.WorkspaceModelKey{}, errors.New("scope_id must be a project id")
		}
		if _, err := h.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{ID: id, WorkspaceID: wsUUID}); err != nil {
			return db.WorkspaceModelKey{}, errors.New("project not found")
		}
		scopeID = id
	default:
		return db.WorkspaceModelKey{}, errors.New("scope must be workspace or project")
	}
	active, err := h.Queries.ListModelKeys(ctx, wsUUID)
	if err != nil {
		return db.WorkspaceModelKey{}, err
	}
	for _, k := range active {
		if k.Active && k.Provider == vendor.ID && k.Scope == req.Scope && k.ScopeID == scopeID {
			if !req.Replace {
				return db.WorkspaceModelKey{}, modelKeyConflict{vendor: vendor.Label}
			}
		}
	}
	if req.Replace {
		if err := h.Queries.DeactivateModelKeysForScope(ctx, db.DeactivateModelKeysForScopeParams{WorkspaceID: wsUUID, Provider: vendor.ID, Scope: req.Scope, ScopeID: scopeID, DeactivatedReason: "rotated"}); err != nil {
			return db.WorkspaceModelKey{}, err
		}
	}
	sealed, err := h.sealModelKey(key)
	if err != nil {
		return db.WorkspaceModelKey{}, err
	}
	row, err := h.Queries.CreateModelKey(ctx, db.CreateModelKeyParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, Scope: req.Scope, ScopeID: scopeID, Provider: vendor.ID, Label: truncate(strings.TrimSpace(req.Label), 100), KeyEncrypted: sealed, KeyHint: modelkey.Hint(key), Priority: req.Priority, CreatedBy: parseUUID(userID)})
	if err != nil {
		return db.WorkspaceModelKey{}, err
	}
	h.audit(ctx, wsUUID, "member", userID, AuditModelKeyCreated, "workspace_model_key", row.ID, map[string]any{"scope": row.Scope, "scope_id": uuidToPtr(row.ScopeID), "provider": row.Provider, "label": row.Label, "hint": row.KeyHint, "rotation": req.Replace}, nil)
	return row, nil
}

// POST /api/workspaces/{id}/model-keys/{keyId}/rotate — a new key for the
// same scope and vendor; the old one is retired, its usage kept.
func (h *Handler) RotateModelKey(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireModelKeyWriter(w, r, workspaceID) {
		return
	}
	keyUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "keyId"), "key id")
	if !ok {
		return
	}
	old, err := h.Queries.GetModelKey(r.Context(), db.GetModelKeyParams{ID: keyUUID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "model key not found")
		return
	}
	var req struct {
		Key   string `json:"key"`
		Label string `json:"label"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	label := old.Label
	if strings.TrimSpace(req.Label) != "" {
		label = req.Label
	}
	key, err := h.createModelKey(r.Context(), wsUUID, requestUserID(r), createModelKeyRequest{Scope: old.Scope, ScopeID: uuidToString(old.ScopeID), Provider: old.Provider, Label: label, Key: req.Key, Priority: old.Priority, Replace: true})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, modelKeyToResponse(key))
}

// DELETE /api/workspaces/{id}/model-keys/{keyId} — retire; rows are never
// removed while usage points at them.
func (h *Handler) DeactivateModelKey(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if !h.requireModelKeyWriter(w, r, workspaceID) {
		return
	}
	keyUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "keyId"), "key id")
	if !ok {
		return
	}
	rows, err := h.Queries.DeactivateModelKey(r.Context(), db.DeactivateModelKeyParams{ID: keyUUID, WorkspaceID: wsUUID, DeactivatedReason: "retired by " + requestUserID(r)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to retire the key")
		return
	}
	if rows == 0 {
		writeError(w, http.StatusNotFound, "no active key with this id")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditModelKeyRetired, "workspace_model_key", keyUUID, map[string]any{"reason": "retired"}, nil)
	writeJSON(w, http.StatusOK, map[string]any{"retired": true})
}

// resolveModelKeyForClaim injects the run's key into its environment: the
// active key of highest priority for the issue's project, else the
// workspace's, for the vendor the runtime spends. An agent that sets the
// variable itself keeps its own key. Returns the env unchanged when nothing
// applies.
func (h *Handler) resolveModelKeyForClaim(ctx context.Context, task db.AgentTaskQueue, runtimeProvider string, wsUUID pgtype.UUID, env map[string]string) map[string]string {
	if !h.modelKeysConfigured() {
		return env
	}
	vendorID := modelkey.VendorForRuntime(runtimeProvider)
	if vendorID == "" {
		return env
	}
	vendor, _ := modelkey.VendorByID(vendorID)
	if strings.TrimSpace(env[vendor.EnvVar]) != "" {
		return env
	}
	var projectID pgtype.UUID
	if task.IssueID.Valid {
		if issue, err := h.Queries.GetIssue(ctx, task.IssueID); err == nil {
			projectID = issue.ProjectID
		}
	}
	keys, err := h.Queries.ListActiveModelKeys(ctx, db.ListActiveModelKeysParams{WorkspaceID: wsUUID, Provider: vendorID, ProjectID: projectID})
	if err != nil || len(keys) == 0 {
		return env
	}
	plain, err := h.openModelKey(keys[0].KeyEncrypted)
	if err != nil {
		slog.Warn("model key: open failed", "key_id", uuidToString(keys[0].ID), "error", err)
		return env
	}
	if env == nil {
		env = map[string]string{}
	}
	env[vendor.EnvVar] = plain
	if err := h.Queries.SetTaskModelKey(ctx, db.SetTaskModelKeyParams{ID: task.ID, ModelKeyID: keys[0].ID}); err != nil {
		slog.Warn("model key: stamp task failed", "task_id", uuidToString(task.ID), "error", err)
	}
	return env
}

// modelKeyFailover (K48) runs when a run fails: a vendor authentication or
// quota failure retires the key the run spent, alerts the managers, and
// answers whether another key can take the retry. Wired into TaskService.
func (h *Handler) modelKeyFailover(ctx context.Context, task db.AgentTaskQueue, reason string) bool {
	if !task.ModelKeyID.Valid {
		return false
	}
	switch reason {
	case string(taskfailure.ReasonAgentProviderAuthOrAccess), string(taskfailure.ReasonAgentProviderQuotaLimit):
	default:
		return false
	}
	agent, err := h.Queries.GetAgent(ctx, task.AgentID)
	if err != nil {
		return false
	}
	key, err := h.Queries.GetModelKey(ctx, db.GetModelKeyParams{ID: task.ModelKeyID, WorkspaceID: agent.WorkspaceID})
	if err != nil {
		return false
	}
	rows, err := h.Queries.DeactivateModelKey(ctx, db.DeactivateModelKeyParams{ID: key.ID, WorkspaceID: key.WorkspaceID, DeactivatedReason: reason})
	if err != nil || rows == 0 {
		return false
	}
	h.audit(ctx, key.WorkspaceID, "system", "", AuditModelKeyRetired, "workspace_model_key", key.ID, map[string]any{"reason": reason, "task_id": uuidToString(task.ID), "hint": key.KeyHint}, nil)
	var projectID pgtype.UUID
	if task.IssueID.Valid {
		if issue, err := h.Queries.GetIssue(ctx, task.IssueID); err == nil {
			projectID = issue.ProjectID
		}
	}
	next, _ := h.Queries.ListActiveModelKeys(ctx, db.ListActiveModelKeysParams{WorkspaceID: key.WorkspaceID, Provider: key.Provider, ProjectID: projectID})
	vendor, _ := modelkey.VendorByID(key.Provider)
	body := fmt.Sprintf("Run %s failed with %s. The key %s (%s) was retired.", uuidToString(task.ID), reason, key.KeyHint, key.Label)
	if len(next) > 0 {
		body += fmt.Sprintf(" The run retries once on %s (%s).", next[0].KeyHint, next[0].Label)
	} else {
		body += " No other active key exists for this vendor: the run is not retried."
	}
	h.modelKeyAlert(ctx, key.WorkspaceID, task.IssueID, vendor.Label+" key retired after a run failure", body, map[string]any{"key_id": uuidToString(key.ID), "provider": key.Provider, "reason": reason, "task_id": uuidToString(task.ID), "failover": len(next) > 0})
	return len(next) > 0
}

// modelKeyAlert files one inbox item per workspace manager.
func (h *Handler) modelKeyAlert(ctx context.Context, wsID, issueID pgtype.UUID, title, body string, details map[string]any) {
	managers, err := h.Queries.ListWorkspaceManagerUserIDs(ctx, wsID)
	if err != nil {
		return
	}
	raw, _ := json.Marshal(details)
	for _, userID := range managers {
		item, err := h.Queries.CreateInboxItem(ctx, db.CreateInboxItemParams{
			ID: dbid.NewV7(), WorkspaceID: wsID, RecipientType: "member", RecipientID: userID, Type: InboxTypeModelKeyAlert, Severity: "action_required",
			IssueID: issueID, Title: truncate(title, 120), Body: pgtype.Text{String: truncate(body, 1000), Valid: true}, Details: raw,
		})
		if err != nil {
			if !errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("model key: inbox failed", "error", err)
			}
			continue
		}
		h.publish(protocol.EventInboxNew, uuidToString(wsID), "system", "", map[string]any{"item": inboxToResponse(item)})
	}
}
