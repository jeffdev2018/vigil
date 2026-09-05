package tts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSpeakSendsTheContractAndReturnsAudio(t *testing.T) {
	var gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write([]byte("ID3AUDIOBYTES"))
	}))
	t.Cleanup(srv.Close)

	c := New(Config{BaseURL: srv.URL + "/", APIKey: "k", Model: "tts-1", Voice: " nova "})
	if !c.Enabled() {
		t.Fatal("client must be enabled with a base URL and a model")
	}
	audio, err := c.Speak(context.Background(), "  Bonjour tout le monde.  ")
	if err != nil {
		t.Fatalf("speak: %v", err)
	}
	if string(audio.Data) != "ID3AUDIOBYTES" || audio.ContentType != "audio/mpeg" {
		t.Fatalf("audio = %+v", audio)
	}
	if gotAuth != "Bearer k" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	for _, want := range []string{`"model":"tts-1"`, `"input":"Bonjour tout le monde."`, `"voice":"nova"`, `"response_format":"mp3"`} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("request body %s missing %s", gotBody, want)
		}
	}
	// `language` is deliberately never forwarded: providers implementing the
	// OpenAI contract strictly answer 400 on an unknown parameter.
	if strings.Contains(gotBody, "language") {
		t.Fatalf("request body must not carry a language field: %s", gotBody)
	}
}

func TestSpeakUnconfiguredAndUpstreamFailure(t *testing.T) {
	if _, err := New(Config{}).Speak(context.Background(), "x"); err != ErrNotConfigured {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":"bad key"}`)
	}))
	t.Cleanup(srv.Close)
	if _, err := New(Config{BaseURL: srv.URL, Model: "m"}).Speak(context.Background(), "x"); err == nil ||
		!strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want an upstream 401", err)
	}
	// Empty text never reaches the provider.
	if _, err := New(Config{BaseURL: srv.URL, Model: "m"}).Speak(context.Background(), "   "); err == nil {
		t.Fatal("empty text must fail")
	}
}

func TestSpeakTruncatesAtTheDocumentedCapAndDefaultsContentType(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		got = map[string]string{}
		_ = json.Unmarshal(raw, &got)
		// A provider that forgets the header still returned mp3 bytes.
		_, _ = w.Write([]byte("BYTES"))
	}))
	t.Cleanup(srv.Close)
	audio, err := New(Config{BaseURL: srv.URL, Model: "m"}).
		Speak(context.Background(), strings.Repeat("é", MaxTextRunes+100))
	if err != nil {
		t.Fatalf("speak: %v", err)
	}
	if audio.ContentType != "audio/mpeg" {
		t.Fatalf("content type = %q, want the mp3 default", audio.ContentType)
	}
	if n := len([]rune(got["input"])); n != MaxTextRunes {
		t.Fatalf("input runes = %d, want %d (runes, not bytes)", n, MaxTextRunes)
	}
	if _, ok := got["voice"]; ok {
		t.Fatalf("an unset voice must be omitted, got %v", got)
	}
}
