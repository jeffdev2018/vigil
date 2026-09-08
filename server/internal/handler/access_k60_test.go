package handler

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// K60: project roles only restrict the workspace role and are inherited by
// default; SCIM provisions and deprovisions members with a dedicated token
// and revokes sessions in the same request; an enforced OIDC connection
// closes the code login and signs members in through the provider.

func TestProjectRoles(t *testing.T) {
	project := dbfx.Project(t, "roles project "+uuid.NewString()[:6])
	member := dbfx.User(t, "role member", "role-member-"+uuid.NewString()[:6]+"@example.test")
	memberID := dbfx.Member(t, testWorkspaceID, member, "member")
	agentID := dbfx.Agent(t, "role agent "+uuid.NewString()[:6], handlerTestRuntimeID(t))
	proj := func(req *http.Request, more ...string) *http.Request {
		return testutil.WithURLParams(req, append([]string{"id", project}, more...)...)
	}
	var list struct {
		Members []ProjectMemberRoleResponse `json:"members"`
	}
	find := func(subjectType, id string) ProjectMemberRoleResponse {
		for _, m := range list.Members {
			if m.SubjectType == subjectType && m.SubjectID == id {
				return m
			}
		}
		t.Fatalf("subject %s %s missing", subjectType, id)
		return ProjectMemberRoleResponse{}
	}
	testutil.Call(t, testHandler.ListProjectMembers, proj(newRequest(http.MethodGet, "/x", nil))).Want(http.StatusOK).JSON(&list)
	if m := find("member", memberID); m.EffectiveRole != "contributor" || m.Source != "inherited" || m.Ceiling != "contributor" {
		t.Fatalf("a member inherits contributor: %+v", m)
	}
	if a := find("agent", agentID); a.EffectiveRole != "contributor" || a.Ceiling != "contributor" {
		t.Fatalf("an agent inherits contributor: %+v", a)
	}
	// A member cannot be raised to admin; a viewer override sticks.
	testutil.Call(t, testHandler.SetProjectMemberRole, proj(newRequest(http.MethodPut, "/x", map[string]any{"role": "admin"}), "subjectType", "member", "subjectId", memberID)).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.SetProjectMemberRole, proj(newRequest(http.MethodPut, "/x", map[string]any{"role": "viewer"}), "subjectType", "member", "subjectId", memberID)).Want(http.StatusOK).JSON(&list)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM project_member_role WHERE project_id = $1`, project)
	})
	if m := find("member", memberID); m.EffectiveRole != "viewer" || m.Source != "override" {
		t.Fatalf("viewer override: %+v", m)
	}
	// The viewer cannot write in the project, still reads it, and cannot set roles.
	issue := dbfx.Issue(t, "roles issue "+uuid.NewString()[:6], testutil.Cols{"project_id": project})
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequestAs(member, http.MethodPut, "/api/issues/"+issue, map[string]any{"title": "nope"}), "id", issue)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.CreateComment, withURLParam(newRequestAs(member, http.MethodPost, "/api/issues/"+issue+"/comments", map[string]any{"content": "hi"}), "id", issue)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.CreateIssue, newRequestAs(member, http.MethodPost, "/api/issues", map[string]any{"title": "new", "project_id": project})).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.UpdateProject, proj(newRequestAs(member, http.MethodPut, "/x", map[string]any{"title": "renamed"}))).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.GetIssue, withURLParam(newRequestAs(member, http.MethodGet, "/api/issues/"+issue, nil), "id", issue)).Want(http.StatusOK)
	testutil.Call(t, testHandler.SetProjectMemberRole, proj(newRequestAs(member, http.MethodPut, "/x", map[string]any{"role": "contributor"}), "subjectType", "member", "subjectId", memberID)).Want(http.StatusForbidden)
	// Outside the project the member writes as before.
	other := dbfx.Issue(t, "roles free issue "+uuid.NewString()[:6])
	testutil.Call(t, testHandler.UpdateIssue, withURLParam(newRequestAs(member, http.MethodPut, "/api/issues/"+other, map[string]any{"title": "ok"}), "id", other)).Want(http.StatusOK)
	// An agent set to viewer is refused through its run token.
	testutil.Call(t, testHandler.SetProjectMemberRole, proj(newRequest(http.MethodPut, "/x", map[string]any{"role": "viewer"}), "subjectType", "agent", "subjectId", agentID)).Want(http.StatusOK)
	task := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": handlerTestRuntimeID(t), "issue_id": issue, "status": "running"})
	testutil.Call(t, testHandler.CreateComment, withURLParam(runRequest(agentID, task, http.MethodPost, "/api/issues/"+issue+"/comments", map[string]any{"content": "agent"}), "id", issue)).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.ClearProjectMemberRole, proj(newRequest(http.MethodDelete, "/x", nil), "subjectType", "agent", "subjectId", agentID)).Want(http.StatusOK)
	testutil.Call(t, testHandler.CreateComment, withURLParam(runRequest(agentID, task, http.MethodPost, "/api/issues/"+issue+"/comments", map[string]any{"content": "agent"}), "id", issue)).Want(http.StatusCreated)
	// Back to inherited for the member.
	testutil.Call(t, testHandler.ClearProjectMemberRole, proj(newRequest(http.MethodDelete, "/x", nil), "subjectType", "member", "subjectId", memberID)).Want(http.StatusOK).JSON(&list)
	if m := find("member", memberID); m.Source != "inherited" || m.EffectiveRole != "contributor" {
		t.Fatalf("cleared: %+v", m)
	}
}

func scimRequest(method, path, token string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/scim+json")
	return req
}

func TestScimProvisioning(t *testing.T) {
	ctx := context.Background()
	prev := middleware.Revocations
	middleware.Revocations = auth.NewSessionRevocations(nil, func(ctx context.Context, userID string) (time.Time, bool, error) {
		id := parseUUID(userID)
		at, err := testHandler.Queries.GetUserSessionsInvalidatedAt(ctx, id)
		if err != nil {
			return time.Time{}, false, err
		}
		return at.Time, at.Valid, nil
	})
	t.Cleanup(func() { middleware.Revocations = prev })
	ws := func(req *http.Request, more ...string) *http.Request {
		return testutil.WithURLParams(req, append([]string{"id", testWorkspaceID}, more...)...)
	}
	var tok ScimTokenResponse
	res := testutil.Call(t, testHandler.CreateScimToken, ws(newRequest(http.MethodPost, "/x", nil))).Want(http.StatusCreated)
	res.JSON(&tok)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM scim_token WHERE workspace_id = $1`, testWorkspaceID) })
	if !strings.HasPrefix(tok.Token, "scim_") || tok.Hint == "" {
		t.Fatalf("token issued once: %+v", tok)
	}
	var listed struct {
		Tokens []ScimTokenResponse `json:"tokens"`
	}
	res = testutil.Call(t, testHandler.ListScimTokens, ws(newRequest(http.MethodGet, "/x", nil))).Want(http.StatusOK)
	res.JSON(&listed)
	if strings.Contains(res.Body.String(), tok.Token) || len(listed.Tokens) != 1 {
		t.Fatal("the token value is shown once only")
	}
	scim := middleware.SCIMBearerOnly(testHandler.Queries)
	call := func(handler http.HandlerFunc, req *http.Request, params ...string) *testutil.Response {
		return testutil.Call(t, scim(http.HandlerFunc(handler)).ServeHTTP, testutil.WithURLParams(req, params...))
	}
	call(testHandler.ScimListUsers, scimRequest(http.MethodGet, "/scim/v2/Users", "scim_bogus", nil)).Want(http.StatusUnauthorized)
	email := "scim-" + uuid.NewString()[:6] + "@example.test"
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email) })
	var created scimUser
	res = call(testHandler.ScimCreateUser, scimRequest(http.MethodPost, "/scim/v2/Users", tok.Token, map[string]any{"schemas": []string{scimUserSchema}, "userName": email, "externalId": "okta-1", "name": map[string]string{"givenName": "Ada", "familyName": "Lovelace"}, "emails": []map[string]any{{"value": email, "primary": true}}, "active": true})).Want(http.StatusCreated)
	res.JSON(&created)
	if created.UserName != email || created.ExternalID != "okta-1" || created.Name.Formatted != "Ada Lovelace" || !created.Active {
		t.Fatalf("created: %+v", created)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM member m JOIN "user" u ON u.id = m.user_id WHERE m.workspace_id = $1 AND u.email = $2 AND m.role = 'member' AND m.scim_external_id = 'okta-1'`, testWorkspaceID, email) != 1 {
		t.Fatal("the member is provisioned")
	}
	call(testHandler.ScimCreateUser, scimRequest(http.MethodPost, "/scim/v2/Users", tok.Token, map[string]any{"userName": email})).Want(http.StatusConflict)
	var page struct {
		TotalResults int        `json:"totalResults"`
		Resources    []scimUser `json:"Resources"`
	}
	call(testHandler.ScimListUsers, scimRequest(http.MethodGet, "/scim/v2/Users?filter="+url.QueryEscape(`userName eq "`+email+`"`), tok.Token, nil)).Want(http.StatusOK).JSON(&page)
	if page.TotalResults != 1 || page.Resources[0].ID != created.ID {
		t.Fatalf("filter: %+v", page)
	}
	// The user holds a session; deprovisioning refuses it in the same request.
	var userID string
	dbfx.QueryRow(t, `SELECT id::text FROM "user" WHERE email = $1`, email).Scan(&userID)
	session, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": userID, "email": email, "iat": time.Now().Add(-time.Minute).Unix(), "exp": time.Now().Add(time.Hour).Unix()}).SignedString(auth.JWTSecret())
	authed := middleware.Auth(testHandler.Queries, nil, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	probe := func() int {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.Header.Set("Authorization", "Bearer "+session)
		w := httptest.NewRecorder()
		authed.ServeHTTP(w, req)
		return w.Code
	}
	if probe() != http.StatusOK {
		t.Fatal("the session works before deprovisioning")
	}
	var patched scimUser
	call(testHandler.ScimPatchUser, scimRequest(http.MethodPatch, "/scim/v2/Users/"+created.ID, tok.Token, map[string]any{"schemas": []string{scimPatchSchema}, "Operations": []map[string]any{{"op": "replace", "value": map[string]any{"active": false}}}}), "id", created.ID).Want(http.StatusOK).JSON(&patched)
	if patched.Active {
		t.Fatal("patched inactive")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM member WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, userID) != 0 {
		t.Fatal("the membership is gone")
	}
	if probe() != http.StatusUnauthorized {
		t.Fatal("the session is revoked at once")
	}
	fresh, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": userID, "email": email, "iat": time.Now().Add(2 * time.Second).Unix(), "exp": time.Now().Add(time.Hour).Unix()}).SignedString(auth.JWTSecret())
	session = fresh
	if probe() != http.StatusOK {
		t.Fatal("a session minted after the revocation is accepted")
	}
	call(testHandler.ScimGetUser, scimRequest(http.MethodGet, "/scim/v2/Users/"+created.ID, tok.Token, nil), "id", created.ID).Want(http.StatusNotFound)
	if dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE workspace_id = $1 AND action IN ('scim.provisioned', 'scim.deprovisioned')`, testWorkspaceID) < 2 {
		t.Fatal("provisioning is audited")
	}
	// A rotated token retires the previous one.
	var second ScimTokenResponse
	testutil.Call(t, testHandler.CreateScimToken, ws(newRequest(http.MethodPost, "/x", nil))).Want(http.StatusCreated).JSON(&second)
	call(testHandler.ScimListUsers, scimRequest(http.MethodGet, "/scim/v2/Users", tok.Token, nil)).Want(http.StatusUnauthorized)
	call(testHandler.ScimListUsers, scimRequest(http.MethodGet, "/scim/v2/Users", second.Token, nil)).Want(http.StatusOK)
	member := dbfx.User(t, "scim viewer", "scim-viewer-"+uuid.NewString()[:6]+"@example.test")
	dbfx.Member(t, testWorkspaceID, member, "admin")
	testutil.Call(t, testHandler.CreateScimToken, ws(newRequestAs(member, http.MethodPost, "/x", nil))).Want(http.StatusForbidden)
}

