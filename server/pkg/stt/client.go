// Package stt is a thin client for OpenAI-compatible speech-to-text
// endpoints (POST {base}/v1/audio/transcriptions, multipart). Mistral Voxtral,
// OpenAI, Groq and a self-hosted Qwen3-ASR / Whisper behind vLLM all speak
// this contract, so the deployment picks the provider through configuration
// alone: MULTICA_STT_BASE_URL, MULTICA_STT_API_KEY, MULTICA_STT_MODEL,
// MULTICA_STT_LANGUAGE, MULTICA_STT_DIARIZE.
package stt

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// ErrNotConfigured is returned by Transcribe when the client has no base URL
// or model. Callers surface it as "transcription not configured".
var ErrNotConfigured = errors.New("stt: not configured")

// Config is read once at boot. BaseURL and Model are required for the client
// to be enabled; APIKey is optional so a self-hosted server without auth works.
type Config struct {
	BaseURL  string
	APIKey   string
	Model    string
	Language string
	// Diarize asks the provider to label speakers. Only providers that accept
	// the `diarize` multipart field honor it (Voxtral); others ignore the field.
	Diarize bool
	// HTTPClient overrides the default client (tests). Nil -> 120s timeout.
	HTTPClient *http.Client
}

// Client transcribes audio through one configured provider.
type Client struct {
	cfg  Config
	http *http.Client
}

// New builds a client; it is always non-nil, Enabled() reports whether it can
// actually transcribe.
func New(cfg Config) *Client {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.Language = strings.TrimSpace(cfg.Language)
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 120 * time.Second}
	}
	return &Client{cfg: cfg, http: hc}
}

// Enabled reports whether a provider is configured.
func (c *Client) Enabled() bool {
	return c != nil && c.cfg.BaseURL != "" && c.cfg.Model != ""
}

// Model returns the configured model id (for diagnostics; never the key).
func (c *Client) Model() string { return c.cfg.Model }

// Result is one transcription. Text is speaker-labelled ("Speaker 1: …" per
// line) when the provider returned diarized segments, plain otherwise.
type Result struct {
	Text string
}

// transcriptionResponse covers the OpenAI shape plus the optional diarized
// segments Voxtral returns. Unknown fields are ignored.
type transcriptionResponse struct {
	Text     string `json:"text"`
	Segments []struct {
		Text      string `json:"text"`
		Speaker   string `json:"speaker"`
		SpeakerID *int   `json:"speaker_id"`
	} `json:"segments"`
}

const maxErrorBody = 300

// Transcribe uploads audio and returns its text. contentType may be empty.
func (c *Client) Transcribe(ctx context.Context, filename, contentType string, audio io.Reader) (Result, error) {
	if !c.Enabled() {
		return Result{}, ErrNotConfigured
	}
	if strings.TrimSpace(filename) == "" {
		filename = "audio.webm"
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", filename)
	if err != nil {
		return Result{}, fmt.Errorf("stt: build form: %w", err)
	}
	if _, err := io.Copy(part, audio); err != nil {
		return Result{}, fmt.Errorf("stt: read audio: %w", err)
	}
	if err := mw.WriteField("model", c.cfg.Model); err != nil {
		return Result{}, fmt.Errorf("stt: build form: %w", err)
	}
	if c.cfg.Language != "" {
		if err := mw.WriteField("language", c.cfg.Language); err != nil {
			return Result{}, fmt.Errorf("stt: build form: %w", err)
		}
	}
	if c.cfg.Diarize {
		if err := mw.WriteField("diarize", "true"); err != nil {
			return Result{}, fmt.Errorf("stt: build form: %w", err)
		}
	}
	if err := mw.Close(); err != nil {
		return Result{}, fmt.Errorf("stt: build form: %w", err)
	}
	_ = contentType // the provider sniffs the file; the part carries the filename

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v1/audio/transcriptions", &body)
	if err != nil {
		return Result{}, fmt.Errorf("stt: build request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Accept", "application/json")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("stt: request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return Result{}, fmt.Errorf("stt: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > maxErrorBody {
			snippet = snippet[:maxErrorBody]
		}
		return Result{}, fmt.Errorf("stt: upstream %d: %s", resp.StatusCode, snippet)
	}

	var parsed transcriptionResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Result{}, fmt.Errorf("stt: decode response: %w", err)
	}
	return Result{Text: formatText(parsed)}, nil
}

// formatText prefers speaker-labelled lines when every segment carries a
// speaker, else the provider's flat text.
func formatText(p transcriptionResponse) string {
	if len(p.Segments) == 0 {
		return strings.TrimSpace(p.Text)
	}
	lines := make([]string, 0, len(p.Segments))
	labelled := false
	for _, s := range p.Segments {
		text := strings.TrimSpace(s.Text)
		if text == "" {
			continue
		}
		label := strings.TrimSpace(s.Speaker)
		if label == "" && s.SpeakerID != nil {
			label = fmt.Sprintf("Speaker %d", *s.SpeakerID+1)
		}
		if label != "" {
			labelled = true
			lines = append(lines, label+": "+text)
		} else {
			lines = append(lines, text)
		}
	}
	if !labelled {
		if t := strings.TrimSpace(p.Text); t != "" {
			return t
		}
	}
	return strings.Join(lines, "\n")
}
