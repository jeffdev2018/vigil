package handler

// Brain capture (OS plan, vague B): "capture first, organize later". Anything
// worth keeping — a line of text, a link, a photo, a voice memo, a file —
// lands in the workspace's capture inbox in one gesture, from the web, the
// desktop, the phone, the CLI, an MCP client, a chat bot or an agent. A
// suggestion (title, tags, summary, the existing note it may belong to) is
// prepared when a model is configured; a person then turns the capture into
// a note, merges it into one, or discards it. Nothing is organized behind
// anyone's back.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	brainCaptureMaxContentRunes = 20000
	brainCaptureMaxTitleRunes   = 200
	brainCaptureMaxUpload       = 50 << 20
	brainCaptureListLimit       = 200
	brainCaptureSuggestMaxNotes = 5

	BrainCaptureStatusRaw       = "raw"
	BrainCaptureStatusOrganized = "organized"
	BrainCaptureStatusDiscarded = "discarded"

	AuditBrainCaptured  = "brain.captured"
	AuditBrainOrganized = "brain.organized"
)

var (
	brainCaptureKinds   = map[string]bool{"text": true, "link": true, "image": true, "audio": true, "file": true, "todo": true}
	brainCaptureOrigins = map[string]bool{"web": true, "desktop": true, "mobile": true, "cli": true, "mcp": true, "channel": true, "agent": true, "api": true}
)

// BrainCaptureSuggestion is what the model proposes for a raw capture.
type BrainCaptureSuggestion struct {
	Title      string                    `json:"title"`
	Tags       []string                  `json:"tags"`
	Summary    string                    `json:"summary"`
	Action     string                    `json:"action"` // note | merge | discard
	MergeNote  *BrainCaptureMergeTarget  `json:"merge_note,omitempty"`
	Candidates []BrainCaptureMergeTarget `json:"candidates"`
	Reason     string                    `json:"reason"`
	Model      string                    `json:"model,omitempty"`
}

type BrainCaptureMergeTarget struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type BrainCaptureResponse struct {
	ID                  string                  `json:"id"`
	WorkspaceID         string                  `json:"workspace_id"`
	Kind                string                  `json:"kind"`
	Content             string                  `json:"content"`
	URL                 string                  `json:"url"`
	TitleHint           string                  `json:"title_hint"`
	Attachment          *AttachmentResponse     `json:"attachment"`
	Origin              string                  `json:"origin"`
	Status              string                  `json:"status"`
	TranscriptionStatus string                  `json:"transcription_status"`
	Suggestion          *BrainCaptureSuggestion `json:"suggestion"`
	NoteID              *string                 `json:"note_id"`
	CreatedByType       string                  `json:"created_by_type"`
	CreatedByID         *string                 `json:"created_by_id"`
	SourceTaskID        *string                 `json:"source_task_id"`
	OrganizedBy         *string                 `json:"organized_by"`
	OrganizedAt         *string                 `json:"organized_at"`
	CreatedAt           string                  `json:"created_at"`
	UpdatedAt           string                  `json:"updated_at"`
}

func (h *Handler) brainCaptureToResponse(ctx context.Context, c db.BrainCapture, mode attachmentURLMode) BrainCaptureResponse {
	out := BrainCaptureResponse{
		ID: uuidToString(c.ID), WorkspaceID: uuidToString(c.WorkspaceID), Kind: c.Kind, Content: c.Content, URL: c.Url, TitleHint: c.TitleHint,
		Origin: c.Origin, Status: c.Status, TranscriptionStatus: c.TranscriptionStatus, NoteID: uuidToPtr(c.NoteID),
		CreatedByType: c.CreatedByType, CreatedByID: uuidToPtr(c.CreatedByID), SourceTaskID: uuidToPtr(c.SourceTaskID), OrganizedBy: uuidToPtr(c.OrganizedBy), OrganizedAt: timestampToPtr(c.OrganizedAt),
		CreatedAt: timestampToString(c.CreatedAt), UpdatedAt: timestampToString(c.UpdatedAt),
	}
	if len(c.Suggestion) > 0 {
		var s BrainCaptureSuggestion
		if json.Unmarshal(c.Suggestion, &s) == nil {
			if s.Tags == nil {
				s.Tags = []string{}
			}
			if s.Candidates == nil {
				s.Candidates = []BrainCaptureMergeTarget{}
			}
			out.Suggestion = &s
		}
	}
	if c.AttachmentID.Valid {
		if att, err := h.Queries.GetAttachment(ctx, db.GetAttachmentParams{ID: c.AttachmentID, WorkspaceID: c.WorkspaceID}); err == nil {
			resp := h.attachmentToResponse(att, mode)
			out.Attachment = &resp
		}
	}
	return out
}

