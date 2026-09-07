package handler

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Run previews (F12).
//
// A run in worktree mode can start a dev server; this file is everything the
// server does about it — take the daemon's declaration, tell members where it
// is, and mint the scoped links that let a reviewer open it from a phone.
//
// The URL a member gets depends on ONE fact: whether the server can relay. It
// can when the daemon advertises run-preview-v1 and the operator did not turn
// the relay off, and then the link is a public /preview/{code}/ URL anyone with
// the code can open. It cannot otherwise, and then the address is
// http://127.0.0.1:<port>, which is honest — it works on the machine that ran
// the task and nowhere else. There is no third answer, and in particular never
// a relay URL that would 502: a dead link is worse than a stated limitation.

const (
	// EventRunPreviewUpdated tells every client in the workspace that a run's
	// preview changed state. Workspace-scoped and payload-thin on purpose: the
	// preview is read back from its endpoint, so a client that missed an event
	// catches up by re-reading rather than reconstructing state.
	EventRunPreviewUpdated = "run_preview:updated"

	previewStatusStarting = "starting"
	previewStatusReady    = "ready"
	previewStatusStale    = "stale"
	previewStatusStopped  = "stopped"
	previewStatusError    = "error"

	previewSchemeRelay    = "relay"
	previewSchemeLoopback = "loopback"

	shareCapabilityPreview = "preview"
	shareCapabilityView    = "view"
	shareCapabilitySteer   = "steer"

	// shareLinkDefaultHours / shareLinkMaxHours bound how long a link lives. A
	// preview dies with its run anyway, so the ceiling is about the LINK: a
	// code that outlives the reviewer's interest in it is a credential nobody
	// remembers issuing.
	shareLinkDefaultHours = 24
	shareLinkMaxHours     = 24 * 30

	// previewErrorMax bounds the daemon-supplied error text. It carries the
	// tail of the run script's log, which is what says why nothing answered.
	previewErrorMax = 8 << 10
)

// runPreviewResponse is what a member reads. `url` is absent when there is
// nothing to open — a stopped, stale or failed preview — because a link that
// cannot work is the one thing this endpoint must not produce.
type runPreviewResponse struct {
	Status         string  `json:"status"`
	Scheme         string  `json:"scheme"`
	URL            string  `json:"url,omitempty"`
	Port           int     `json:"port"`
	ExpiresAt      *string `json:"expires_at,omitempty"`
	Error          string  `json:"error,omitempty"`
	RelayAvailable bool    `json:"relay_available"`
}

type taskShareLinkResponse struct {
	ID           string   `json:"id"`
	Code         string   `json:"code"`
	URL          string   `json:"url"`
	Capabilities []string `json:"capabilities"`
	ExpiresAt    string   `json:"expires_at"`
	CreatedAt    string   `json:"created_at"`
	UseCount     int64    `json:"use_count"`
}

// previewRelayEnabled reports whether this deployment relays at all.
// MULTICA_PREVIEW_RELAY=off is the self-hosted escape hatch: an operator who
// does not want their API server proxying arbitrary bytes out of employee
// laptops turns it off, and every preview becomes loopback.
func previewRelayEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv("MULTICA_PREVIEW_RELAY")), "off")
}

// relayAvailableForRuntime is the whole relay decision: the operator allows it,
// a connection serves this runtime, and that connection's daemon says it can
// answer preview.fetch.
func (h *Handler) relayAvailableForRuntime(runtimeID string) bool {
	if !previewRelayEnabled() || h.DaemonHub == nil {
		return false
	}
	caps := h.DaemonHub.RuntimeCapabilities(runtimeID)
	for _, c := range strings.Split(caps, ",") {
		if strings.TrimSpace(c) == protocol.DaemonCapabilityRunPreviewV1 {
			return true
		}
	}
	return false
}