// fakeOIDCProvider answers discovery, JWKS and the token endpoint, signing an
// id_token for the email it was told to vouch for.
func fakeOIDCProvider(t *testing.T, clientID string, email string) *httptest.Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	var srv *httptest.Server
	nonce := ""
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize", "token_endpoint": srv.URL + "/token", "jwks_uri": srv.URL + "/jwks"})
		case "/jwks":
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]any{{"kty": "RSA", "kid": "k1", "n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()), "e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes())}}})
		case "/token":
			_ = r.ParseForm()
			if r.Form.Get("code") != "good-code" || r.Form.Get("client_secret") != "s3cret" || r.Form.Get("code_verifier") == "" {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{"iss": srv.URL, "aud": clientID, "sub": "idp-user", "email": email, "email_verified": true, "nonce": nonce, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix()})
			tok.Header["kid"] = "k1"
			signed, _ := tok.SignedString(key)
			_ = json.NewEncoder(w).Encode(map[string]any{"id_token": signed, "access_token": "x", "token_type": "Bearer"})
		case "/nonce":
			nonce = r.URL.Query().Get("v")
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSSOEnforcementAndOIDCLogin(t *testing.T) {
	ctx := context.Background()
	prevBox, prevClient := testHandler.SSOSecretBox, OIDCHTTPClient
	t.Cleanup(func() { testHandler.SSOSecretBox, OIDCHTTPClient = prevBox, prevClient })
	box, _ := secretbox.New(bytes.Repeat([]byte("s"), secretbox.KeySize))
	testHandler.SSOSecretBox = box
	domain := "sso-" + uuid.NewString()[:6] + ".example"
	email := "ada@" + domain
	idp := fakeOIDCProvider(t, "client-1", email)
	OIDCHTTPClient = idp.Client()
	ws := func(req *http.Request) *http.Request { return testutil.WithURLParams(req, "id", testWorkspaceID) }
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM workspace_sso_connection WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM member WHERE workspace_id = $1 AND user_id IN (SELECT id FROM "user" WHERE email = $2)`, testWorkspaceID, email)
		testPool.Exec(ctx, `DELETE FROM "user" WHERE email = $1`, email)
	})
	admin := dbfx.User(t, "sso admin", "sso-admin-"+uuid.NewString()[:6]+"@example.test")
	dbfx.Member(t, testWorkspaceID, admin, "admin")
	testutil.Call(t, testHandler.PutSSOConnection, ws(newRequestAs(admin, http.MethodPut, "/x", map[string]any{"issuer": idp.URL, "client_id": "client-1", "client_secret": "s3cret"}))).Want(http.StatusForbidden)
	var conn struct {
		Connection SSOConnectionResponse `json:"connection"`
	}
	res := testutil.Call(t, testHandler.PutSSOConnection, ws(newRequest(http.MethodPut, "/x", map[string]any{"issuer": idp.URL, "client_id": "client-1", "client_secret": "s3cret", "allowed_domains": []string{domain}}))).Want(http.StatusOK)
	res.JSON(&conn)
	if strings.Contains(res.Body.String(), "s3cret") || !conn.Connection.HasSecret || conn.Connection.Enforced {
		t.Fatalf("connection: %s", res.Body.String())
	}
	var stored string
	dbfx.QueryRow(t, `SELECT client_secret_encrypted FROM workspace_sso_connection WHERE workspace_id = $1`, testWorkspaceID).Scan(&stored)
	if strings.Contains(stored, "s3cret") {
		t.Fatal("the secret is encrypted at rest")
	}
	// Code login still works until enforced; then it is refused for the domain.
	dbfx.Insert(t, "verification_code", testutil.Cols{"email": email, "code": "123456", "expires_at": testutil.Raw("now() + interval '10 minutes'")})
	testutil.Call(t, testHandler.SetSSOEnforced, ws(newRequest(http.MethodPut, "/x", map[string]any{"enforced": true}))).Want(http.StatusOK)
	res = testutil.Call(t, testHandler.VerifyCode, testutil.JSONRequest(http.MethodPost, "/auth/verify-code", map[string]string{"email": email, "code": "123456"})).Want(http.StatusForbidden)
	if !strings.Contains(res.Body.String(), "sso_required") {
		t.Fatalf("code login refused with the workspace to use: %s", res.Body.String())
	}
	// The OIDC flow signs the user in and provisions the membership.
	var start struct {
		AuthorizationURL string `json:"authorization_url"`
	}
	var slug string
	dbfx.QueryRow(t, `SELECT slug FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&slug)
	testutil.Call(t, testHandler.OIDCStart, testutil.JSONRequest(http.MethodPost, "/auth/oidc/start", map[string]string{"workspace_slug": slug, "redirect_uri": "https://app.example/login/sso"})).Want(http.StatusOK).JSON(&start)
	authURL, err := url.Parse(start.AuthorizationURL)
	if err != nil || !strings.HasPrefix(start.AuthorizationURL, idp.URL+"/authorize?") || authURL.Query().Get("code_challenge_method") != "S256" || authURL.Query().Get("client_id") != "client-1" {
		t.Fatalf("authorization url: %s", start.AuthorizationURL)
	}
	state := authURL.Query().Get("state")
	http.Get(idp.URL + "/nonce?v=" + url.QueryEscape(authURL.Query().Get("nonce")))
	testutil.Call(t, testHandler.OIDCCallback, testutil.JSONRequest(http.MethodPost, "/auth/oidc/callback", map[string]string{"code": "bad-code", "state": state})).Want(http.StatusUnauthorized)
	testutil.Call(t, testHandler.OIDCCallback, testutil.JSONRequest(http.MethodPost, "/auth/oidc/callback", map[string]string{"code": "good-code", "state": "tampered"})).Want(http.StatusBadRequest)
	var login struct {
		Token         string `json:"token"`
		WorkspaceSlug string `json:"workspace_slug"`
	}
	testutil.Call(t, testHandler.OIDCCallback, testutil.JSONRequest(http.MethodPost, "/auth/oidc/callback", map[string]string{"code": "good-code", "state": state})).Want(http.StatusOK).JSON(&login)
	if login.Token == "" || login.WorkspaceSlug != slug {
		t.Fatalf("login: %+v", login)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM member m JOIN "user" u ON u.id = m.user_id WHERE m.workspace_id = $1 AND u.email = $2`, testWorkspaceID, email) != 1 {
		t.Fatal("the member is provisioned on first login")
	}
	// A replayed state is refused? It is still valid until it expires; a
	// second exchange with a used code is the provider's business. The
	// connection can be removed by the owner only.
	testutil.Call(t, testHandler.DeleteSSOConnection, ws(newRequestAs(admin, http.MethodDelete, "/x", nil))).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.DeleteSSOConnection, ws(newRequest(http.MethodDelete, "/x", nil))).Want(http.StatusNoContent)
	dbfx.Insert(t, "verification_code", testutil.Cols{"email": email, "code": "654321", "expires_at": testutil.Raw("now() + interval '10 minutes'")})
	testutil.Call(t, testHandler.VerifyCode, testutil.JSONRequest(http.MethodPost, "/auth/verify-code", map[string]string{"email": email, "code": "654321"})).Want(http.StatusOK)
}
