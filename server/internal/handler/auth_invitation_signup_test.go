package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func signupInvitation(t *testing.T, email string) string {
	t.Helper()
	return dbfx.Insert(t, "workspace_invitation", testutil.Cols{
		"workspace_id":  parseUUID(testWorkspaceID),
		"inviter_id":    parseUUID(testUserID),
		"invitee_email": email,
		"role":          "member",
		"status":        "pending",
		"expires_at":    testutil.Raw("now() + interval '1 hour'"),
	})
}

func restrictSignupHandler(t *testing.T) *Handler {
	t.Helper()
	// Keep email delivery local even when the developer has configured a provider.
	t.Setenv("RESEND_API_KEY", "")
	t.Setenv("SMTP_HOST", "")
	h := *testHandler
	h.cfg = Config{AllowSignup: false}
	h.EmailService = service.NewEmailService()
	return &h
}

func changeSignupInvitation(t *testing.T, id, state string) {
	t.Helper()
	switch state {
	case "pending":
	case "revoked":
		if _, err := testHandler.Queries.RevokeInvitation(context.Background(), parseUUID(id)); err != nil {
			t.Fatal(err)
		}
	case "expired":
		// Leave status pending to exercise expiry without relying on cleanup jobs.
		dbfx.Exec(t, `UPDATE workspace_invitation SET expires_at = now() - interval '1 hour' WHERE id = $1`, id)
	default:
		dbfx.Exec(t, `UPDATE workspace_invitation SET status = $2 WHERE id = $1`, id, state)
	}
}

// send-code must answer identically for every invitation state, valid or not.
// That indiscernibility IS the guard: a status that varied with the answer
// would let anyone probe who may sign up on a closed instance — the rule
// TestSendCodeDoesNotRevealAccountsWhenSignupIsClosed states for the signup
// flag and the allowlist, applied here to invitations.
//
// What the refusal actually does is checked one test down, at verify-code:
// no account, no session. It cannot be checked here, because a refused email
// still gets its verification_code row on purpose — the rate limiter has to
// behave the same for a refused address too — and the only real difference,
// the outgoing mail, is not observable through the concrete EmailService.
func TestSendCodeAnswersEveryInvitationStateAlike(t *testing.T) {
	var statuses []int
	for _, state := range []string{"pending", "expired", "accepted", "declined", "revoked", "missing"} {
		email := "invite-signup-" + state + "@example.com"
		if state != "missing" {
			id := signupInvitation(t, email)
			changeSignupInvitation(t, id, state)
		}
		dbfx.Cleanup(t, `DELETE FROM verification_code WHERE email = $1`, email)
		h := restrictSignupHandler(t)
		req := testutil.JSONRequest(http.MethodPost, "/auth/send-code", map[string]string{"email": " " + strings.ToUpper(email) + " "})
		statuses = append(statuses, testutil.Call(t, h.SendCode, req).Want(http.StatusOK).Result().StatusCode)
	}
	for i, got := range statuses {
		if got != statuses[0] {
			t.Errorf("state %d answered %d, first state answered %d — the status tells them apart", i, got, statuses[0])
		}
	}
}

