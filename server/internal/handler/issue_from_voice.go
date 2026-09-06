package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Voice-dictated issue draft (K36): a phone dictates a short command, the
// server turns it into an editable draft — title, description, labels the
// workspace already has — and the human creates the issue from the draft.
// Nothing is created here. Unlike the scoping assistant (K14), a missing or
// broken LLM is not a refusal: dictation happens away from a keyboard, so
// the endpoint always answers with a usable draft and lets the human edit it.

// IssueOriginVoiceMobile stamps issues created from a voice draft
// (migration 738). It is the only origin with no origin_id — a transcript is
// not a stored row.
const IssueOriginVoiceMobile = "voice_mobile"

const (
	ErrCodeTranscriptTooShort = "transcript_too_short"

	// voiceTranscriptMinChars counts non-space characters. A dictation
	// shorter than this is a mis-tap or a stray noise, not a command.
	voiceTranscriptMinChars = 8
	voiceTranscriptMaxRunes = 4000
	voiceTitleMaxRunes      = 80
	voiceMaxLabels          = 5
	voiceRequestTimeout     = 30 * time.Second
)

type VoiceIssueDraft struct {
	Title           string   `json:"title"`
	Description     string   `json:"description"`
	SuggestedLabels []string `json:"suggested_labels"`
}

const voiceDraftSystemPrompt = `You turn a spoken, dictated command into a draft software issue a human will review.
Answer with JSON only, exactly this shape:
{"title":"...","description":"...","suggested_labels":["..."]}
Rules:
- title: one line, at most 12 words, imperative, no trailing period.
- description: keep every fact the speaker said. Do not invent requirements, do not add sections the speaker did not imply.
- suggested_labels: 0 to 3 names, chosen ONLY from the workspace labels listed below, verbatim. Return an empty array when none fits. Never invent a label.
- Answer in the same language the speaker used.`

