package stt

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTranscribeSendsOpenAICompatibleForm(t *testing.T) {
	var gotModel, gotLang, gotDiarize, gotGranularity, gotFile, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		gotModel = r.FormValue("model")
		gotLang = r.FormValue("language")
		gotDiarize = r.FormValue("diarize")
		gotGranularity = r.FormValue("timestamp_granularities")
		f, hdr, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("file: %v", err)
		}
		defer f.Close()
		b, _ := io.ReadAll(f)
		gotFile = hdr.Filename + ":" + string(b)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":"  Bonjour à tous.  "}`)
	}))
	defer srv.Close()

	c := New(Config{BaseURL: srv.URL + "/", APIKey: "k", Model: "voxtral-mini-latest", Language: "fr", Diarize: true})
	if !c.Enabled() {
		t.Fatal("client should be enabled")
	}
	res, err := c.Transcribe(context.Background(), "seg-1.webm", "audio/webm", strings.NewReader("AUDIO"))
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if res.Text != "Bonjour à tous." {
		t.Fatalf("text = %q", res.Text)
	}
	if gotModel != "voxtral-mini-latest" || gotLang != "fr" || gotDiarize != "true" || gotGranularity != "segment" {
		t.Fatalf("form = model:%q lang:%q diarize:%q granularity:%q", gotModel, gotLang, gotDiarize, gotGranularity)
	}
	if gotFile != "seg-1.webm:AUDIO" {
		t.Fatalf("file = %q", gotFile)
	}
	if gotAuth != "Bearer k" {
		t.Fatalf("auth = %q", gotAuth)
	}
}

func TestTranscribeLabelsDiarizedSegments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Mixed shapes: Voxtral's "speaker_1" strings and Whisper-style ints.
		_, _ = io.WriteString(w, `{"text":"on livre vendredi ok","segments":[{"type":"transcription_segment","text":"On livre vendredi.","start":0.1,"end":3.0,"speaker_id":"speaker_1"},{"text":"Ok.","speaker_id":1}]}`)
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL, Model: "m"})
	res, err := c.Transcribe(context.Background(), "", "", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("transcribe: %v", err)
	}
	if want := "Speaker 1: On livre vendredi.\nSpeaker 2: Ok."; res.Text != want {
		t.Fatalf("text = %q, want %q", res.Text, want)
	}
}

func TestTranscribeErrors(t *testing.T) {
	if _, err := New(Config{}).Transcribe(context.Background(), "", "", strings.NewReader("x")); err != ErrNotConfigured {
		t.Fatalf("unconfigured err = %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = io.WriteString(w, strings.Repeat("x", 1000))
	}))
	defer srv.Close()
	_, err := New(Config{BaseURL: srv.URL, Model: "m"}).Transcribe(context.Background(), "", "", strings.NewReader("x"))
	if err == nil || !strings.Contains(err.Error(), "upstream 502") {
		t.Fatalf("err = %v", err)
	}
	if len(err.Error()) > maxErrorBody+40 {
		t.Fatalf("error body not truncated: %d chars", len(err.Error()))
	}
}
