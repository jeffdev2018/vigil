package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/go-chi/chi/v5"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/redact"
)

type CliAuthStatus string

const (
	CliAuthPending   CliAuthStatus = "pending"
	CliAuthRunning   CliAuthStatus = "running"
	CliAuthCompleted CliAuthStatus = "completed"
	CliAuthFailed    CliAuthStatus = "failed"
	CliAuthTimeout   CliAuthStatus = "timeout"

	cliAuthPendingTimeout = 30 * time.Second
	cliAuthRequestTTL     = 10 * time.Minute
	cliAuthStoreRetention = 12 * time.Minute
)

type CliAuthRequest struct {
	ID              string        `json:"id"`
	RuntimeID       string        `json:"runtime_id"`
	Action          string        `json:"action"`
	Status          CliAuthStatus `json:"status"`
	VerificationURL string        `json:"verification_url,omitempty"`
	UserCode        string        `json:"user_code,omitempty"`
	Authenticated   *bool         `json:"authenticated,omitempty"`
	Error           string        `json:"error,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	ExpiresAt       time.Time     `json:"expires_at"`
	RunStartedAt    *time.Time    `json:"-"`
}

type CliAuthStore interface {
	Create(ctx context.Context, runtimeID, action string) (*CliAuthRequest, error)
	Get(ctx context.Context, id string) (*CliAuthRequest, error)
	HasPending(ctx context.Context, runtimeID string) (bool, error)
	PopPending(ctx context.Context, runtimeID string) (*CliAuthRequest, error)
	Progress(ctx context.Context, id, verificationURL, userCode string) error
	Complete(ctx context.Context, id string, authenticated bool) error
	Fail(ctx context.Context, id, errMsg string) error
}

func cliAuthRequestTerminal(status CliAuthStatus) bool {
	return status == CliAuthCompleted || status == CliAuthFailed || status == CliAuthTimeout
}

func applyCliAuthTimeout(req *CliAuthRequest, now time.Time) bool {
	if cliAuthRequestTerminal(req.Status) {
		return false
	}
	if req.Status == CliAuthPending && now.Sub(req.CreatedAt) > cliAuthPendingTimeout {
		req.Status = CliAuthTimeout
		req.Error = "daemon did not pick up the authentication request within 30 seconds"
	} else if !now.Before(req.ExpiresAt) {
		req.Status = CliAuthTimeout
		req.Error = "authentication request expired before the CLI completed it"
	} else {
		return false
	}
	req.UpdatedAt = now
	return true
}

type InMemoryCliAuthStore struct {
	mu       sync.Mutex
	requests map[string]*CliAuthRequest
}

func NewInMemoryCliAuthStore() *InMemoryCliAuthStore {
	return &InMemoryCliAuthStore{requests: make(map[string]*CliAuthRequest)}
}

func (s *InMemoryCliAuthStore) Create(_ context.Context, runtimeID, action string) (*CliAuthRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, req := range s.requests {
		if now.Sub(req.CreatedAt) > cliAuthStoreRetention {
			delete(s.requests, id)
		}
	}
	req := &CliAuthRequest{
		ID: randomID(), RuntimeID: runtimeID, Action: action, Status: CliAuthPending,
		CreatedAt: now, UpdatedAt: now, ExpiresAt: now.Add(cliAuthRequestTTL),
	}
	s.requests[req.ID] = req
	return req, nil
}

func (s *InMemoryCliAuthStore) Get(_ context.Context, id string) (*CliAuthRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	req := s.requests[id]
	if req != nil {
		applyCliAuthTimeout(req, time.Now())
	}
	return req, nil
}

func (s *InMemoryCliAuthStore) HasPending(_ context.Context, runtimeID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for _, req := range s.requests {
		applyCliAuthTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == CliAuthPending {
			return true, nil
		}
	}
	return false, nil
}

func (s *InMemoryCliAuthStore) PopPending(_ context.Context, runtimeID string) (*CliAuthRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var oldest *CliAuthRequest
	now := time.Now()
	for _, req := range s.requests {
		applyCliAuthTimeout(req, now)
		if req.RuntimeID == runtimeID && req.Status == CliAuthPending && (oldest == nil || req.CreatedAt.Before(oldest.CreatedAt)) {
			oldest = req
		}
	}
	if oldest != nil {
		oldest.Status = CliAuthRunning
		oldest.RunStartedAt = &now
		oldest.UpdatedAt = now
	}
	return oldest, nil
}

func (s *InMemoryCliAuthStore) update(id string, fn func(*CliAuthRequest)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req := s.requests[id]; req != nil && !cliAuthRequestTerminal(req.Status) {
		fn(req)
		req.UpdatedAt = time.Now()
	}
	return nil
}

func (s *InMemoryCliAuthStore) Progress(_ context.Context, id, verificationURL, userCode string) error {
	return s.update(id, func(req *CliAuthRequest) {
		req.Status = CliAuthRunning
		if verificationURL != "" {
			req.VerificationURL = verificationURL
		}
		if userCode != "" {
			req.UserCode = userCode
		}
	})
}

func (s *InMemoryCliAuthStore) Complete(_ context.Context, id string, authenticated bool) error {
	return s.update(id, func(req *CliAuthRequest) {
		req.Status = CliAuthCompleted
		req.Authenticated = &authenticated
	})
}

func (s *InMemoryCliAuthStore) Fail(_ context.Context, id, errMsg string) error {
	return s.update(id, func(req *CliAuthRequest) {
		req.Status = CliAuthFailed
		req.Error = errMsg
	})
}

func cliAuthProviderSupported(provider string) bool {
	return provider == "claude" || provider == "codex"
}

func (h *Handler) InitiateCliAuth(w http.ResponseWriter, r *http.Request) {
	h.initiateCliAuthAction(w, r, "login")
}

func (h *Handler) InitiateCliLogout(w http.ResponseWriter, r *http.Request) {
	h.initiateCliAuthAction(w, r, "logout")
}

func (h *Handler) initiateCliAuthAction(w http.ResponseWriter, r *http.Request, action string) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, member, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if !ok {
		return
	}
	if !canEditRuntime(member, rt) {
		writeError(w, http.StatusForbidden, "only runtime owners and workspace admins can authenticate runtimes")
		return
	}
	if rt.Status != "online" {
		writeError(w, http.StatusServiceUnavailable, "runtime is offline")
		return
	}
	if !cliAuthProviderSupported(rt.Provider) {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("CLI authentication is not supported for provider %q", rt.Provider))
		return
	}
	resolvedRuntimeID := uuidToString(rt.ID)
	req, err := h.CliAuthStore.Create(r.Context(), resolvedRuntimeID, action)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to enqueue CLI authentication request")
		return
	}
	h.requestDaemonPendingWork(resolvedRuntimeID, protocol.PendingWorkKindCliAuth)
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) GetCliAuthRequest(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, _, ok := h.requireRuntimeReadAccess(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI, runtimeID)
	if !ok {
		return
	}
	req, err := h.CliAuthStore.Get(r.Context(), chi.URLParam(r, "requestId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load CLI authentication request")
		return
	}
	if req == nil || req.RuntimeID != uuidToString(rt.ID) {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func validCliAuthURL(raw string) bool {
	if raw == "" || len(raw) > 2048 {
		return false
	}
	u, err := url.ParseRequestURI(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func validCliAuthCode(code string) bool {
	return len(code) <= 128 && !strings.ContainsFunc(code, unicode.IsControl)
}

func (h *Handler) ReportCliAuthResult(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}
	requestID := chi.URLParam(r, "requestId")
	existing, err := h.CliAuthStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load CLI authentication request")
		return
	}
	if existing == nil || existing.RuntimeID != runtimeID {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if cliAuthRequestTerminal(existing.Status) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}

	var body struct {
		Status          string `json:"status"`
		VerificationURL string `json:"verification_url"`
		UserCode        string `json:"user_code"`
		Authenticated   *bool  `json:"authenticated"`
		Error           string `json:"error"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	body.VerificationURL = strings.TrimSpace(body.VerificationURL)
	body.UserCode = strings.TrimSpace(body.UserCode)
	if body.VerificationURL != "" && !validCliAuthURL(body.VerificationURL) {
		writeError(w, http.StatusBadRequest, "invalid verification URL")
		return
	}
	if !validCliAuthCode(body.UserCode) {
		writeError(w, http.StatusBadRequest, "invalid verification code")
		return
	}

	switch body.Status {
	case "running":
		err = h.CliAuthStore.Progress(r.Context(), requestID, body.VerificationURL, body.UserCode)
	case "completed":
		if body.Authenticated == nil {
			writeError(w, http.StatusBadRequest, "authenticated is required for a completed request")
			return
		}
		reason := "authenticated"
		if !*body.Authenticated {
			reason = "signed_out"
		}
		state, marshalErr := json.Marshal(map[string]any{
			"authenticated": *body.Authenticated,
			"checked_at":    time.Now().UTC().Format(time.RFC3339),
			"provider":      rt.Provider,
			"reason":        reason,
		})
		if marshalErr != nil {
			err = marshalErr
		} else {
			err = h.Queries.UpdateRuntimeCliAuthState(r.Context(), db.UpdateRuntimeCliAuthStateParams{ID: rt.ID, CliAuth: state})
		}
		if err == nil {
			err = h.CliAuthStore.Complete(r.Context(), requestID, *body.Authenticated)
		}
		if err == nil {
			h.publish(protocol.EventDaemonRegister, uuidToString(rt.WorkspaceID), "system", "", map[string]any{"action": "update"})
		}
	case "failed":
		message := strings.TrimSpace(redact.Text(body.Error))
		if message == "" {
			message = "CLI authentication failed"
		}
		if len(message) > 1000 {
			message = message[:1000]
		}
		err = h.CliAuthStore.Fail(r.Context(), requestID, message)
	default:
		writeError(w, http.StatusBadRequest, "invalid status")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist CLI authentication result")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