func TestVerifyCodeRechecksSignupInvitation(t *testing.T) {
	for _, state := range []string{"pending", "expired", "revoked", "declined", "accepted"} {
		t.Run(state, func(t *testing.T) {
			email := "invite-recheck-" + state + "@example.com"
			id := signupInvitation(t, email)
			dbfx.Cleanup(t, `DELETE FROM "user" WHERE email = $1`, email)
			dbfx.Cleanup(t, `DELETE FROM verification_code WHERE email = $1`, email)
			h := restrictSignupHandler(t)
			testutil.Call(t, h.SendCode, testutil.JSONRequest(http.MethodPost, "/auth/send-code", map[string]string{"email": email})).Want(http.StatusOK)
			code, err := h.Queries.GetLatestVerificationCode(context.Background(), email)
			if err != nil {
				t.Fatal(err)
			}
			changeSignupInvitation(t, id, state)
			want := http.StatusForbidden
			if state == "pending" {
				want = http.StatusOK
			}
			resp := testutil.Call(t, h.VerifyCode, testutil.JSONRequest(http.MethodPost, "/auth/verify-code", map[string]string{"email": email, "code": code.Code})).Want(want)
			count := dbfx.Count(t, `SELECT count(*) FROM "user" WHERE email = $1`, email)
			if state != "pending" {
				if count != 0 || len(resp.Result().Cookies()) != 0 {
					t.Fatal("invalidated invitation created an account or authenticated session")
				}
				return
			}
			if count != 1 {
				t.Fatalf("expected one new account, got %d", count)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM member WHERE user_id = (SELECT id FROM "user" WHERE email = $1)`, email); n != 0 {
				t.Fatal("signup must not accept the invitation or grant workspace membership")
			}
			changeSignupInvitation(t, id, "revoked")
			_, isNew, err := h.findOrCreateUser(context.Background(), email)
			if err != nil || isNew {
				t.Fatalf("revocation must not block the existing account: isNew=%t, err=%v", isNew, err)
			}
		})
	}
}

func TestGoogleLoginInvitationSignup(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "test-client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")
	for _, invited := range []bool{false, true} {
		t.Run(fmt.Sprintf("invited=%t", invited), func(t *testing.T) {
			email := fmt.Sprintf("google-invited-%t@other.com", invited)
			if invited {
				signupInvitation(t, email)
			}
			dbfx.Cleanup(t, `DELETE FROM "user" WHERE email = $1`, email)
			h := *testHandler
			h.cfg = Config{AllowSignup: true, AllowedEmailDomains: []string{"company.com"}}
			h.googleOAuthHTTPClient = &http.Client{Transport: googleRoundTripper(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Host {
				case "oauth2.googleapis.com":
					return googleResponse(req, http.StatusOK, `{"access_token":"test-token"}`), nil
				case "www.googleapis.com":
					return googleResponse(req, http.StatusOK, fmt.Sprintf(`{"email":%q}`, " "+strings.ToUpper(email)+" ")), nil
				default:
					t.Fatalf("unexpected Google request: %s", req.URL)
					return nil, nil
				}
			})}
			want := http.StatusForbidden
			if invited {
				want = http.StatusOK
			}
			resp := testutil.Call(t, h.GoogleLogin, googleLoginRequest(`{"code":"test-code","redirect_uri":"http://localhost/auth/callback"}`)).Want(want)
			if invited {
				var login LoginResponse
				resp.JSON(&login)
				if login.Token == "" || login.User.Email != email {
					t.Fatalf("expected authenticated invited account, got %+v", login.User)
				}
			} else {
				// GoogleLogin always clears the state cookie, win or lose; only a
				// real session cookie means an uninvited user got authenticated.
				for _, cookie := range resp.Result().Cookies() {
					if cookie.Name != auth.GoogleOAuthStateCookieName {
						t.Fatalf("uninvited user must not get a session, got cookie %q", cookie.Name)
					}
				}
				if dbfx.Count(t, `SELECT count(*) FROM "user" WHERE email = $1`, email) != 0 {
					t.Fatal("uninvited user must not get an account")
				}
			}
		})
	}
}

// Delegate every real query except the failing invitation lookup. This pins
// HTTP error handling and proves no account or session is created on failure.
type failingSignupInvitationDB struct{ db.DBTX }

func (m failingSignupInvitationDB) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	if strings.HasPrefix(sql, "-- name: HasPendingInvitationForEmail :one\n") {
		return &mockRow{err: context.Canceled}
	}
	return m.DBTX.QueryRow(ctx, sql, args...)
}

func TestSignupInvitationLookupFailure(t *testing.T) {
	t.Setenv("GOOGLE_CLIENT_ID", "test-client")
	t.Setenv("GOOGLE_CLIENT_SECRET", "test-secret")
	for _, path := range []string{"send-code", "verify-code", "google"} {
		t.Run(path, func(t *testing.T) {
			email := "invite-lookup-error-" + path + "@example.com"
			signupInvitation(t, email)
			h := *testHandler
			h.cfg = Config{AllowSignup: false}
			h.Queries = db.New(failingSignupInvitationDB{testPool})
			handler := h.SendCode
			body := map[string]string{"email": email}
			switch path {
			case "verify-code":
				handler = h.VerifyCode
				body["code"] = "123456"
				dbfx.Insert(t, "verification_code", testutil.Cols{
					"email": email, "code": body["code"], "expires_at": testutil.Raw("now() + interval '10 minutes'"),
				})
			case "google":
				handler = h.GoogleLogin
				body = map[string]string{"code": "test-code", "redirect_uri": "http://localhost/auth/callback"}
				h.googleOAuthHTTPClient = &http.Client{Transport: googleRoundTripper(func(req *http.Request) (*http.Response, error) {
					switch req.URL.Host {
					case "oauth2.googleapis.com":
						return googleResponse(req, http.StatusOK, `{"access_token":"test-token"}`), nil
					case "www.googleapis.com":
						return googleResponse(req, http.StatusOK, fmt.Sprintf(`{"email":%q}`, email)), nil
					default:
						t.Fatalf("unexpected Google request: %s", req.URL)
						return nil, nil
					}
				})}
			}
			dbfx.Cleanup(t, `DELETE FROM "user" WHERE email = $1`, email)
			req := testutil.JSONRequest(http.MethodPost, "/auth/"+path, body)
			if path == "google" {
				req = googleLoginRequest(`{"code":"test-code","redirect_uri":"http://localhost/auth/callback"}`)
			}
			resp := testutil.Call(t, handler, req).Want(http.StatusInternalServerError)
			// GoogleLogin always clears the state cookie, win or lose; only a
			// real session cookie means the failure still authenticated someone.
			for _, cookie := range resp.Result().Cookies() {
				if cookie.Name != auth.GoogleOAuthStateCookieName {
					t.Fatalf("invitation lookup failure must not create a session, got cookie %q", cookie.Name)
				}
			}
			if dbfx.Count(t, `SELECT count(*) FROM "user" WHERE email = $1`, email) != 0 {
				t.Fatal("invitation lookup failure must not create an account")
			}
		})
	}
}
