package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/multica-ai/multica/server/pkg/tts"
)

// Read aloud: the server voice. The browser's own speechSynthesis stays the
// fallback, so this endpoint is optional — a deployment without MULTICA_TTS_*
// answers 409 and the client never asks again for that session.

type speakVoiceRequest struct {
	Text string `json:"text"`
	// Language is the caller's locale. It is not forwarded upstream (the
	// OpenAI /v1/audio/speech contract has no such field); it is accepted
	// because the client keys its audio cache on it.
	Language string `json:"language"`
}

// SpeakVoice synthesizes one block of text. POST /api/voice/speak.
func (h *Handler) SpeakVoice(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if h.TTS == nil || !h.TTS.Enabled() {
		writeErrorCode(w, http.StatusConflict, "tts_not_configured", "text-to-speech is not configured on this server")
		return
	}
	var req speakVoiceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	text := strings.TrimSpace(req.Text)
	if text == "" {
		writeError(w, http.StatusBadRequest, "text cannot be empty")
		return
	}
	// Refused rather than truncated: silently dropping the tail would read
	// half a message aloud and sound like the model stopped mid-sentence.
	// The client splits by sentence before it gets here.
	if n := len([]rune(text)); n > tts.MaxTextRunes {
		writeError(w, http.StatusBadRequest, "text is longer than "+strconv.Itoa(tts.MaxTextRunes)+" characters")
		return
	}
	audio, err := h.TTS.Speak(r.Context(), text)
	if err != nil {
		slog.Warn("voice speak failed", "error", err)
		writeError(w, http.StatusBadGateway, "speech synthesis failed")
		return
	}
	w.Header().Set("Content-Type", audio.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(audio.Data)))
	// The audio is a pure function of the text and the configured voice, but
	// it is user content: keep it out of shared caches.
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(audio.Data); err != nil {
		slog.Warn("voice speak write failed", "error", err)
	}
}
