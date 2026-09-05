package handler

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// SSO (K60): one OIDC connection per workspace. The browser starts the
// flow, the identity provider sends it back to the web app with a code, and
// the web app trades the code here for a Multica session. `enforced` closes
// the code and Google logins for the workspace's members and email domains.
// The client secret is encrypted at rest with MULTICA_SSO_SECRET_KEY.

const (
	AuditSSOConfigured = "sso.configured"
	AuditSSOLogin      = "sso.login"
	ssoStateTTL        = 10 * time.Minute
	ssoHTTPTimeout     = 10 * time.Second
)

func (h *Handler) ssoConfigured() bool { return h.SSOSecretBox != nil }

func (h *Handler) sealSSOSecret(plain string) (string, error) {
	sealed, err := h.SSOSecretBox.Seal([]byte(plain))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

func (h *Handler) openSSOSecret(enc string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	plain, err := h.SSOSecretBox.Open(raw)
	return string(plain), err
}

type SSOConnectionResponse struct {
	Provider       string   `json:"provider"`
	Issuer         string   `json:"issuer"`
	ClientID       string   `json:"client_id"`
	HasSecret      bool     `json:"has_secret"`
	AllowedDomains []string `json:"allowed_domains"`
	AutoProvision  bool     `json:"auto_provision"`
	Enforced       bool     `json:"enforced"`
	UpdatedAt      string   `json:"updated_at"`
}

func ssoDomains(raw []byte) []string {
	var out []string
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func ssoToResponse(c db.WorkspaceSsoConnection) SSOConnectionResponse {
	return SSOConnectionResponse{Provider: c.Provider, Issuer: c.Issuer, ClientID: c.ClientID, HasSecret: c.ClientSecretEncrypted != "", AllowedDomains: ssoDomains(c.AllowedDomains), AutoProvision: c.AutoProvision, Enforced: c.Enforced, UpdatedAt: timestampToString(c.UpdatedAt)}
}

// GET /api/workspaces/{id}/sso — owner/admin read; the secret never returns.
func (h *Handler) GetSSOConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}
	conn, err := h.Queries.GetSSOConnection(r.Context(), wsUUID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"connection": nil, "configured": h.ssoConfigured()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"connection": ssoToResponse(conn), "configured": h.ssoConfigured()})
}

type ssoRequest struct {
	Issuer         string   `json:"issuer"`
	ClientID       string   `json:"client_id"`
	ClientSecret   string   `json:"client_secret"`
	AllowedDomains []string `json:"allowed_domains"`
	AutoProvision  *bool    `json:"auto_provision"`
	Enforced       *bool    `json:"enforced"`
}

// PUT /api/workspaces/{id}/sso — owner only.
func (h *Handler) PutSSOConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner"); !ok {
		return
	}
	if !h.ssoConfigured() {
		writeError(w, http.StatusConflict, "SSO is not configured on this server (MULTICA_SSO_SECRET_KEY)")
		return
	}
	var req ssoRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	issuer := strings.TrimRight(strings.TrimSpace(req.Issuer), "/")
	if u, err := url.Parse(issuer); err != nil || u.Scheme != "https" && !(u.Scheme == "http" && strings.HasPrefix(u.Host, "127.0.0.1")) || u.Host == "" {
		writeError(w, http.StatusBadRequest, "issuer must be an https URL")
		return
	}
	if strings.TrimSpace(req.ClientID) == "" {
		writeError(w, http.StatusBadRequest, "client_id is required")
		return
	}
	domains := make([]string, 0, len(req.AllowedDomains))
	for _, d := range req.AllowedDomains {
		d = strings.ToLower(strings.TrimSpace(d))
		if d == "" {
			continue
		}
		if !sandboxHostRe.MatchString(d) {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%q is not a domain", d))
			return
		}
		domains = append(domains, d)
	}
	existing, existErr := h.Queries.GetSSOConnection(r.Context(), wsUUID)
	sealed := ""
	if secret := strings.TrimSpace(req.ClientSecret); secret != "" {
		var err error
		if sealed, err = h.sealSSOSecret(secret); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to store the secret")
			return
		}
	} else if existErr != nil {
		writeError(w, http.StatusBadRequest, "client_secret is required")
		return
	}
	autoProvision, enforced := true, false
	if existErr == nil {
		autoProvision, enforced = existing.AutoProvision, existing.Enforced
	}
	if req.AutoProvision != nil {
		autoProvision = *req.AutoProvision
	}
	if req.Enforced != nil {
		enforced = *req.Enforced
	}
	rawDomains, _ := json.Marshal(domains)
	conn, err := h.Queries.UpsertSSOConnection(r.Context(), db.UpsertSSOConnectionParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, Issuer: issuer, ClientID: strings.TrimSpace(req.ClientID), ClientSecretEncrypted: sealed, AllowedDomains: rawDomains, AutoProvision: autoProvision, Enforced: enforced, CreatedBy: parseUUID(requestUserID(r))})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save the SSO connection")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditSSOConfigured, "workspace", wsUUID, map[string]any{"issuer": issuer, "enforced": enforced, "domains": domains}, nil)
	writeJSON(w, http.StatusOK, map[string]any{"connection": ssoToResponse(conn), "configured": true})
}

