package handler

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/stt"
)

// TestNormalizeVoiceLanguage is the canonical matrix for the allowlist: an
// unknown or malformed code degrades to "" (the deployment default), never to
// a value forwarded to the provider.
func TestNormalizeVoiceLanguage(t *testing.T) {
	cases := map[string]string{
		"fr":      "fr",
		"FR":      "fr",
		"  ja  ":  "ja",
		"zh":      "zh",
		"":        "",
		"de":      "",
		"fr-FR":   "",
		"'; --":   "",
		"english": "",
	}
	for input, want := range cases {
		if got := normalizeVoiceLanguage(input); got != want {
			t.Errorf("normalizeVoiceLanguage(%q) = %q, want %q", input, got, want)
		}
	}
}

// TestTranscribeVoiceForwardsLanguage pins the whole path: the client's
// `language` field reaches the provider request, an unknown one falls back to
// the deployment's MULTICA_STT_LANGUAGE, and no field at all keeps the same
// default.
func TestTranscribeVoiceForwardsLanguage(t *testing.T) {
	var seen chan string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseMultipartForm(1 << 20)
		seen <- r.FormValue("language")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"ok"}`)
	}))
	t.Cleanup(srv.Close)

	prev := testHandler.STT
	// The deployment default the request-level choice must override.
	testHandler.STT = stt.New(stt.Config{BaseURL: srv.URL, Model: "stub", Language: "en"})
	t.Cleanup(func() { testHandler.STT = prev })

	upload := func(t *testing.T, language string) string {
		t.Helper()
		seen = make(chan string, 1)
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		part, err := mw.CreateFormFile("file", "memo.webm")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		_, _ = part.Write([]byte("OPUSBYTES"))
		if language != "" {
			_ = mw.WriteField("language", language)
		}
		_ = mw.Close()
		req := httptest.NewRequest(http.MethodPost, "/api/voice/transcribe", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("X-User-ID", testUserID)
		req.Header.Set("X-Workspace-ID", testWorkspaceID)
		testutil.Call(t, testHandler.TranscribeVoice, req).Want(http.StatusOK)
		return <-seen
	}

	if got := upload(t, "fr"); got != "fr" {
		t.Errorf("provider language = %q, want fr (the request's choice)", got)
	}
	if got := upload(t, ""); got != "en" {
		t.Errorf("provider language = %q, want en (the deployment default)", got)
	}
	if got := upload(t, "de"); got != "en" {
		t.Errorf("provider language = %q for an unsupported code, want the deployment default en", got)
	}
}
