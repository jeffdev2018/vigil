// Package tts is a thin client for OpenAI-compatible text-to-speech endpoints
// (POST {base}/v1/audio/speech, JSON in, audio bytes out). OpenAI, Groq, and
// self-hosted servers such as Kokoro-FastAPI or openedai-speech all speak this
// contract, so the deployment picks the provider through configuration alone:
// MULTICA_TTS_BASE_URL, MULTICA_TTS_API_KEY, MULTICA_TTS_MODEL,
// MULTICA_TTS_VOICE.
//
// Same shape as pkg/stt, deliberately: one provider, read once at boot,
// Enabled() reports whether it can do anything, and the handler answers 409
// when it cannot.
package tts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotConfigured is returned by Speak when no provider is configured.
var ErrNotConfigured = errors.New("tts: not configured")

const (
	maxErrorBody = 300
	// maxAudio bounds one synthesized clip. 4000 characters of speech is a few
	// minutes of mp3; 20 MiB is well past that and still cheap to hold.
	maxAudio = 20 << 20
	// MaxTextRunes is the longest text one request may synthesize. Callers
	// split longer text into sentences themselves.
	MaxTextRunes = 4000
)

// Config is read once at boot. BaseURL and Model are required for the client
// to be enabled; APIKey is optional so a self-hosted server without auth works.
type Config struct {
	BaseURL string
	APIKey  string
	Model   string
	// Voice is the provider's voice id (OpenAI: alloy, nova, …). Empty lets
	// the provider pick, which some of them refuse — hence the env var.
	Voice string
	// HTTPClient overrides the default client (tests). Nil -> 60s timeout.
	HTTPClient *http.Client
}

// Client synthesizes speech through one configured provider.
type Client struct {
	cfg  Config
	http *http.Client
}

// New builds a client; it is always non-nil, Enabled() reports whether it can
// actually speak.
func New(cfg Config) *Client {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.Voice = strings.TrimSpace(cfg.Voice)
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{cfg: cfg, http: hc}
}

// Enabled reports whether a provider is configured.
func (c *Client) Enabled() bool {
	return c != nil && c.cfg.BaseURL != "" && c.cfg.Model != ""
}

// Audio is one synthesized clip: the bytes and the media type to serve them
// with, taken from the provider's own response.
type Audio struct {
	ContentType string
	Data        []byte
}

// Speak synthesizes `text`. `language` is NOT forwarded: the OpenAI
// /v1/audio/speech contract has no such field and providers that implement it
// strictly answer 400 on an unknown parameter. The caller keeps the language
// for its own cache key and for the browser fallback voice.
func (c *Client) Speak(ctx context.Context, text string) (Audio, error) {
	if !c.Enabled() {
		return Audio{}, ErrNotConfigured
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Audio{}, errors.New("tts: empty text")
	}
	if n := len([]rune(text)); n > MaxTextRunes {
		text = string([]rune(text)[:MaxTextRunes])
	}
	payload := map[string]string{
		"model":           c.cfg.Model,
		"input":           text,
		"response_format": "mp3",
	}
	if c.cfg.Voice != "" {
		payload["voice"] = c.cfg.Voice
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Audio{}, fmt.Errorf("tts: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v1/audio/speech", bytes.NewReader(body))
	if err != nil {
		return Audio{}, fmt.Errorf("tts: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "audio/mpeg")
	if c.cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Audio{}, fmt.Errorf("tts: request: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxAudio))
	if err != nil {
		return Audio{}, fmt.Errorf("tts: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(string(raw))
		if len(snippet) > maxErrorBody {
			snippet = snippet[:maxErrorBody]
		}
		return Audio{}, fmt.Errorf("tts: upstream %d: %s", resp.StatusCode, snippet)
	}
	if len(raw) == 0 {
		return Audio{}, errors.New("tts: provider returned no audio")
	}
	// We asked for mp3, so anything that is not an audio media type is a
	// provider mislabelling its own bytes (or not labelling them at all).
	// Serve them as mp3 rather than handing the browser a type it will refuse
	// to play.
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "audio/") {
		ct = "audio/mpeg"
	}
	return Audio{ContentType: ct, Data: raw}, nil
}
