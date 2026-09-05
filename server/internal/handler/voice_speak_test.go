package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/tts"
)

// stubTTS points testHandler.TTS at a server that answers with `blob` for
// every /v1/audio/speech call.
func stubTTS(t *testing.T, blob string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = io.WriteString(w, blob)
	}))
	t.Cleanup(srv.Close)
	prev := testHandler.TTS
	testHandler.TTS = tts.New(tts.Config{BaseURL: srv.URL, Model: "stub", Voice: "nova"})
	t.Cleanup(func() { testHandler.TTS = prev })
}

func TestSpeakVoiceReturnsAudio(t *testing.T) {
	stubTTS(t, "ID3AUDIO")
	res := testutil.Call(t, testHandler.SpeakVoice,
		newRequest(http.MethodPost, "/api/voice/speak", map[string]string{"text": "Bonjour.", "language": "fr"})).
		Want(http.StatusOK)
	if got := res.Text(); got != "ID3AUDIO" {
		t.Fatalf("body = %q", got)
	}
	if ct := res.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("content type = %q", ct)
	}
}

func TestSpeakVoiceRefusesEmptyAndOverlongText(t *testing.T) {
	stubTTS(t, "x")
	testutil.Call(t, testHandler.SpeakVoice,
		newRequest(http.MethodPost, "/api/voice/speak", map[string]string{"text": "   "})).
		Want(http.StatusBadRequest)
	long := make([]rune, tts.MaxTextRunes+1)
	for i := range long {
		long[i] = 'a'
	}
	// Refused, never truncated: half a message read aloud sounds like the
	// model stopped mid-sentence.
	testutil.Call(t, testHandler.SpeakVoice,
		newRequest(http.MethodPost, "/api/voice/speak", map[string]string{"text": string(long)})).
		Want(http.StatusBadRequest)
}

func TestSpeakVoiceWithoutProviderTellsTheClientToUseItsOwnVoice(t *testing.T) {
	prev := testHandler.TTS
	testHandler.TTS = tts.New(tts.Config{})
	t.Cleanup(func() { testHandler.TTS = prev })
	body := testutil.Call(t, testHandler.SpeakVoice,
		newRequest(http.MethodPost, "/api/voice/speak", map[string]string{"text": "hi"})).
		Want(http.StatusConflict).Map()
	if body["code"] != "tts_not_configured" {
		t.Fatalf("code = %v", body["code"])
	}
}

// The capability the client branches on before it ever calls /api/voice/speak.
func TestConfigReportsTTSAvailability(t *testing.T) {
	stubTTS(t, "x")
	var cfg AppConfig
	testutil.Call(t, testHandler.GetConfig, newRequest(http.MethodGet, "/api/config", nil)).
		Want(http.StatusOK).JSON(&cfg)
	if !cfg.TTSAvailable {
		t.Fatal("tts_available must be true when a provider is configured")
	}
	prev := testHandler.TTS
	testHandler.TTS = tts.New(tts.Config{})
	t.Cleanup(func() { testHandler.TTS = prev })
	testutil.Call(t, testHandler.GetConfig, newRequest(http.MethodGet, "/api/config", nil)).
		Want(http.StatusOK).JSON(&cfg)
	if cfg.TTSAvailable {
		t.Fatal("tts_available must be false without a provider")
	}
}
