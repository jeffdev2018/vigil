package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/triage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Email intake. A workspace hands out one endpoint whose bearer token in the
// URL path IS the credential, exactly like the autopilot webhook ingress: no
// session, no workspace header, and the workspace is derived from the source
// row the token resolves to. Unlike the webhook token, this one is stored as
// its sha256 digest (run_scoped_secret's rule): a database dump must not hand
// somebody a working inbox.
//
// The body is the small common denominator of the JSON inbound webhooks the
// email providers post, so a forwarding rule can be pointed here with a
// two-line transform rather than a parser:
//
//	Mailgun  "sender" / "subject" / "body-plain" / "body-html" / "Message-Id"
//	Postmark "From"   / "Subject" / "TextBody"   / "HtmlBody"  / "MessageID"
//	SendGrid "from"   / "subject" / "text"       / "html"      / "headers"
//
//	{"from": ..., "subject": ..., "text": ..., "html": ..., "message_id": ...}
//
// An inbound email is never an issue on arrival: it is the least
// authenticated material in the product, so it always lands in the queue and
// a human decides. That is why the source is created gated.
const (
	emailTokenPrefix = "mti_"
	// maxInboundEmailBytes caps one delivery. Email bodies are text here (the
	// HTML alternative is stored in the payload, not rendered), and a message
	// larger than this is a forwarded attachment chain, not a report.
	maxInboundEmailBytes = 256 * 1024
	// emailTitleMaxRunes keeps a runaway subject line out of the queue's
	// title column and out of the issue it may become.
	emailTitleMaxRunes = 200
	// inboundEmailPathPrefix is the public ingress path.
	inboundEmailPathPrefix = "/api/triage/inbound/email/"
)

// inboundEmailRequest is the accepted payload. `from` and one of
// `subject` / `text` are required: a message with no sender and no content is
// a misconfigured forwarder, and answering 202 to it would hide the mistake.
type inboundEmailRequest struct {
	From      string `json:"from"`
	Subject   string `json:"subject"`
	Text      string `json:"text"`
	HTML      string `json:"html"`
	MessageID string `json:"message_id"`
}

// TriageEmailSourceResponse is returned when the endpoint is created or
// rotated. Token is present exactly once, in that response: only its digest is
// stored, so a lost token is rotated, never recovered.
type TriageEmailSourceResponse struct {
	ID    string `json:"id"`
	Mode  string `json:"mode"`
	Path  string `json:"path"`
	URL   string `json:"url,omitempty"`
	Token string `json:"token"`
}

func newInboundEmailToken() (token, hash string, err error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	token = emailTokenPrefix + hex.EncodeToString(buf)
	return token, hashInboundEmailToken(token), nil
}

func hashInboundEmailToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func inboundEmailPathForToken(token string) string {
	return inboundEmailPathPrefix + token
}

// CreateTriageEmailSource mints the workspace's email intake endpoint, or
// rotates its token when one already exists. Rotation is the revocation
// mechanism: the previous token stops resolving the moment this returns.
func (h *Handler) CreateTriageEmailSource(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return
	}

	token, hash, err := newInboundEmailToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mint an intake token")
		return
	}
	src, err := h.Queries.SetTriageSourceToken(r.Context(), db.SetTriageSourceTokenParams{
		WorkspaceID: workspaceID,
		Kind:        triage.SourceEmail,
		// One inbox per workspace: the source is the workspace itself.
		RefID:       workspaceID,
		Name:        "Email intake",
		Mode:        string(triage.ModeGate),
		TokenHash:   hash,
		CreatedByID: safeParseUUID(userID),
	})
	if err != nil {
		slog.Error("triage email source create failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create the email intake source")
		return
	}

	resp := TriageEmailSourceResponse{
		ID:    util.UUIDToString(src.ID),
		Mode:  src.Mode,
		Path:  inboundEmailPathForToken(token),
		Token: token,
	}
	if h.cfg.PublicURL != "" {
		resp.URL = h.cfg.PublicURL + resp.Path
	}
	writeJSON(w, http.StatusCreated, resp)
}

