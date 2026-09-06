package linear

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OAuth: the authorize URL the admin is sent to, the code exchange, and the
// signed state that ties the callback back to the workspace/agent that started
// it. Both endpoints are fields rather than constants so tests point them at an
// httptest server.

const (
	// DefaultAuthorizeURL is where the workspace admin approves the app.
	DefaultAuthorizeURL = "https://linear.app/oauth/authorize"
	// DefaultTokenURL exchanges the returned code for an access token.
	DefaultTokenURL = "https://api.linear.app/oauth/token"
)

// Scopes the bridge needs: read issues and teams, write status, create issues
// and comments.
var DefaultScopes = []string{"read", "write", "issues:create", "comments:create"}

// Signed-state errors. The callback maps them all to one generic failure so a
// tampered state never reports which check failed.
var (
	ErrStateMalformed = errors.New("linear: state malformed")
	ErrStateSignature = errors.New("linear: state signature mismatch")
	ErrStateExpired   = errors.New("linear: state expired")
	// ErrOAuthNotConfigured means the deployment has no Linear app credentials.
	ErrOAuthNotConfigured = errors.New("linear: MULTICA_LINEAR_CLIENT_ID / MULTICA_LINEAR_CLIENT_SECRET are not set")
)

// StateClaims is what the callback needs to attribute an approval without a
// server-side session table. Short field names keep the token compact; it is
// an internal wire format.
type StateClaims struct {
	WorkspaceID string `json:"w"`
	UserID      string `json:"u"`
	AgentID     string `json:"a"`
	Redirect    string `json:"r"`
	Exp         int64  `json:"e"`
}

// SignState produces a URL-safe "<payload>.<sig>" token.
func SignState(secret []byte, claims StateClaims) (string, error) {
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	return payload + "." + signPayload(secret, payload), nil
}

// VerifyState validates signature then expiry and returns the claims.
func VerifyState(secret []byte, token string, now time.Time) (StateClaims, error) {
	payload, sig, found := strings.Cut(token, ".")
	if !found || payload == "" || sig == "" {
		return StateClaims{}, ErrStateMalformed
	}
	if !hmac.Equal([]byte(sig), []byte(signPayload(secret, payload))) {
		return StateClaims{}, ErrStateSignature
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return StateClaims{}, ErrStateMalformed
	}
	var claims StateClaims
	if err := json.Unmarshal(raw, &claims); err != nil {
		return StateClaims{}, ErrStateMalformed
	}
	if now.Unix() > claims.Exp {
		return StateClaims{}, ErrStateExpired
	}
	return claims, nil
}

func signPayload(secret []byte, payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// OAuthConfig is the deployment-level Linear app plus the callback it
// registered. ClientID/ClientSecret come from the environment and are never
// written to a repo file.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	AuthorizeURL string
	TokenURL     string
	HTTP         *http.Client
}

// Configured reports whether this deployment can run the OAuth flow at all.
func (c OAuthConfig) Configured() bool {
	return c.ClientID != "" && c.ClientSecret != "" && c.RedirectURI != ""
}

// AuthorizeURLFor builds the URL the admin is redirected to.
//
// actor=app is load-bearing: it makes the token act as the app's own Linear
// user rather than as the installing human, which is what gives the bridge a
// stable identity to be assigned issues as and to recognise its own events by.
func (c OAuthConfig) AuthorizeURLFor(state string) (string, error) {
	if !c.Configured() {
		return "", ErrOAuthNotConfigured
	}
	base := c.AuthorizeURL
	if base == "" {
		base = DefaultAuthorizeURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("client_id", c.ClientID)
	q.Set("redirect_uri", c.RedirectURI)
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(DefaultScopes, ","))
	q.Set("state", state)
	q.Set("actor", "app")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// TokenResponse is the exchange result.
type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	Scope       any    `json:"scope"`
	ExpiresIn   int64  `json:"expires_in"`
}

// Exchange trades the callback code for an access token.
func (c OAuthConfig) Exchange(ctx context.Context, code string) (TokenResponse, error) {
	if !c.Configured() {
		return TokenResponse{}, ErrOAuthNotConfigured
	}
	endpoint := c.TokenURL
	if endpoint == "" {
		endpoint = DefaultTokenURL
	}
	form := url.Values{}
	form.Set("code", code)
	form.Set("redirect_uri", c.RedirectURI)
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	form.Set("grant_type", "authorization_code")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return TokenResponse{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return TokenResponse{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return TokenResponse{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return TokenResponse{}, &APIError{Status: resp.StatusCode, Message: truncate(string(body), 300)}
	}
	var out TokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return TokenResponse{}, fmt.Errorf("linear: malformed token response: %w", err)
	}
	if out.AccessToken == "" {
		return TokenResponse{}, errors.New("linear: token response carried no access_token")
	}
	return out, nil
}

// NewWebhookSecret mints the shared secret we hand Linear at webhookCreate and
// then verify every delivery against.
func NewWebhookSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