// ProposeIssueFromVoiceTranscript — POST /api/issues/from-voice-transcript.
// Nothing is created: the draft is returned for review and editing.
func (h *Handler) ProposeIssueFromVoiceTranscript(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, ctxWorkspaceID(r.Context()), "workspace id")
	if !ok {
		return
	}
	var req struct {
		Transcript string `json:"transcript"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	transcript := strings.TrimSpace(req.Transcript)
	if countNonSpace(transcript) < voiceTranscriptMinChars {
		writeErrorCode(w, http.StatusBadRequest, ErrCodeTranscriptTooShort, "say a little more: a draft needs at least 8 characters")
		return
	}
	if utf8.RuneCountInString(transcript) > voiceTranscriptMaxRunes {
		writeError(w, http.StatusBadRequest, "transcript is too long")
		return
	}

	// Label names are the workspace's own; the model may only pick from them
	// and anything it invents is dropped below.
	labelNames := h.workspaceIssueLabelNames(r.Context(), wsUUID)

	draft, ok := h.voiceDraftFromLLM(r, transcript, labelNames)
	if !ok {
		draft = voiceDraftFallback(transcript, labelNames)
	}
	writeJSON(w, http.StatusOK, normalizeVoiceDraft(draft, transcript, labelNames))
}

// voiceDraftFromLLM asks the model for a draft. Every failure — no LLM, a
// refused call, an unparseable answer — reports ok=false so the caller falls
// back rather than leaving the speaker with nothing.
func (h *Handler) voiceDraftFromLLM(r *http.Request, transcript string, labelNames []string) (VoiceIssueDraft, bool) {
	if h.LLM == nil || !h.LLM.Enabled() {
		return VoiceIssueDraft{}, false
	}
	var prompt strings.Builder
	if len(labelNames) > 0 {
		prompt.WriteString("Workspace labels: ")
		prompt.WriteString(strings.Join(labelNames, ", "))
		prompt.WriteString("\n\n")
	} else {
		prompt.WriteString("Workspace labels: (none — return an empty suggested_labels array)\n\n")
	}
	prompt.WriteString("Dictated command:\n")
	prompt.WriteString(transcript)

	ctx, cancel := context.WithTimeout(r.Context(), voiceRequestTimeout)
	defer cancel()
	raw, err := h.LLM.GenerateJSON(ctx, "", voiceDraftSystemPrompt, prompt.String(), 0.2, 1024)
	if err != nil {
		slog.Warn("voice issue draft: llm call failed", append(logger.RequestAttrs(r), "error", err)...)
		return VoiceIssueDraft{}, false
	}
	var draft VoiceIssueDraft
	if err := json.Unmarshal([]byte(raw), &draft); err != nil {
		slog.Warn("voice issue draft: malformed model answer", append(logger.RequestAttrs(r), "error", err)...)
		return VoiceIssueDraft{}, false
	}
	return draft, true
}

func (h *Handler) workspaceIssueLabelNames(ctx context.Context, wsUUID pgtype.UUID) []string {
	rows, err := h.Queries.ListLabels(ctx, db.ListLabelsParams{WorkspaceID: wsUUID, ResourceType: "issue"})
	if err != nil {
		// A label lookup failure costs suggestions, never the draft.
		slog.Warn("voice issue draft: label lookup failed", "error", err)
		return nil
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		if name := strings.TrimSpace(row.Name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// voiceDraftFallback is the no-LLM draft: the first sentence becomes the
// title, the whole transcript the description, and any workspace label the
// speaker literally named is suggested.
func voiceDraftFallback(transcript string, labelNames []string) VoiceIssueDraft {
	return VoiceIssueDraft{
		Title:           voiceDraftTitle(transcript),
		Description:     strings.TrimSpace(transcript),
		SuggestedLabels: labelsNamedIn(transcript, labelNames),
	}
}

// normalizeVoiceDraft fills a missing title or description from the
// transcript and drops every label the workspace does not have, so the
// client always receives a complete, editable draft.
func normalizeVoiceDraft(d VoiceIssueDraft, transcript string, labelNames []string) VoiceIssueDraft {
	d.Title = strings.Join(strings.Fields(d.Title), " ")
	if d.Title == "" {
		d.Title = voiceDraftTitle(transcript)
	}
	if r := []rune(d.Title); len(r) > voiceTitleMaxRunes {
		d.Title = strings.TrimSpace(string(r[:voiceTitleMaxRunes]))
	}
	d.Description = strings.TrimSpace(d.Description)
	if d.Description == "" {
		d.Description = strings.TrimSpace(transcript)
	}
	d.SuggestedLabels = keepKnownLabels(d.SuggestedLabels, labelNames)
	return d
}

// voiceDraftTitle takes the transcript's first sentence or clause, capped at
// voiceTitleMaxRunes on a word boundary. The terminators are a heuristic —
// "e.g." cuts early — which is acceptable for a draft the human then edits.
func voiceDraftTitle(transcript string) string {
	first := strings.TrimSpace(transcript)
	// Every terminator is ASCII, so the byte index is a safe rune boundary.
	if i := strings.IndexAny(first, ".!?;\n"); i > 0 {
		first = first[:i]
	}
	first = strings.Join(strings.Fields(first), " ")
	runes := []rune(first)
	if len(runes) <= voiceTitleMaxRunes {
		return first
	}
	cut := strings.TrimSpace(string(runes[:voiceTitleMaxRunes]))
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut)
}

// labelsNamedIn returns the workspace labels the speaker said out loud,
// matched case-insensitively, in the workspace's own order and casing.
func labelsNamedIn(transcript string, labelNames []string) []string {
	lower := strings.ToLower(transcript)
	out := make([]string, 0, voiceMaxLabels)
	for _, name := range labelNames {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" || !strings.Contains(lower, strings.ToLower(trimmed)) {
			continue
		}
		out = append(out, trimmed)
		if len(out) == voiceMaxLabels {
			break
		}
	}
	return out
}

// keepKnownLabels drops names the model invented and de-duplicates, matching
// case-insensitively but answering with the workspace's own casing.
func keepKnownLabels(suggested, labelNames []string) []string {
	known := make(map[string]string, len(labelNames))
	for _, name := range labelNames {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			known[strings.ToLower(trimmed)] = trimmed
		}
	}
	seen := make(map[string]bool, len(suggested))
	out := make([]string, 0, voiceMaxLabels)
	for _, s := range suggested {
		key := strings.ToLower(strings.TrimSpace(s))
		name, isKnown := known[key]
		if !isKnown || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, name)
		if len(out) == voiceMaxLabels {
			break
		}
	}
	return out
}

func countNonSpace(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}