// HandleInboundTriageEmail admits one forwarded email into the queue. It is
// outside the authenticated group on purpose: the token in the path is the
// credential and the workspace comes from the source row it resolves to.
func (h *Handler) HandleInboundTriageEmail(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	if token == "" {
		writeError(w, http.StatusNotFound, "intake endpoint not found")
		return
	}

	// Same two-tier limiter the autopilot webhook ingress uses: a high absolute
	// ceiling per IP, plus a tighter budget consumed only by bad credentials so
	// a NAT full of legitimate forwarders is not throttled by one attacker.
	ip := h.clientIPForRateLimit(r)
	if ip != "" && h.WebhookAbsoluteIPRateLimiter != nil && !h.WebhookAbsoluteIPRateLimiter.Allow(r.Context(), ip) {
		writeWebhookRateLimit(w, r, h.WebhookAbsoluteIPRateLimiter, ip, "absolute_ip", h.Metrics)
		return
	}
	if ip != "" && h.WebhookIPRateLimiter != nil && !slidingWindowLimiterCheck(r.Context(), h.WebhookIPRateLimiter, ip) {
		writeWebhookRateLimit(w, r, h.WebhookIPRateLimiter, ip, "bad_credential_ip", h.Metrics)
		return
	}

	source, err := h.Queries.GetTriageSourceByTokenHash(r.Context(), hashInboundEmailToken(token))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if ip != "" && h.WebhookIPRateLimiter != nil {
				h.WebhookIPRateLimiter.Allow(r.Context(), ip)
			}
			// Generic: never tell a caller which tokens existed.
			writeError(w, http.StatusNotFound, "intake endpoint not found")
			return
		}
		slog.Error("triage email intake: token lookup failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxInboundEmailBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "message too large")
		return
	}
	var req inboundEmailRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	title, body, ok := inboundEmailContent(req)
	if !ok {
		writeError(w, http.StatusBadRequest, "from is required, along with a subject or a text body")
		return
	}

	params := triage.CaptureParams{
		WorkspaceID:     source.WorkspaceID,
		SourceKind:      source.Kind,
		SourceRefID:     source.RefID,
		SourceName:      source.Name,
		SourceCreatedBy: source.CreatedByID,
		OriginType:      "email",
		Title:           title,
		BodyMarkdown:    body,
		TriggerPayload:  raw,
		State:           triage.StatePending,
	}
	if triage.Decide(source.Mode) == triage.RouteDrop {
		// A blocked inbox still records what it refused, and still answers 202:
		// a sender must not learn from the status code that it was blocked.
		params.State = triage.StateDropped
		params.DropReason = "source_blocked"
		h.captureTriageInbound(r.Context(), params)
		writeJSON(w, http.StatusAccepted, map[string]any{"status": "accepted"})
		return
	}
	if _, ok := h.captureTriageInbound(r.Context(), params); !ok {
		writeError(w, http.StatusInternalServerError, "failed to record the message")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "queued"})
}

// inboundEmailContent derives the queue entry's title and body. The subject is
// the title when there is one; otherwise the first line of the text stands in,
// because an item with no title is unreadable in the queue and un-mergeable by
// the collapse index.
func inboundEmailContent(req inboundEmailRequest) (title, body string, ok bool) {
	from := strings.TrimSpace(req.From)
	subject := strings.TrimSpace(req.Subject)
	text := strings.TrimSpace(req.Text)
	if from == "" || (subject == "" && text == "") {
		return "", "", false
	}
	title = subject
	if title == "" {
		title, _, _ = strings.Cut(text, "\n")
		title = strings.TrimSpace(title)
	}
	if n := []rune(title); len(n) > emailTitleMaxRunes {
		title = string(n[:emailTitleMaxRunes])
	}

	var b strings.Builder
	b.WriteString("**From:** ")
	b.WriteString(from)
	if text != "" {
		b.WriteString("\n\n")
		b.WriteString(text)
	}
	return title, b.String(), true
}
