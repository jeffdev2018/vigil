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
	"net/url"
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
	// RealtimeModel enables live (word-by-word) transcription through the
	// provider's realtime WebSocket, authenticated with short-lived tokens
	// the server mints from POST {base}/v1/client/sessions (Mistral). Empty
	// disables the feature; batch transcription keeps working.
	RealtimeModel string
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
	cfg.RealtimeModel = strings.TrimSpace(cfg.RealtimeModel)
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

// RealtimeEnabled reports whether live transcription sessions can be minted.
func (c *Client) RealtimeEnabled() bool {
	return c.Enabled() && c.cfg.RealtimeModel != "" && c.cfg.APIKey != ""
}

// RealtimeSession is what a browser needs to open the provider's realtime
// WebSocket itself: the URL, a short-lived token (never the API key) and the
// PCM format the session expects. Audio is 16 kHz mono signed 16-bit LE.
type RealtimeSession struct {
	URL        string `json:"url"`
	Model      string `json:"model"`
	Token      string `json:"token"`
	ExpiresAt  string `json:"expires_at"`
	Encoding   string `json:"encoding"`
	SampleRate int    `json:"sample_rate"`
}

// ErrRealtimeNotConfigured is returned when no realtime model is set.
var ErrRealtimeNotConfigured = errors.New("stt: realtime not configured")

// RealtimeSession mints a client token (POST /v1/client/sessions, Mistral's
// contract) and returns the connection details for the browser.
func (c *Client) RealtimeSession(ctx context.Context) (RealtimeSession, error) {
	if !c.RealtimeEnabled() {
		return RealtimeSession{}, ErrRealtimeNotConfigured
	}
	body, _ := json.Marshal(map[string]string{"purpose": "realtime", "model": c.cfg.RealtimeModel})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v1/client/sessions", bytes.NewReader(body))
	if err != nil {
		return RealtimeSession{}, fmt.Errorf("stt: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return RealtimeSession{}, fmt.Errorf("stt: request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return RealtimeSession{}, fmt.Errorf("stt: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > maxErrorBody {
			snippet = snippet[:maxErrorBody]
		}
		return RealtimeSession{}, fmt.Errorf("stt: upstream %d: %s", resp.StatusCode, snippet)
	}
	var parsed struct {
		ExpiresAt    string `json:"expires_at"`
		ClientSecret struct {
			Value     string `json:"value"`
			ExpiresAt string `json:"expires_at"`
		} `json:"client_secret"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return RealtimeSession{}, fmt.Errorf("stt: decode response: %w", err)
	}
	if parsed.ClientSecret.Value == "" {
		return RealtimeSession{}, errors.New("stt: session response carries no client secret")
	}
	expires := parsed.ClientSecret.ExpiresAt
	if expires == "" {
		expires = parsed.ExpiresAt
	}
	return RealtimeSession{
		URL:        realtimeURL(c.cfg.BaseURL, c.cfg.RealtimeModel),
		Model:      c.cfg.RealtimeModel,
		Token:      parsed.ClientSecret.Value,
		ExpiresAt:  expires,
		Encoding:   "pcm_s16le",
		SampleRate: 16000,
	}, nil
}

// realtimeURL turns the HTTP base into the realtime WebSocket endpoint.
func realtimeURL(base, model string) string {
	ws := base
	switch {
	case strings.HasPrefix(ws, "https://"):
		ws = "wss://" + strings.TrimPrefix(ws, "https://")
	case strings.HasPrefix(ws, "http://"):
		ws = "ws://" + strings.TrimPrefix(ws, "http://")
	}
	return ws + "/v1/audio/transcriptions/realtime?model=" + url.QueryEscape(model)
}

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
		Text    string `json:"text"`
		Speaker string `json:"speaker"`
		// Voxtral sends "speaker_1", Whisper-style servers send 0; keep raw.
		SpeakerID json.RawMessage `json:"speaker_id"`
	} `json:"segments"`
}

// speakerLabel turns a provider speaker id into "Speaker N". A number is
// zero-based; a string like "speaker_1" keeps its own number.
func speakerLabel(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return fmt.Sprintf("Speaker %d", n+1)
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil && strings.TrimSpace(str) != "" {
		str = strings.TrimSpace(str)
		if i := strings.LastIndexAny(str, "_- "); i >= 0 && i < len(str)-1 {
			return "Speaker " + str[i+1:]
		}
		return str
	}
	return ""
}

const maxErrorBody = 300

// Transcribe uploads audio and returns its text, with speaker labels when
// diarization is configured. contentType may be empty.
func (c *Client) Transcribe(ctx context.Context, filename, contentType string, audio io.Reader) (Result, error) {
	return c.transcribe(ctx, filename, contentType, audio, c.cfg.Diarize, "")
}

// TranscribePlain is Transcribe without speaker labels: a dictated memo has
// one speaker and the "Speaker 1:" prefix would only be noise in a composer.
func (c *Client) TranscribePlain(ctx context.Context, filename, contentType string, audio io.Reader) (Result, error) {
	return c.transcribe(ctx, filename, contentType, audio, false, "")
}

// TranscribePlainIn is TranscribePlain in a caller-chosen language, which
// overrides MULTICA_STT_LANGUAGE for this one request. An empty language keeps
// the deployment default — that is what "the user has no preference" means,
// not "let the provider guess".
func (c *Client) TranscribePlainIn(ctx context.Context, filename, contentType string, audio io.Reader, language string) (Result, error) {
	return c.transcribe(ctx, filename, contentType, audio, false, language)
}

func (c *Client) transcribe(ctx context.Context, filename, contentType string, audio io.Reader, diarize bool, language string) (Result, error) {
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
	if language == "" {
		language = c.cfg.Language
	}
	if language != "" {
		if err := mw.WriteField("language", language); err != nil {
			return Result{}, fmt.Errorf("stt: build form: %w", err)
		}
	}
	if diarize {
		// Voxtral refuses diarize without a timestamp granularity; other
		// providers ignore both fields.
		for k, v := range map[string]string{"diarize": "true", "timestamp_granularities": "segment"} {
			if err := mw.WriteField(k, v); err != nil {
				return Result{}, fmt.Errorf("stt: build form: %w", err)
			}
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
		if label == "" {
			label = speakerLabel(s.SpeakerID)
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
