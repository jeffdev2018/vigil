package handler

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// The OIDC flow end to end (connection lifecycle, enforcement, start/callback,
// provisioning) lives in access_k60_test.go, always with `email_verified: true`.
// OIDC Core makes the claim optional and some providers emit it as a string.
// OIDCCallback refuses only a boolean false: a missing claim, or one it cannot
// read as a bool, does not block the login. These cases document that
// behaviour as it stands; loosening or tightening it is a product decision.
func TestOIDCCallbackEmailVerifiedClaimShapes(t *testing.T) {
	prevBox, prevClient := testHandler.SSOSecretBox, OIDCHTTPClient
	t.Cleanup(func() { testHandler.SSOSecretBox, OIDCHTTPClient = prevBox, prevClient })
	box, err := secretbox.New(bytes.Repeat([]byte("s"), secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	testHandler.SSOSecretBox = box
	dbfx.Cleanup(t, `DELETE FROM workspace_sso_connection WHERE workspace_id = $1`, testWorkspaceID)
	var slug string
	dbfx.QueryRow(t, `SELECT slug FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&slug)
	ws := func(req *http.Request) *http.Request { return testutil.WithURLParams(req, "id", testWorkspaceID) }

	cases := []struct {
		name   string
		claims jwt.MapClaims
		want   int
	}{
		{name: "claim absent signs in", claims: nil, want: http.StatusOK},
		{name: "string false is refused like the boolean (#355)", claims: jwt.MapClaims{"email_verified": "false"}, want: http.StatusUnauthorized},
		{name: "boolean false is refused", claims: jwt.MapClaims{"email_verified": false}, want: http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			email := "sso-verified-" + uuid.NewString()[:8] + "@example.test"
			dbfx.Cleanup(t, `DELETE FROM member WHERE workspace_id = $1 AND user_id IN (SELECT id FROM "user" WHERE email = $2)`, testWorkspaceID, email)
			dbfx.Cleanup(t, `DELETE FROM "user" WHERE email = $1`, email)
			claims := jwt.MapClaims{"email": email}
			for k, v := range tc.claims {
				claims[k] = v
			}
			idp := fakeOIDCProviderWithClaims(t, "client-1", claims)
			OIDCHTTPClient = idp.Client()
			// Each case has its own issuer, so the connection is re-pointed.
			testutil.Call(t, testHandler.PutSSOConnection, ws(newRequest(http.MethodPut, "/x", map[string]any{"issuer": idp.URL, "client_id": "client-1", "client_secret": "s3cret"}))).Want(http.StatusOK)

			var start struct {
				AuthorizationURL string `json:"authorization_url"`
			}
			testutil.Call(t, testHandler.OIDCStart, testutil.JSONRequest(http.MethodPost, "/auth/oidc/start", map[string]string{"workspace_slug": slug, "redirect_uri": "https://app.example/login/sso"})).Want(http.StatusOK).JSON(&start)
			authURL, err := url.Parse(start.AuthorizationURL)
			if err != nil {
				t.Fatalf("authorization url %q: %v", start.AuthorizationURL, err)
			}
			if _, err := idp.Client().Get(idp.URL + "/nonce?v=" + url.QueryEscape(authURL.Query().Get("nonce"))); err != nil {
				t.Fatalf("register the nonce with the fake provider: %v", err)
			}

			res := testutil.Call(t, testHandler.OIDCCallback, testutil.JSONRequest(http.MethodPost, "/auth/oidc/callback", map[string]string{"code": "good-code", "state": authURL.Query().Get("state")})).Want(tc.want)
			provisioned := dbfx.Count(t, `SELECT COUNT(*) FROM member m JOIN "user" u ON u.id = m.user_id WHERE m.workspace_id = $1 AND u.email = $2`, testWorkspaceID, email)
			if tc.want == http.StatusOK {
				var login struct {
					Token string `json:"token"`
				}
				res.JSON(&login)
				if login.Token == "" || provisioned != 1 {
					t.Fatalf("login: token %q, provisioned %d: %s", login.Token, provisioned, res.Body.String())
				}
				return
			}
			if !strings.Contains(res.Body.String(), "not verified") || provisioned != 0 {
				t.Fatalf("refusal: provisioned %d: %s", provisioned, res.Body.String())
			}
		})
	}
}