func (h *Handler) loadBrainWorkspace(w http.ResponseWriter, r *http.Request) (pgtype.UUID, pgtype.UUID, bool) {
	wsUUID, ok := parseUUIDOrBadRequest(w, h.resolveWorkspaceID(r), "workspace_id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	userID, ok := requireUserID(w, r)
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	userUUID, ok := parseUUIDOrBadRequest(w, userID, "user id")
	if !ok {
		return pgtype.UUID{}, pgtype.UUID{}, false
	}
	return wsUUID, userUUID, true
}

func brainCaptureOrigin(r *http.Request, requested, actorType string) string {
	if actorType == "agent" {
		return "agent"
	}
	if brainCaptureOrigins[requested] && requested != "agent" {
		return requested
	}
	return "web"
}

func inferBrainCaptureKind(kind, content, rawURL string) string {
	if brainCaptureKinds[kind] {
		return kind
	}
	if rawURL != "" && strings.TrimSpace(content) == "" {
		return "link"
	}
	return "text"
}

func validateBrainCaptureURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", true
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(raw) > 2048 {
		return "", false
	}
	return raw, true
}

// POST /api/brain/captures {kind?, content?, url?, title_hint?, origin?}
func (h *Handler) CreateBrainCapture(w http.ResponseWriter, r *http.Request) {
	wsUUID, userUUID, ok := h.loadBrainWorkspace(w, r)
	if !ok {
		return
	}
	var req struct {
		Kind      string `json:"kind"`
		Content   string `json:"content"`
		URL       string `json:"url"`
		TitleHint string `json:"title_hint"`
		Origin    string `json:"origin"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	content := strings.TrimSpace(util.SanitizeTextForPostgres(req.Content))
	rawURL, ok := validateBrainCaptureURL(req.URL)
	if !ok {
		writeError(w, http.StatusBadRequest, "url must be an http(s) URL")
		return
	}
	if content == "" && rawURL == "" {
		writeError(w, http.StatusBadRequest, "content or url is required")
		return
	}
	if utf8.RuneCountInString(content) > brainCaptureMaxContentRunes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("content is limited to %d characters", brainCaptureMaxContentRunes))
		return
	}
	hint := strings.TrimSpace(util.SanitizeTextForPostgres(req.TitleHint))
	if utf8.RuneCountInString(hint) > brainCaptureMaxTitleRunes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("title_hint is limited to %d characters", brainCaptureMaxTitleRunes))
		return
	}
	actorType, actorID, taskID := h.noteActor(r, uuidToString(userUUID), uuidToString(wsUUID))
	kind := inferBrainCaptureKind(req.Kind, content, rawURL)
	if kind == "image" || kind == "audio" || kind == "file" {
		writeError(w, http.StatusBadRequest, "image, audio and file captures go through /api/brain/captures/upload")
		return
	}
	capture, err := h.Queries.CreateBrainCapture(r.Context(), db.CreateBrainCaptureParams{
		ID: dbid.NewV7(), WorkspaceID: wsUUID, Kind: kind, Content: content, Url: rawURL, TitleHint: hint, Origin: brainCaptureOrigin(r, req.Origin, actorType), TranscriptionStatus: "none",
		CreatedByType: actorType, CreatedByID: actorID, SourceTaskID: taskID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to capture")
		return
	}
	h.afterBrainCapture(r.Context(), capture, actorType, uuidToString(actorID))
	writeJSON(w, http.StatusCreated, struct {
		Capture BrainCaptureResponse `json:"capture"`
	}{h.brainCaptureToResponse(r.Context(), capture, attachmentURLModeFromRequest(r))})
}

// afterBrainCapture audits, publishes and starts the suggestion in the
// background.
func (h *Handler) afterBrainCapture(ctx context.Context, c db.BrainCapture, actorType, actorID string) {
	h.audit(ctx, c.WorkspaceID, actorType, actorID, AuditBrainCaptured, "brain_capture", c.ID, map[string]any{"kind": c.Kind, "origin": c.Origin, "bytes": len(c.Content)}, nil)
	h.publishBrainCapture(c, actorType, actorID, "captured")
	if c.TranscriptionStatus != "pending" {
		h.suggestBrainCaptureAsync(c)
	}
}

func (h *Handler) publishBrainCapture(c db.BrainCapture, actorType, actorID, change string) {
	h.publish(protocol.EventBrainCaptureChanged, uuidToString(c.WorkspaceID), actorType, actorID, map[string]any{"capture_id": uuidToString(c.ID), "status": c.Status, "change": change})
}

// POST /api/brain/captures/upload (multipart: file, title_hint?, origin?, content?)
func (h *Handler) UploadBrainCapture(w http.ResponseWriter, r *http.Request) {
	if h.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "file upload not configured")
		return
	}
	wsUUID, userUUID, ok := h.loadBrainWorkspace(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, brainCaptureMaxUpload)
	if err := r.ParseMultipartForm(brainCaptureMaxUpload); err != nil {
		writeError(w, http.StatusBadRequest, "file too large or invalid multipart form")
		return
	}
	defer func() { _ = r.MultipartForm.RemoveAll() }()
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, `a file is required (form field "file")`)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil || len(data) == 0 {
		writeError(w, http.StatusBadRequest, "failed to read the file")
		return
	}
	sniff := data
	if len(sniff) > 512 {
		sniff = sniff[:512]
	}
	contentType := http.DetectContentType(sniff)
	if ct, ok := extContentTypes[strings.ToLower(path.Ext(header.Filename))]; ok {
		contentType = ct
	}
	if declared := header.Header.Get("Content-Type"); strings.HasPrefix(contentType, "application/octet-stream") && declared != "" {
		contentType = declared
	}
	kind := "file"
	switch {
	case strings.HasPrefix(contentType, "image/"):
		kind = "image"
	case strings.HasPrefix(contentType, "audio/") || strings.HasPrefix(contentType, "video/webm"):
		kind = "audio"
	}
	actorType, actorID, taskID := h.noteActor(r, uuidToString(userUUID), uuidToString(wsUUID))
	id, err := uuid.NewV7()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	key := "workspaces/" + uuidToString(wsUUID) + "/" + id.String() + path.Ext(header.Filename)
	link, err := h.Storage.Upload(r.Context(), key, data, contentType, header.Filename)
	if err != nil {
		slog.Error("brain capture upload failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to store the file")
		return
	}
	att, err := h.Queries.CreateAttachment(r.Context(), db.CreateAttachmentParams{
		ID: pgtype.UUID{Bytes: id, Valid: true}, WorkspaceID: wsUUID, UploaderType: actorType, UploaderID: actorID, Filename: header.Filename, Url: link, ContentType: contentType, SizeBytes: int64(len(data)),
	})
	if err != nil {
		h.Storage.Delete(r.Context(), key)
		writeError(w, http.StatusInternalServerError, "failed to record the file")
		return
	}
	content := strings.TrimSpace(util.SanitizeTextForPostgres(r.FormValue("content")))
	hint := strings.TrimSpace(util.SanitizeTextForPostgres(r.FormValue("title_hint")))
	if hint == "" && kind != "text" {
		hint = strings.TrimSuffix(header.Filename, path.Ext(header.Filename))
	}
	if utf8.RuneCountInString(hint) > brainCaptureMaxTitleRunes {
		hint = string([]rune(hint)[:brainCaptureMaxTitleRunes])
	}
	transcription := "none"
	if kind == "audio" {
		transcription = "pending"
		if h.STT == nil || !h.STT.Enabled() {
			transcription = "failed"
		}
	}
	capture, err := h.Queries.CreateBrainCapture(r.Context(), db.CreateBrainCaptureParams{
		ID: dbid.NewV7(), WorkspaceID: wsUUID, Kind: kind, Content: content, Url: "", TitleHint: hint, AttachmentID: att.ID, Origin: brainCaptureOrigin(r, r.FormValue("origin"), actorType), TranscriptionStatus: transcription,
		CreatedByType: actorType, CreatedByID: actorID, SourceTaskID: taskID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to capture")
		return
	}
	_ = h.Queries.AttachAttachmentToCapture(r.Context(), db.AttachAttachmentToCaptureParams{ID: att.ID, WorkspaceID: wsUUID, CaptureID: capture.ID})
	h.afterBrainCapture(r.Context(), capture, actorType, uuidToString(actorID))
	if transcription == "pending" {
		h.transcribeBrainCaptureAsync(capture, header.Filename, contentType, data, actorType, uuidToString(actorID))
	}
	writeJSON(w, http.StatusCreated, struct {
		Capture BrainCaptureResponse `json:"capture"`
	}{h.brainCaptureToResponse(r.Context(), capture, attachmentURLModeFromRequest(r))})
}

// transcribeBrainCaptureAsync turns a voice memo into text, then asks for
// the suggestion the text now allows.
func (h *Handler) transcribeBrainCaptureAsync(c db.BrainCapture, filename, contentType string, audio []byte, actorType, actorID string) {
	go func() {
		ctx := context.Background()
		res, err := h.STT.TranscribePlain(ctx, filename, contentType, strings.NewReader(string(audio)))
		status, text := "done", strings.TrimSpace(res.Text)
		if err != nil || text == "" {
			status, text = "failed", c.Content
			if err != nil {
				slog.Warn("brain capture transcription failed", "capture_id", uuidToString(c.ID), "error", err)
			}
		}
		if utf8.RuneCountInString(text) > brainCaptureMaxContentRunes {
			text = string([]rune(text)[:brainCaptureMaxContentRunes])
		}
		updated, err := h.Queries.SetBrainCaptureTranscript(ctx, db.SetBrainCaptureTranscriptParams{ID: c.ID, WorkspaceID: c.WorkspaceID, Content: text, TranscriptionStatus: status})
		if err != nil {
			return
		}
		h.publishBrainCapture(updated, actorType, actorID, "transcribed")
		if status == "done" {
			h.suggestBrainCaptureAsync(updated)
		}
	}()
}

// GET /api/brain/captures?status=raw|organized|discarded|all&limit=
func (h *Handler) ListBrainCaptures(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.loadBrainWorkspace(w, r)
	if !ok {
		return
	}
	params := db.ListBrainCapturesParams{WorkspaceID: wsUUID, Limit: brainCaptureListLimit}
	switch status := r.URL.Query().Get("status"); status {
	case "", BrainCaptureStatusRaw:
		params.Status = pgtype.Text{String: BrainCaptureStatusRaw, Valid: true}
	case BrainCaptureStatusOrganized, BrainCaptureStatusDiscarded:
		params.Status = pgtype.Text{String: status, Valid: true}
	case "all":
	default:
		writeError(w, http.StatusBadRequest, "status must be raw, organized, discarded or all")
		return
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var n int
		if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n < 1 || n > brainCaptureListLimit {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("limit must be between 1 and %d", brainCaptureListLimit))
			return
		}
		params.Limit = int32(n)
	}
	rows, err := h.Queries.ListBrainCaptures(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list captures")
		return
	}
	mode := attachmentURLModeFromRequest(r)
	out := make([]BrainCaptureResponse, 0, len(rows))
	for _, c := range rows {
		out = append(out, h.brainCaptureToResponse(r.Context(), c, mode))
	}
	rawCount, _ := h.Queries.CountRawBrainCaptures(r.Context(), wsUUID)
	writeJSON(w, http.StatusOK, struct {
		Captures []BrainCaptureResponse `json:"captures"`
		RawCount int64                  `json:"raw_count"`
	}{out, rawCount})
}

func (h *Handler) loadBrainCapture(w http.ResponseWriter, r *http.Request, wsUUID pgtype.UUID) (db.BrainCapture, bool) {
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "capture id")
	if !ok {
		return db.BrainCapture{}, false
	}
	c, err := h.Queries.GetBrainCapture(r.Context(), db.GetBrainCaptureParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "capture not found")
		return db.BrainCapture{}, false
	}
	return c, true
}

// GET /api/brain/captures/{id}
func (h *Handler) GetBrainCapture(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.loadBrainWorkspace(w, r)
	if !ok {
		return
	}
	c, ok := h.loadBrainCapture(w, r, wsUUID)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Capture BrainCaptureResponse `json:"capture"`
	}{h.brainCaptureToResponse(r.Context(), c, attachmentURLModeFromRequest(r))})
}

// --- suggestion ------------------------------------------------------------------------

const brainSuggestSystemPrompt = `You organize a team's shared knowledge base ("Brain"). A person captured something raw: a line of text, a link, a transcript, a file name. Propose how to file it.

Return JSON only:
{"title": "<a note title, at most 12 words, in the capture's language>",
 "tags": ["<lowercase tag>", ...],           // 0 to 5 short tags
 "summary": "<one or two sentences: what this is and why it was kept>",
 "action": "note" | "merge" | "discard",     // merge only when one of the candidate notes is clearly the same topic
 "merge_note_id": "<candidate id or empty>",
 "reason": "<one sentence>"}

Rules: never invent facts that are not in the capture; keep the person's words; "discard" only for empty, test or accidental captures; when unsure between note and merge, choose note.`

func brainSuggestUserPrompt(c db.BrainCapture, candidates []db.SearchWorkspaceNotesRow) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Capture kind: %s\n", c.Kind)
	if c.TitleHint != "" {
		fmt.Fprintf(&b, "Title hint: %s\n", c.TitleHint)
	}
	if c.Url != "" {
		fmt.Fprintf(&b, "URL: %s\n", c.Url)
	}
	content := c.Content
	if len(content) > 6000 {
		content = content[:6000] + "…"
	}
	fmt.Fprintf(&b, "Content:\n<capture>\n%s\n</capture>\n", content)
	if len(candidates) > 0 {
		b.WriteString("\nExisting notes that may be the same topic (id · title · excerpt):\n")
		for _, n := range candidates {
			excerpt := n.Content
			if len(excerpt) > 240 {
				excerpt = excerpt[:240] + "…"
			}
			fmt.Fprintf(&b, "- %s · %s · %s\n", uuidToString(n.ID), n.Title, strings.ReplaceAll(excerpt, "\n", " "))
		}
	} else {
		b.WriteString("\nNo existing note looks related.\n")
	}
	return b.String()
}

// brainCaptureCandidates finds the notes a capture may belong to.
func (h *Handler) brainCaptureCandidates(ctx context.Context, c db.BrainCapture) []db.SearchWorkspaceNotesRow {
	query := strings.TrimSpace(c.TitleHint + " " + c.Content)
	if query == "" {
		query = c.Url
	}
	if len(query) > 500 {
		query = query[:500]
	}
	if strings.TrimSpace(query) == "" {
		return nil
	}
	rows, err := h.searchWorkspaceNotes(ctx, wsSearch{wsUUID: c.WorkspaceID, query: query, limit: brainCaptureSuggestMaxNotes})
	if err != nil {
		return nil
	}
	return rows
}

// suggestBrainCapture asks the model; ok is false without a model.
func (h *Handler) suggestBrainCapture(ctx context.Context, c db.BrainCapture) (BrainCaptureSuggestion, bool, error) {
	if h.LLM == nil || !h.LLM.Enabled() {
		return BrainCaptureSuggestion{}, false, nil
	}
	if strings.TrimSpace(c.Content) == "" && c.Url == "" && c.TitleHint == "" {
		return BrainCaptureSuggestion{}, false, nil
	}
	candidates := h.brainCaptureCandidates(ctx, c)
	raw, err := h.LLM.GenerateJSON(ctx, "", brainSuggestSystemPrompt, brainSuggestUserPrompt(c, candidates), 0, 800)
	if err != nil {
		return BrainCaptureSuggestion{}, true, err
	}
	var out struct {
		Title       string   `json:"title"`
		Tags        []string `json:"tags"`
		Summary     string   `json:"summary"`
		Action      string   `json:"action"`
		MergeNoteID string   `json:"merge_note_id"`
		Reason      string   `json:"reason"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return BrainCaptureSuggestion{}, true, fmt.Errorf("suggestion is not JSON: %w", err)
	}
	s := BrainCaptureSuggestion{Title: strings.TrimSpace(out.Title), Summary: strings.TrimSpace(out.Summary), Action: out.Action, Reason: strings.TrimSpace(out.Reason), Candidates: []BrainCaptureMergeTarget{}, Model: h.LLM.DefaultModel()}
	if tags, ok := normalizeWorkspaceNoteTags(out.Tags); ok {
		s.Tags = tags
	} else {
		s.Tags = []string{}
	}
	if s.Title == "" {
		s.Title = nonEmpty(c.TitleHint, firstLine(c.Content, 80))
	}
	if utf8.RuneCountInString(s.Title) > workspaceNoteMaxTitleRunes {
		s.Title = string([]rune(s.Title)[:workspaceNoteMaxTitleRunes])
	}
	for _, n := range candidates {
		s.Candidates = append(s.Candidates, BrainCaptureMergeTarget{ID: uuidToString(n.ID), Title: n.Title})
		if out.MergeNoteID != "" && uuidToString(n.ID) == out.MergeNoteID {
			s.MergeNote = &BrainCaptureMergeTarget{ID: uuidToString(n.ID), Title: n.Title}
		}
	}
	switch s.Action {
	case "merge":
		if s.MergeNote == nil {
			s.Action = "note" // a merge target the workspace does not have is no merge
		}
	case "discard", "note":
	default:
		s.Action = "note"
	}
	return s, true, nil
}

func firstLine(s string, max int) string {
	line := strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0])
	if utf8.RuneCountInString(line) > max {
		return string([]rune(line)[:max])
	}
	return line
}

func (h *Handler) suggestBrainCaptureAsync(c db.BrainCapture) {
	if h.LLM == nil || !h.LLM.Enabled() {
		return
	}
	go func() {
		ctx := context.Background()
		s, ok, err := h.suggestBrainCapture(ctx, c)
		if !ok || err != nil {
			if err != nil {
				slog.Warn("brain capture suggestion failed", "capture_id", uuidToString(c.ID), "error", err)
			}
			return
		}
		raw, _ := json.Marshal(s)
		updated, err := h.Queries.SetBrainCaptureSuggestion(ctx, db.SetBrainCaptureSuggestionParams{ID: c.ID, WorkspaceID: c.WorkspaceID, Suggestion: raw})
		if err != nil {
			return
		}
		h.publishBrainCapture(updated, "system", "", "suggested")
	}()
}

// POST /api/brain/captures/{id}/suggest — a fresh suggestion, on demand.
func (h *Handler) SuggestBrainCapture(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.loadBrainWorkspace(w, r)
	if !ok {
		return
	}
	c, ok := h.loadBrainCapture(w, r, wsUUID)
	if !ok {
		return
	}
	s, available, err := h.suggestBrainCapture(r.Context(), c)
	if !available {
		writeError(w, http.StatusServiceUnavailable, "no model is configured to suggest; organize the capture yourself")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, "the model could not suggest: "+err.Error())
		return
	}
	raw, _ := json.Marshal(s)
	updated, err := h.Queries.SetBrainCaptureSuggestion(r.Context(), db.SetBrainCaptureSuggestionParams{ID: c.ID, WorkspaceID: wsUUID, Suggestion: raw})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the suggestion")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Capture BrainCaptureResponse `json:"capture"`
	}{h.brainCaptureToResponse(r.Context(), updated, attachmentURLModeFromRequest(r))})
}

// --- organize ------------------------------------------------------------------------

type brainOrganizeRequest struct {
	Action  string   `json:"action"` // note | merge | discard
	Title   string   `json:"title"`
	Tags    []string `json:"tags"`
	Content string   `json:"content"`
	Pinned  bool     `json:"pinned"`
	NoteID  string   `json:"note_id"`
}

// brainCaptureAsMarkdown is what a capture becomes inside a note when the
// person did not rewrite it.
func brainCaptureAsMarkdown(c db.BrainCapture, att *db.Attachment, markdownURL string) string {
	var b strings.Builder
	if c.Url != "" {
		fmt.Fprintf(&b, "%s\n\n", c.Url)
	}
	if strings.TrimSpace(c.Content) != "" {
		b.WriteString(strings.TrimSpace(c.Content))
		b.WriteString("\n")
	}
	if att != nil && markdownURL != "" {
		if strings.HasPrefix(att.ContentType, "image/") {
			fmt.Fprintf(&b, "\n![%s](%s)\n", att.Filename, markdownURL)
		} else {
			fmt.Fprintf(&b, "\n[%s](%s)\n", att.Filename, markdownURL)
		}
	}
	return strings.TrimSpace(b.String())
}

// POST /api/brain/captures/{id}/organize {action, title, tags, content, pinned, note_id}
func (h *Handler) OrganizeBrainCapture(w http.ResponseWriter, r *http.Request) {
	wsUUID, userUUID, ok := h.loadBrainWorkspace(w, r)
	if !ok {
		return
	}
	c, ok := h.loadBrainCapture(w, r, wsUUID)
	if !ok {
		return
	}
	if c.Status != BrainCaptureStatusRaw {
		writeError(w, http.StatusConflict, "this capture is already "+c.Status)
		return
	}
	var req brainOrganizeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	actorType, actorID, taskID := h.noteActor(r, uuidToString(userUUID), uuidToString(wsUUID))
	var att *db.Attachment
	markdownURL := ""
	if c.AttachmentID.Valid {
		if row, err := h.Queries.GetAttachment(r.Context(), db.GetAttachmentParams{ID: c.AttachmentID, WorkspaceID: c.WorkspaceID}); err == nil {
			att = &row
			markdownURL = h.buildMarkdownURL(row, uuidToString(row.ID))
		}
	}
	body := strings.TrimSpace(util.SanitizeTextForPostgres(req.Content))
	if body == "" {
		body = brainCaptureAsMarkdown(c, att, markdownURL)
	}
	var note *db.WorkspaceNote
	switch req.Action {
	case "discard":
	case "note":
		title, ok := validateWorkspaceNoteTitle(nonEmpty(req.Title, nonEmpty(c.TitleHint, firstLine(c.Content, 80))))
		if !ok {
			writeError(w, http.StatusBadRequest, "a title of at most 200 characters is required")
			return
		}
		content, ok := validateWorkspaceNoteContent(body)
		if !ok {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("content is limited to %d characters", workspaceNoteMaxContentRunes))
			return
		}
		tags, ok := normalizeWorkspaceNoteTags(req.Tags)
		if !ok {
			writeError(w, http.StatusBadRequest, "at most 10 tags of 50 characters")
			return
		}
		params := db.CreateWorkspaceNoteParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, Title: title, Content: content, Tags: tags, Source: "capture", Pinned: req.Pinned, CreatedByType: actorType, CreatedByID: actorID}
		if actorType == "agent" {
			params.SourceAgentID = actorID
			params.SourceTaskID = taskID
		}
		created, err := h.Queries.CreateWorkspaceNote(r.Context(), params)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to create the note")
			return
		}
		note = &created
		h.publish(protocol.EventWorkspaceNoteCreated, uuidToString(wsUUID), actorType, uuidToString(actorID), map[string]any{"note": workspaceNoteToResponse(created)})
	case "merge":
		noteID, ok := parseUUIDOrBadRequest(w, req.NoteID, "note_id")
		if !ok {
			return
		}
		existing, err := h.Queries.GetWorkspaceNote(r.Context(), db.GetWorkspaceNoteParams{ID: noteID, WorkspaceID: wsUUID})
		if err != nil {
			writeError(w, http.StatusNotFound, "note not found")
			return
		}
		merged := strings.TrimRight(existing.Content, "\n") + "\n\n" + body
		content, ok := validateWorkspaceNoteContent(merged)
		if !ok {
			writeError(w, http.StatusBadRequest, "the note would exceed its size limit; create a new note instead")
			return
		}
		tags := existing.Tags
		if len(req.Tags) > 0 {
			if normalized, ok := normalizeWorkspaceNoteTags(append(append([]string{}, existing.Tags...), req.Tags...)); ok {
				tags = normalized
			}
		}
		updated, err := h.Queries.UpdateWorkspaceNote(r.Context(), db.UpdateWorkspaceNoteParams{ID: existing.ID, WorkspaceID: wsUUID, Content: pgtype.Text{String: content, Valid: true}, Tags: tags, ExpectedRevision: existing.Revision})
		if err != nil {
			writeError(w, http.StatusConflict, "the note changed while merging; try again")
			return
		}
		note = &updated
		h.publish(protocol.EventWorkspaceNoteUpdated, uuidToString(wsUUID), actorType, uuidToString(actorID), map[string]any{"note": workspaceNoteToResponse(updated)})
	default:
		writeError(w, http.StatusBadRequest, "action must be note, merge or discard")
		return
	}
	status, noteRef := BrainCaptureStatusDiscarded, pgtype.UUID{}
	if note != nil {
		status, noteRef = BrainCaptureStatusOrganized, note.ID
		if att != nil {
			_ = h.Queries.AttachAttachmentToNote(r.Context(), db.AttachAttachmentToNoteParams{ID: att.ID, WorkspaceID: wsUUID, NoteID: note.ID})
		}
		h.embedNoteAsync(note.ID)
	}
	updated, err := h.Queries.OrganizeBrainCapture(r.Context(), db.OrganizeBrainCaptureParams{ID: c.ID, WorkspaceID: wsUUID, Status: status, NoteID: noteRef, OrganizedBy: userUUID})
	if err != nil {
		writeError(w, http.StatusConflict, "this capture was organized meanwhile")
		return
	}
	h.audit(r.Context(), wsUUID, actorType, uuidToString(actorID), AuditBrainOrganized, "brain_capture", c.ID, map[string]any{"action": req.Action, "note_id": uuidToPtr(noteRef)}, nil)
	h.publishBrainCapture(updated, actorType, uuidToString(actorID), req.Action)
	resp := struct {
		Capture BrainCaptureResponse   `json:"capture"`
		Note    *WorkspaceNoteResponse `json:"note"`
	}{Capture: h.brainCaptureToResponse(r.Context(), updated, attachmentURLModeFromRequest(r))}
	if note != nil {
		n := workspaceNoteToResponse(*note)
		resp.Note = &n
	}
	writeJSON(w, http.StatusOK, resp)
}

// POST /api/brain/captures/{id}/reopen — a discarded capture back to raw.
func (h *Handler) ReopenBrainCapture(w http.ResponseWriter, r *http.Request) {
	wsUUID, userUUID, ok := h.loadBrainWorkspace(w, r)
	if !ok {
		return
	}
	c, ok := h.loadBrainCapture(w, r, wsUUID)
	if !ok {
		return
	}
	updated, err := h.Queries.ReopenBrainCapture(r.Context(), db.ReopenBrainCaptureParams{ID: c.ID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusConflict, "only a discarded capture can be reopened")
		return
	}
	h.publishBrainCapture(updated, "member", uuidToString(userUUID), "reopened")
	writeJSON(w, http.StatusOK, struct {
		Capture BrainCaptureResponse `json:"capture"`
	}{h.brainCaptureToResponse(r.Context(), updated, attachmentURLModeFromRequest(r))})
}

// --- ranked note search --------------------------------------------------------------

type wsSearch struct {
	wsUUID          pgtype.UUID
	query           string
	tag             string
	includeArchived bool
	limit           int32
}

// searchWorkspaceNotes runs the fused search, with the query embedded when
// a provider is configured.
func (h *Handler) searchWorkspaceNotes(ctx context.Context, s wsSearch) ([]db.SearchWorkspaceNotesRow, error) {
	params := db.SearchWorkspaceNotesParams{WorkspaceID: s.wsUUID, Query: s.query, IncludeArchived: s.includeArchived, Prefilter: 60, TopK: s.limit}
	if s.tag != "" {
		params.Tag = pgtype.Text{String: s.tag, Valid: true}
	}
	if h.BrainEmbedder != nil {
		if literal, model, ok := h.BrainEmbedder.QueryEmbedding(ctx, s.query); ok {
			params.QueryEmbedding = pgtype.Text{String: literal, Valid: true}
			params.EmbeddingModel = pgtype.Text{String: model, Valid: true}
		}
	}
	return h.Queries.SearchWorkspaceNotes(ctx, params)
}

type WorkspaceNoteSearchHit struct {
	WorkspaceNoteResponse
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
	LexRank *int64  `json:"lex_rank"`
	VecRank *int64  `json:"vec_rank"`
}

// GET /api/workspace/notes/search?q=&tag=&archived=&limit=
func (h *Handler) SearchWorkspaceNotes(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.loadBrainWorkspace(w, r)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" || utf8.RuneCountInString(q) > 500 {
		writeError(w, http.StatusBadRequest, "q is required (at most 500 characters)")
		return
	}
	limit := int32(20)
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var n int
		if _, err := fmt.Sscanf(raw, "%d", &n); err != nil || n < 1 || n > 100 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 100")
			return
		}
		limit = int32(n)
	}
	rows, err := h.searchWorkspaceNotes(r.Context(), wsSearch{wsUUID: wsUUID, query: q, tag: strings.TrimSpace(r.URL.Query().Get("tag")), includeArchived: r.URL.Query().Get("archived") == "true", limit: limit})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "search failed")
		return
	}
	out := make([]WorkspaceNoteSearchHit, 0, len(rows))
	for _, row := range rows {
		note := db.WorkspaceNote{ID: row.ID, WorkspaceID: row.WorkspaceID, Title: row.Title, Content: row.Content, Tags: row.Tags, Source: row.Source, SourceTaskID: row.SourceTaskID, SourceAgentID: row.SourceAgentID, Pinned: row.Pinned, ArchivedAt: row.ArchivedAt, MergedInto: row.MergedInto, CreatedByType: row.CreatedByType, CreatedByID: row.CreatedByID, Revision: row.Revision, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}
		hit := WorkspaceNoteSearchHit{WorkspaceNoteResponse: workspaceNoteToResponse(note), Score: row.Score, Snippet: string(row.Snippet)}
		if row.LexRank.Valid {
			v := row.LexRank.Int64
			hit.LexRank = &v
		}
		if row.VecRank.Valid {
			v := row.VecRank.Int64
			hit.VecRank = &v
		}
		out = append(out, hit)
	}
	writeJSON(w, http.StatusOK, struct {
		Notes  []WorkspaceNoteSearchHit `json:"notes"`
		Vector bool                     `json:"vector"`
	}{out, h.BrainEmbedder != nil && h.BrainEmbedder.Enabled()})
}

// embedNoteAsync refreshes a note's vector after a write, when an embedder
// is configured.
func (h *Handler) embedNoteAsync(noteID pgtype.UUID) {
	if h.BrainEmbedder != nil {
		h.BrainEmbedder.EmbedNoteAsync(noteID)
	}
}

// BackfillBrainEmbeddings is the scheduler entry point: embeds up to a batch
// of notes whose vector is missing or stale. Returns how many were embedded.
func (h *Handler) BackfillBrainEmbeddings(ctx context.Context) int {
	b, ok := h.BrainEmbedder.(*service.BrainEmbedder)
	if !ok || !b.Enabled() {
		return 0
	}
	n, err := b.Backfill(ctx, 200)
	if err != nil {
		slog.Warn("brain embedding backfill stopped", "embedded", n, "error", err)
	}
	return n
}