// ReportRunPreview: POST /api/daemon/tasks/{taskId}/preview
//
// The daemon declares what it found on the run's port. The scheme it asks for
// is a request, not a decision: the server downgrades relay→loopback whenever
// it cannot actually relay, because the daemon does not know whether the
// operator turned the relay off.
func (h *Handler) ReportRunPreview(w http.ResponseWriter, r *http.Request) {
	task, wsID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	var payload struct {
		Port       int    `json:"port"`
		Scheme     string `json:"scheme"`
		Status     string `json:"status"`
		HealthPath string `json:"health_path"`
		Error      string `json:"error"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if payload.Port <= 0 || payload.Port > 65535 {
		writeError(w, http.StatusBadRequest, "port must be a TCP port")
		return
	}
	status := strings.TrimSpace(payload.Status)
	switch status {
	case previewStatusStarting, previewStatusReady, previewStatusStopped, previewStatusError:
	default:
		writeError(w, http.StatusBadRequest, "unknown preview status")
		return
	}
	if !task.RuntimeID.Valid {
		writeError(w, http.StatusConflict, "this run has no runtime")
		return
	}

	scheme := previewSchemeLoopback
	if strings.TrimSpace(payload.Scheme) == previewSchemeRelay &&
		h.relayAvailableForRuntime(uuidToString(task.RuntimeID)) {
		scheme = previewSchemeRelay
	}
	healthPath := strings.TrimSpace(payload.HealthPath)
	if healthPath == "" {
		healthPath = "/"
	}
	errText := truncate(strings.TrimSpace(payload.Error), previewErrorMax)

	row, err := h.Queries.UpsertRunPreview(r.Context(), db.UpsertRunPreviewParams{
		WorkspaceID: parseUUID(wsID),
		TaskID:      task.ID,
		RuntimeID:   task.RuntimeID,
		Port:        int32(payload.Port),
		Scheme:      scheme,
		Status:      status,
		HealthPath:  healthPath,
		Error:       nullText(errText),
	})
	if err != nil {
		slog.Warn("run preview: upsert failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to record the preview")
		return
	}
	h.publishRunPreview(row)
	writeJSON(w, http.StatusOK, map[string]any{"status": row.Status, "scheme": row.Scheme})
}

// StopRunPreview: DELETE /api/daemon/tasks/{taskId}/preview
//
// The row is kept rather than deleted so an open browser tab is told the
// preview ended (410) instead of being told the link never existed (404).
func (h *Handler) StopRunPreview(w http.ResponseWriter, r *http.Request) {
	task, _, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	row, err := h.Queries.StopRunPreview(r.Context(), task.ID)
	if err != nil {
		if isNotFound(err) {
			// Nothing to stop. Not an error: the daemon stops previews on every
			// exit path, including the ones that never declared one.
			w.WriteHeader(http.StatusNoContent)
			return
		}
		slog.Warn("run preview: stop failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to stop the preview")
		return
	}
	h.publishRunPreview(row)
	w.WriteHeader(http.StatusNoContent)
}

// GetRunPreview: GET /api/tasks/{taskId}/preview
func (h *Handler) GetRunPreview(w http.ResponseWriter, r *http.Request) {
	task, _, ok := h.runReplayTask(w, r)
	if !ok {
		return
	}
	row, err := h.Queries.GetRunPreviewByTask(r.Context(), task.ID)
	if err != nil {
		if isNotFound(err) {
			// No preview is a normal state, not a missing resource: most runs
			// declare no `run` script. A 404 here would make every run detail
			// panel render an error.
			writeJSON(w, http.StatusOK, runPreviewResponse{Status: "", Scheme: "", RelayAvailable: previewRelayEnabled()})
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load the preview")
		return
	}
	writeJSON(w, http.StatusOK, h.renderRunPreview(r.Context(), row))
}

func (h *Handler) renderRunPreview(ctx context.Context, row db.RunPreview) runPreviewResponse {
	resp := runPreviewResponse{
		Status:         row.Status,
		Scheme:         row.Scheme,
		Port:           int(row.Port),
		Error:          row.Error.String,
		RelayAvailable: h.relayAvailableForRuntime(uuidToString(row.RuntimeID)),
	}
	// A URL only for a preview that can actually be opened right now. `ready`
	// is checked explicitly rather than "not stopped": starting, stale and
	// error are all states where a link would fail in the reviewer's hands.
	if row.Status != previewStatusReady {
		return resp
	}
	if row.Scheme == previewSchemeLoopback {
		resp.URL = "http://127.0.0.1:" + strconv.Itoa(int(row.Port))
		return resp
	}
	link, err := h.activePreviewShareLink(ctx, row.TaskID)
	if err != nil || link == nil {
		// Relay with no live link: the member has not shared it yet. No URL,
		// which is what makes the "create a link" affordance the next step.
		return resp
	}
	resp.URL = h.previewURL(link.Code)
	if link.ExpiresAt.Valid {
		at := link.ExpiresAt.Time.UTC().Format(time.RFC3339)
		resp.ExpiresAt = &at
	}
	return resp
}

// activePreviewShareLink is the newest unrevoked, unexpired link carrying the
// `preview` capability, or nil.
func (h *Handler) activePreviewShareLink(ctx context.Context, taskID pgtype.UUID) (*db.TaskShareLink, error) {
	rows, err := h.Queries.ListTaskShareLinks(ctx, taskID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for i := range rows {
		if !shareLinkUsable(rows[i], now) || !shareLinkHas(rows[i], shareCapabilityPreview) {
			continue
		}
		return &rows[i], nil
	}
	return nil, nil
}

func shareLinkUsable(link db.TaskShareLink, now time.Time) bool {
	if link.RevokedAt.Valid {
		return false
	}
	return link.ExpiresAt.Valid && link.ExpiresAt.Time.After(now)
}

func shareLinkHas(link db.TaskShareLink, capability string) bool {
	for _, c := range link.Capabilities {
		if c == capability {
			return true
		}
	}
	return false
}

// previewURL is the absolute address of a relayed preview. It is served by the
// API, not the app, so it is built from PublicURL — a link built on the app
// origin would 404 on a split deployment.
func (h *Handler) previewURL(code string) string {
	base := strings.TrimRight(h.cfg.PublicURL, "/")
	if base == "" {
		base = strings.TrimRight(h.cfg.AppURL, "/")
	}
	return base + "/preview/" + code + "/"
}

// CreateTaskShareLink: POST /api/tasks/{taskId}/share-links
func (h *Handler) CreateTaskShareLink(w http.ResponseWriter, r *http.Request) {
	task, wsID, ok := h.runReplayTask(w, r)
	if !ok {
		return
	}
	var payload struct {
		Capabilities   []string `json:"capabilities"`
		ExpiresInHours int      `json:"expires_in_hours"`
	}
	if r.Body != nil {
		// An empty body is a valid request for the default link.
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&payload)
	}
	caps, err := normalizeShareCapabilities(payload.Capabilities)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	hours := payload.ExpiresInHours
	if hours <= 0 {
		hours = shareLinkDefaultHours
	}
	if hours > shareLinkMaxHours {
		writeError(w, http.StatusUnprocessableEntity, "expires_in_hours is above the maximum of "+strconv.Itoa(shareLinkMaxHours))
		return
	}
	code, err := newShareCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mint a code")
		return
	}
	var createdBy pgtype.UUID
	if userID := requestUserID(r); userID != "" {
		if parsed, parseErr := util.ParseUUID(userID); parseErr == nil {
			createdBy = parsed
		}
	}
	row, err := h.Queries.CreateTaskShareLink(r.Context(), db.CreateTaskShareLinkParams{
		WorkspaceID:  wsID,
		TaskID:       task.ID,
		Code:         code,
		Capabilities: caps,
		CreatedBy:    createdBy,
		ExpiresAt:    pgtype.Timestamptz{Time: time.Now().Add(time.Duration(hours) * time.Hour), Valid: true},
	})
	if err != nil {
		slog.Warn("task share link: create failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create the link")
		return
	}
	writeJSON(w, http.StatusCreated, h.renderShareLink(row))
}

// ListTaskShareLinks: GET /api/tasks/{taskId}/share-links
func (h *Handler) ListTaskShareLinks(w http.ResponseWriter, r *http.Request) {
	task, _, ok := h.runReplayTask(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListTaskShareLinks(r.Context(), task.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load the links")
		return
	}
	out := make([]taskShareLinkResponse, 0, len(rows))
	for _, row := range rows {
		out = append(out, h.renderShareLink(row))
	}
	writeJSON(w, http.StatusOK, map[string]any{"links": out})
}

// RevokeTaskShareLink: DELETE /api/tasks/{taskId}/share-links/{id}
//
// Revocation is immediate for everyone: the proxy resolves the code on every
// single request, so there is no cached grant to expire.
func (h *Handler) RevokeTaskShareLink(w http.ResponseWriter, r *http.Request) {
	_, wsID, ok := h.runReplayTask(w, r)
	if !ok {
		return
	}
	linkID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "id")
	if !ok {
		return
	}
	if _, err := h.Queries.RevokeTaskShareLink(r.Context(), db.RevokeTaskShareLinkParams{
		ID: linkID, WorkspaceID: wsID,
	}); err != nil {
		if isNotFound(err) {
			writeError(w, http.StatusNotFound, "link not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to revoke the link")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) renderShareLink(row db.TaskShareLink) taskShareLinkResponse {
	out := taskShareLinkResponse{
		ID:           uuidToString(row.ID),
		Code:         row.Code,
		URL:          h.previewURL(row.Code),
		Capabilities: row.Capabilities,
		UseCount:     row.UseCount,
	}
	if row.ExpiresAt.Valid {
		out.ExpiresAt = row.ExpiresAt.Time.UTC().Format(time.RFC3339)
	}
	if row.CreatedAt.Valid {
		out.CreatedAt = row.CreatedAt.Time.UTC().Format(time.RFC3339)
	}
	return out
}

// normalizeShareCapabilities validates the requested grant. An unknown word is
// refused rather than dropped: silently narrowing a grant would hand the caller
// a link that does less than it asked for, with nothing saying so.
func normalizeShareCapabilities(raw []string) ([]string, error) {
	if len(raw) == 0 {
		return []string{shareCapabilityPreview}, nil
	}
	seen := make(map[string]bool, len(raw))
	out := make([]string, 0, len(raw))
	for _, c := range raw {
		c = strings.TrimSpace(c)
		switch c {
		case shareCapabilityPreview, shareCapabilityView, shareCapabilitySteer:
		default:
			return nil, errUnknownCapability(c)
		}
		if seen[c] {
			continue
		}
		seen[c] = true
		out = append(out, c)
	}
	return out, nil
}

type unknownCapabilityError struct{ value string }

func (e unknownCapabilityError) Error() string {
	return "unknown capability " + strconv.Quote(e.value)
}

func errUnknownCapability(v string) error { return unknownCapabilityError{value: v} }

// newShareCode mints the credential. 20 random bytes, unpadded base32: 160 bits
// is far past guessing, and base32 survives being read aloud, copied out of a
// chat client, and typed on a phone — which is where these links are opened.
func newShareCode() (string, error) {
	buf := make([]byte, 20)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)), nil
}

func (h *Handler) publishRunPreview(row db.RunPreview) {
	h.publish(EventRunPreviewUpdated, uuidToString(row.WorkspaceID), "system", "", map[string]any{
		"task_id": uuidToString(row.TaskID),
		"status":  row.Status,
	})
}

// SweepStaleRunPreviews moves previews of a gone daemon to `stale`.
//
// The signal is the runtime heartbeat freshness the run sweeper already uses,
// not a preview-specific clock: a preview and the run that owns it must agree
// on when the machine left, or the UI shows a live preview attached to a failed
// run. Returns how many rows moved.
func (h *Handler) SweepStaleRunPreviews(ctx context.Context) (int, error) {
	n, err := h.Queries.MarkStaleRunPreviewsForGoneDaemons(ctx, runPreviewStaleAfterSeconds)
	return int(n), err
}

// runPreviewStaleAfterSeconds IS the runtime liveness window the run sweeper
// uses, not a copy of it. A preview and the run that owns it must agree on when
// the machine left, or the UI shows a live preview attached to a failed run.
const runPreviewStaleAfterSeconds = service.RuntimeClaimFreshnessSeconds

func nullText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