// PUT /api/workspaces/{id}/sso/enforce — owner only; the caller must be
// able to log in through the provider: enforcing without a connection is refused.
func (h *Handler) SetSSOEnforced(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner"); !ok {
		return
	}
	var req struct {
		Enforced bool `json:"enforced"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	conn, err := h.Queries.SetSSOEnforced(r.Context(), db.SetSSOEnforcedParams{WorkspaceID: wsUUID, Enforced: req.Enforced})
	if err != nil {
		writeError(w, http.StatusNotFound, "no SSO connection to enforce")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditSSOConfigured, "workspace", wsUUID, map[string]any{"enforced": req.Enforced}, nil)
	writeJSON(w, http.StatusOK, map[string]any{"connection": ssoToResponse(conn), "configured": h.ssoConfigured()})
}

// DELETE /api/workspaces/{id}/sso — owner only.
func (h *Handler) DeleteSSOConnection(w http.ResponseWriter, r *http.Request) {
	workspaceID := workspaceIDFromURL(r, "id")
	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner"); !ok {
		return
	}
	if err := h.Queries.DeleteSSOConnection(r.Context(), wsUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to remove the SSO connection")
		return
	}
	h.audit(r.Context(), wsUUID, "member", requestUserID(r), AuditSSOConfigured, "workspace", wsUUID, map[string]any{"removed": true}, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ssoRequiredFor answers whether a code or Google login must be refused for
// this email: the user belongs to a workspace that enforces SSO, or the
// email's domain is claimed by one. Returns that workspace's slug.
func (h *Handler) ssoRequiredFor(ctx context.Context, userID pgtype.UUID, email string) (string, bool) {
	rows, err := h.Queries.ListEnforcedSSOConnections(ctx)
	if err != nil || len(rows) == 0 {
		return "", false
	}
	domain := ""
	if at := strings.LastIndex(email, "@"); at >= 0 {
		domain = strings.ToLower(email[at+1:])
	}
	for _, c := range rows {
		for _, d := range ssoDomains(c.AllowedDomains) {
			if d == domain {
				return c.WorkspaceSlug, true
			}
		}
		if userID.Valid {
			if _, err := h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{UserID: userID, WorkspaceID: c.WorkspaceID}); err == nil {
				return c.WorkspaceSlug, true
			}
		}
	}
	return "", false
}

// --- OIDC login -------------------------------------------------------------------

type oidcDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JwksURI               string `json:"jwks_uri"`
}

// OIDCHTTPClient is swapped by tests for a fake provider.
var OIDCHTTPClient = &http.Client{Timeout: ssoHTTPTimeout}

func oidcDiscover(ctx context.Context, issuer string) (oidcDiscovery, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, issuer+"/.well-known/openid-configuration", nil)
	if err != nil {
		return oidcDiscovery{}, err
	}
	res, err := OIDCHTTPClient.Do(req)
	if err != nil {
		return oidcDiscovery{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return oidcDiscovery{}, fmt.Errorf("discovery answered %d", res.StatusCode)
	}
	var d oidcDiscovery
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&d); err != nil {
		return oidcDiscovery{}, err
	}
	if d.AuthorizationEndpoint == "" || d.TokenEndpoint == "" || d.JwksURI == "" {
		return oidcDiscovery{}, errors.New("discovery document is incomplete")
	}
	return d, nil
}

func randomURLToken(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// ssoState is signed with the server's JWT secret: the callback proves the
// start happened here, carries the PKCE verifier and the nonce, and expires.
func (h *Handler) signSSOState(workspaceID, verifier, nonce, redirectURI string) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"kind": "sso_state", "ws": workspaceID, "cv": verifier, "nonce": nonce, "ru": redirectURI, "exp": time.Now().Add(ssoStateTTL).Unix(), "iat": time.Now().Unix()}).SignedString(auth.JWTSecret())
}

func (h *Handler) parseSSOState(state string) (jwt.MapClaims, error) {
	tok, err := jwt.Parse(state, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return auth.JWTSecret(), nil
	})
	if err != nil || !tok.Valid {
		return nil, errors.New("invalid state")
	}
	claims, ok := tok.Claims.(jwt.MapClaims)
	if !ok || claims["kind"] != "sso_state" {
		return nil, errors.New("invalid state")
	}
	return claims, nil
}

// POST /auth/oidc/start {workspace_slug, redirect_uri} → {authorization_url}
func (h *Handler) OIDCStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		WorkspaceSlug string `json:"workspace_slug"`
		RedirectURI   string `json:"redirect_uri"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || strings.TrimSpace(req.WorkspaceSlug) == "" || strings.TrimSpace(req.RedirectURI) == "" {
		writeError(w, http.StatusBadRequest, "workspace_slug and redirect_uri are required")
		return
	}
	ws, err := h.Queries.GetWorkspaceBySlug(r.Context(), strings.TrimSpace(req.WorkspaceSlug))
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	conn, err := h.Queries.GetSSOConnection(r.Context(), ws.ID)
	if err != nil || !h.ssoConfigured() {
		writeError(w, http.StatusNotFound, "this workspace has no SSO connection")
		return
	}
	disc, err := oidcDiscover(r.Context(), conn.Issuer)
	if err != nil {
		writeError(w, http.StatusBadGateway, "identity provider discovery failed: "+err.Error())
		return
	}
	verifier, nonce := randomURLToken(32), randomURLToken(16)
	sum := sha256.Sum256([]byte(verifier))
	state, err := h.signSSOState(uuidToString(ws.ID), verifier, nonce, req.RedirectURI)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start the login")
		return
	}
	q := url.Values{"response_type": {"code"}, "client_id": {conn.ClientID}, "redirect_uri": {req.RedirectURI}, "scope": {"openid email profile"}, "state": {state}, "nonce": {nonce}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	sep := "?"
	if strings.Contains(disc.AuthorizationEndpoint, "?") {
		sep = "&"
	}
	writeJSON(w, http.StatusOK, map[string]any{"authorization_url": disc.AuthorizationEndpoint + sep + q.Encode()})
}

