package handler

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The public preview proxy (F12): ANY /preview/{code}/*
//
// This is the only route in the product where an unauthenticated request causes
// a call into somebody's laptop, so its shape is defensive on purpose.
//
//   - The code IS the credential. Unknown, revoked and expired all answer 404
//     with the same body: a 403 would confirm which codes exist, and the codes
//     are the only secret here.
//   - A stopped preview answers 410, not 404 — the reviewer had a working link
//     a minute ago and "gone" is the true answer, while "never existed" would
//     send them looking for a typo.
//   - Only an allow-listed set of headers crosses in either direction. The dev
//     server is somebody's unfinished app; whatever it sets for cookies, auth or
//     transport framing is not something to forward onto a public origin.
//   - The response is served with a locked-down CSP and X-Frame-Options set by
//     the proxy, replacing anything the app said, so a preview cannot be framed
//     into a phishing page on the API origin.
//
// No WebSocket. HMR does not survive this route, and that is stated in the
// product rather than half-implemented: a relay that upgraded connections would
// hold one socket per open tab against a laptop, for a convenience.

const (
	// previewRelayTimeout is how long the visitor waits for the daemon. Matches
	// the daemon's own fetch bound so both sides give up together.
	previewRelayTimeout = 30 * time.Second
	// previewRequestBodyLimit caps what a visitor can push through the relay.
	previewRequestBodyLimit = 2 << 20
	// previewWorkspaceRelayCap bounds concurrent relayed requests per
	// workspace. One reviewer with a page full of assets must not be able to
	// saturate the daemon's in-flight budget for the whole workspace, and a
	// crawler must not be able to at all.
	previewWorkspaceRelayCap = 16
)

// previewRequestHeaderAllowList is what travels from the visitor to the dev
// server. Deliberately short: everything a browser needs to render a page, and
// nothing that carries identity. Cookie and Authorization are absent on
// purpose — the share code is the only credential in this flow, and forwarding
// a visitor's cookies for the API origin into an app the reviewer is inspecting
// would leak them.
var previewRequestHeaderAllowList = map[string]bool{
	"accept":            true,
	"accept-language":   true,
	"content-type":      true,
	"range":             true,
	"user-agent":        true,
	"x-requested-with":  true,
	"if-none-match":     true,
	"if-modified-since": true,
}

// previewResponseHeaderAllowList is what travels back. Set-Cookie is absent for
// the same reason: a dev server's session cookie would be set on the API
// origin, where it is both useless and dangerous.
var previewResponseHeaderAllowList = map[string]bool{
	"content-type":     true,
	"content-language": true,
	"cache-control":    true,
	"etag":             true,
	"last-modified":    true,
	"content-range":    true,
	"accept-ranges":    true,
	"location":         true,
	"vary":             true,
}

// PreviewProxy serves a relayed preview request.
func (h *Handler) PreviewProxy(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	if code == "" {
		previewNotFound(w)
		return
	}
	link, err := h.Queries.GetTaskShareLinkByCode(r.Context(), code)
	if err != nil {
		if !isNotFound(err) {
			slog.Warn("preview proxy: link lookup failed", "error", err)
		}
		previewNotFound(w)
		return
	}
	// Same answer for revoked, expired, and wrong-capability: none of them tell
	// the holder anything they are entitled to know.
	if !shareLinkUsable(link, time.Now()) || !shareLinkHas(link, shareCapabilityPreview) {
		previewNotFound(w)
		return
	}

	preview, err := h.Queries.GetRunPreviewByTask(r.Context(), link.TaskID)
	if err != nil {
		if isNotFound(err) {
			// A valid link to a run that never started a preview. The link is
			// real, so this is not a 404 about the link — it is a 410 about the
			// thing it points at.
			previewGone(w, "this run has no preview")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to load the preview")
		return
	}
	switch preview.Status {
	case previewStatusReady:
	case previewStatusStopped, previewStatusStale, previewStatusError:
		previewGone(w, "the preview for this run has ended")
		return
	default:
		previewGone(w, "the preview for this run is not ready")
		return
	}
	if preview.Scheme != previewSchemeRelay {
		previewGone(w, "this preview is local to the machine that ran it")
		return
	}
	if h.DaemonHub == nil || !previewRelayEnabled() {
		previewGone(w, "previews are not relayed by this deployment")
		return
	}

	release, ok := previewRelaySlots.acquire(uuidToString(preview.WorkspaceID), previewWorkspaceRelayCap)
	if !ok {
		w.Header().Set("Retry-After", "2")
		writeError(w, http.StatusTooManyRequests, "too many preview requests in flight for this workspace")
		return
	}
	defer release()

	body, err := io.ReadAll(io.LimitReader(r.Body, previewRequestBodyLimit+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request body")
		return
	}
	if len(body) > previewRequestBodyLimit {
		writeError(w, http.StatusRequestEntityTooLarge, "request body over the preview limit")
		return
	}

	reqPayload := protocol.PreviewFetchRequest{
		TaskID:  uuidToString(preview.TaskID),
		Method:  r.Method,
		Path:    previewTargetPath(r),
		Query:   r.URL.RawQuery,
		Headers: filterHeaders(r.Header, previewRequestHeaderAllowList),
	}
	if len(body) > 0 {
		reqPayload.Body = base64.StdEncoding.EncodeToString(body)
	}
	raw, err := json.Marshal(reqPayload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to build the relay request")
		return
	}

	_, respRaw, err := h.DaemonHub.CallRuntime(r.Context(), uuidToString(preview.RuntimeID),
		"preview.fetch", raw, previewRelayTimeout)
	if err != nil {
		switch {
		case errors.Is(err, daemonws.ErrServerRPCBusy):
			w.Header().Set("Retry-After", "2")
			writeError(w, http.StatusTooManyRequests, "the machine hosting this preview is busy")
		default:
			// Not connected, timed out, or the daemon refused: from the
			// visitor's side these are one thing — the upstream did not answer.
			writeError(w, http.StatusBadGateway, "the machine hosting this preview did not answer")
		}
		return
	}
	var fetched protocol.PreviewFetchResponse
	if err := json.Unmarshal(respRaw, &fetched); err != nil {
		writeError(w, http.StatusBadGateway, "the machine hosting this preview sent an unreadable response")
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(fetched.Body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "the machine hosting this preview sent an unreadable body")
		return
	}

	// Usage accounting is best-effort: a failed bump must never cost the
	// visitor their response.
	if err := h.Queries.TouchTaskShareLink(r.Context(), link.ID); err != nil {
		slog.Debug("preview proxy: could not record the link use", "error", err)
	}

	for name, values := range filterHeaders(httpHeaderFrom(fetched.Headers), previewResponseHeaderAllowList) {
		for _, v := range values {
			w.Header().Add(name, v)
		}
	}
	// Set by the proxy, after the allow-list, so the app cannot relax them.
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	w.Header().Set("X-Frame-Options", "DENY")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	// A preview is a moving target; caching it on a shared origin would serve
	// one reviewer's build to the next.
	w.Header().Set("Cache-Control", "no-store")
	if fetched.Truncated {
		w.Header().Set("X-Multica-Preview-Truncated", "1")
	}
	status := fetched.Status
	if status <= 0 {
		status = http.StatusBadGateway
	}
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		w.Write(decoded)
	}
}

// previewTargetPath is the path inside the preview: everything after
// /preview/{code}. Chi's wildcard gives it directly.
func previewTargetPath(r *http.Request) string {
	rest := chi.URLParam(r, "*")
	if rest == "" {
		return "/"
	}
	if !strings.HasPrefix(rest, "/") {
		rest = "/" + rest
	}
	return rest
}

func filterHeaders(in http.Header, allow map[string]bool) map[string][]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]string, len(in))
	for name, values := range in {
		if !allow[strings.ToLower(name)] {
			continue
		}
		out[name] = append([]string(nil), values...)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func httpHeaderFrom(m map[string][]string) http.Header {
	h := make(http.Header, len(m))
	for k, v := range m {
		h[http.CanonicalHeaderKey(k)] = v
	}
	return h
}

// previewNotFound is the one answer for every "you may not have this" case, so
// the response cannot be used to enumerate codes.
func previewNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, "not found")
}

func previewGone(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusGone, msg)
}

// previewRelayLimiter counts in-flight relayed requests per workspace. A
// counter rather than a semaphore channel: a request that finds the cap reached
// must be REFUSED (429), not queued behind sixteen others until the visitor's
// browser gives up.
//
// ponytail: per-process. A multi-instance deployment gets the cap per instance;
// move it to Redis alongside the other shared limiters if that stops being
// close enough.
//
// Package-level rather than a Handler field: the Handler is copied by value in
// a dozen places, and a per-copy counter would cap nothing.
var previewRelaySlots previewRelayLimiter

type previewRelayLimiter struct {
	mu       sync.Mutex
	inFlight map[string]int
}

func (l *previewRelayLimiter) acquire(workspaceID string, cap int) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.inFlight == nil {
		l.inFlight = make(map[string]int)
	}
	if l.inFlight[workspaceID] >= cap {
		return nil, false
	}
	l.inFlight[workspaceID]++
	return func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.inFlight[workspaceID] <= 1 {
			delete(l.inFlight, workspaceID)
			return
		}
		l.inFlight[workspaceID]--
	}, true
}