type jwks struct {
	Keys []struct {
		Kid string `json:"kid"`
		Kty string `json:"kty"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

func oidcKeyFunc(ctx context.Context, jwksURI string) (jwt.Keyfunc, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return nil, err
	}
	res, err := OIDCHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var set jwks
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&set); err != nil {
		return nil, err
	}
	return func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, errors.New("unexpected id_token signing method")
		}
		kid, _ := t.Header["kid"].(string)
		for _, k := range set.Keys {
			if k.Kty != "RSA" || (kid != "" && k.Kid != kid) {
				continue
			}
			n, err := base64.RawURLEncoding.DecodeString(k.N)
			if err != nil {
				return nil, err
			}
			e, err := base64.RawURLEncoding.DecodeString(k.E)
			if err != nil {
				return nil, err
			}
			return &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}, nil
		}
		return nil, errors.New("no matching key in the provider's JWKS")
	}, nil
}

// POST /auth/oidc/callback {code, state} → {token, user, workspace_slug}
func (h *Handler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil || req.Code == "" || req.State == "" {
		writeError(w, http.StatusBadRequest, "code and state are required")
		return
	}
	claims, err := h.parseSSOState(req.State)
	if err != nil {
		writeError(w, http.StatusBadRequest, "the login attempt expired or was tampered with")
		return
	}
	wsID, _ := claims["ws"].(string)
	verifier, _ := claims["cv"].(string)
	nonce, _ := claims["nonce"].(string)
	redirectURI, _ := claims["ru"].(string)
	ws, err := h.Queries.GetWorkspace(r.Context(), parseUUID(wsID))
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	conn, err := h.Queries.GetSSOConnection(r.Context(), ws.ID)
	if err != nil || !h.ssoConfigured() {
		writeError(w, http.StatusNotFound, "this workspace has no SSO connection")
		return
	}
	secret, err := h.openSSOSecret(conn.ClientSecretEncrypted)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the SSO secret cannot be read")
		return
	}
	disc, err := oidcDiscover(r.Context(), conn.Issuer)
	if err != nil {
		writeError(w, http.StatusBadGateway, "identity provider discovery failed")
		return
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {req.Code}, "redirect_uri": {redirectURI}, "client_id": {conn.ClientID}, "client_secret": {secret}, "code_verifier": {verifier}}
	tokReq, _ := http.NewRequestWithContext(r.Context(), http.MethodPost, disc.TokenEndpoint, strings.NewReader(form.Encode()))
	tokReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokReq.Header.Set("Accept", "application/json")
	res, err := OIDCHTTPClient.Do(tokReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, "identity provider token exchange failed")
		return
	}
	defer res.Body.Close()
	var tokens struct {
		IDToken string `json:"id_token"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&tokens); err != nil || res.StatusCode != http.StatusOK || tokens.IDToken == "" {
		writeError(w, http.StatusUnauthorized, "the identity provider refused the code")
		return
	}
	keyFunc, err := oidcKeyFunc(r.Context(), disc.JwksURI)
	if err != nil {
		writeError(w, http.StatusBadGateway, "identity provider keys unavailable")
		return
	}
	idTok, err := jwt.Parse(tokens.IDToken, keyFunc, jwt.WithIssuer(conn.Issuer), jwt.WithAudience(conn.ClientID), jwt.WithLeeway(time.Minute))
	if err != nil || !idTok.Valid {
		writeError(w, http.StatusUnauthorized, "the identity token is invalid")
		return
	}
	idClaims, _ := idTok.Claims.(jwt.MapClaims)
	if got, _ := idClaims["nonce"].(string); got != nonce {
		writeError(w, http.StatusUnauthorized, "the identity token does not match this login")
		return
	}
	email, _ := idClaims["email"].(string)
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		writeError(w, http.StatusUnauthorized, "the identity token carries no email")
		return
	}
	if verified, ok := idClaims["email_verified"].(bool); ok && !verified {
		writeError(w, http.StatusUnauthorized, "the identity provider has not verified this email")
		return
	}
	if domains := ssoDomains(conn.AllowedDomains); len(domains) > 0 {
		at := strings.LastIndex(email, "@")
		if at < 0 || !mcpContains(domains, email[at+1:]) {
			writeError(w, http.StatusForbidden, "this email domain is not allowed for the workspace")
			return
		}
	}
	user, _, err := h.findOrCreateUser(r.Context(), email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign in")
		return
	}
	if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{UserID: user.ID, WorkspaceID: ws.ID}); err != nil {
		if !conn.AutoProvision {
			writeError(w, http.StatusForbidden, "you are not a member of this workspace")
			return
		}
		if _, err := h.Queries.CreateMember(r.Context(), db.CreateMemberParams{WorkspaceID: ws.ID, UserID: user.ID, Role: "member"}); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to join the workspace")
			return
		}
	}
	tokenString, err := h.issueJWT(user)
	if err != nil {
		writeError(w, http.StatusForbidden, "sign-in refused")
		return
	}
	if err := auth.SetAuthCookies(w, tokenString); err != nil {
		slog.Warn("sso: set cookies failed", "error", err)
	}
	h.audit(r.Context(), ws.ID, "member", uuidToString(user.ID), AuditSSOLogin, "user", user.ID, map[string]any{"issuer": conn.Issuer}, nil)
	writeJSON(w, http.StatusOK, map[string]any{"token": tokenString, "user": h.userToResponse(user), "workspace_slug": ws.Slug})
}
