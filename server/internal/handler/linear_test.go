package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/linear"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// Linear Bridge (K21) HTTP surface. The bridge's own rules live in
// internal/integrations/linear/sync_test.go; this file covers the endpoints:
// what they answer when the deployment is unconfigured, that the OAuth
// handshake persists an installation, and that the public webhook refuses an
// unsigned delivery.

// stubLinearAPI is the Linear side of the callback: enough for Viewer +
// webhookCreate, which is all the install path calls.
type stubLinearAPI struct {
	viewerErr  error
	webhookErr error
}

func (s stubLinearAPI) Viewer(context.Context) (linear.Viewer, error) {
	if s.viewerErr != nil {
		return linear.Viewer{}, s.viewerErr
	}
	return linear.Viewer{ID: "user_app", OrganizationID: "org_k21", OrganizationName: "Acme"}, nil
}
func (s stubLinearAPI) Issue(context.Context, string) (linear.Issue, error) {
	return linear.Issue{ID: "iss_1", Identifier: "ENG-1", Title: "Resynced", Team: struct {
		ID string `json:"id"`
	}{ID: "team_1"}}, nil
}
func (s stubLinearAPI) CreateComment(context.Context, string, string) (string, error) {
	return "c_remote", nil
}
func (s stubLinearAPI) UpdateIssueState(context.Context, string, string) error { return nil }
func (s stubLinearAPI) ListTeamStates(context.Context, string) ([]linear.WorkflowState, error) {
	return []linear.WorkflowState{{ID: "st_todo", Type: "unstarted"}}, nil
}
func (s stubLinearAPI) CreateWebhook(context.Context, string, string, []string) (string, error) {
	if s.webhookErr != nil {
		return "", s.webhookErr
	}
	return "wh_1", nil
}
func (s stubLinearAPI) DeleteWebhook(context.Context, string) error { return nil }

// enableLinear wires the handler for the test and restores it afterwards, so
// the rest of the suite still sees an unconfigured deployment.
func enableLinear(t *testing.T, tokenURL string) {
	t.Helper()
	box, err := secretbox.New(make([]byte, 32))
	if err != nil {
		t.Fatalf("secretbox: %v", err)
	}
	prev := struct {
		bridge  *linear.Bridge
		box     *secretbox.Box
		oauth   linear.OAuthConfig
		secret  []byte
		public  string
		app     string
		factory func(string) linear.API
	}{testHandler.Linear, testHandler.LinearSecretBox, testHandler.LinearOAuth,
		testHandler.LinearStateSecret, testHandler.LinearPublicURL, testHandler.LinearAppURL,
		testHandler.LinearClientFactory}

	testHandler.LinearSecretBox = box
	testHandler.LinearStateSecret = []byte("linear-test-state-secret")
	testHandler.LinearPublicURL = "https://api.example.com"
	testHandler.LinearAppURL = "https://app.example.com"
	testHandler.LinearOAuth = linear.OAuthConfig{
		ClientID:     "cid",
		ClientSecret: "csecret",
		RedirectURI:  "https://api.example.com/api/integrations/linear/oauth/callback",
		TokenURL:     tokenURL,
	}
	testHandler.LinearClientFactory = func(string) linear.API { return stubLinearAPI{} }
	bridge := linear.NewBridge(testHandler.Queries, box.Open, testHandler, nil)
	bridge.SetClientFactory(func(string) linear.API { return stubLinearAPI{} })
	testHandler.Linear = bridge

	t.Cleanup(func() {
		testHandler.Linear = prev.bridge
		testHandler.LinearSecretBox = prev.box
		testHandler.LinearOAuth = prev.oauth
		testHandler.LinearStateSecret = prev.secret
		testHandler.LinearPublicURL = prev.public
		testHandler.LinearAppURL = prev.app
		testHandler.LinearClientFactory = prev.factory
	})
}

// linearCleanup removes the rows a Linear test creates outside the fixture.
func linearCleanup(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM linear_comment_link WHERE installation_id IN (SELECT id FROM linear_installation WHERE workspace_id = $1)`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM linear_issue_link WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM linear_installation WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1 AND origin_type = 'linear')`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND origin_type = 'linear'`, testWorkspaceID)
	})
}

func linearWorkspaceCall(t *testing.T, h http.HandlerFunc, method, path string, body any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, h, withURLParam(newRequest(method, path, body), "id", testWorkspaceID))
}

// linearTokenServer stands in for https://api.linear.app/oauth/token.
func linearTokenServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"lin_oauth_token","token_type":"Bearer"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLinearInstallationUnconfiguredDeployment(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	var resp LinearInstallationResponse
	linearWorkspaceCall(t, testHandler.GetLinearInstallation, http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/linear/installation", nil).Want(http.StatusOK).JSON(&resp)

	if resp.Connected {
		t.Fatal("reported connected with no installation")
	}
	// The status map still comes back so the settings tab can render the
	// editor before anything is connected.
	if resp.StatusMap["started"] != "in_progress" {
		t.Fatalf("default status map = %v, want started -> in_progress", resp.StatusMap)
	}
	if len(resp.LinearStateTypes) == 0 {
		t.Fatal("linear_state_types is empty; the status-map editor has no columns to offer")
	}
}

func TestLinearOAuthStartRejectsForeignAgent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableLinear(t, "")

	otherWS := dbfx.Workspace(t, "Linear Other", "linear-other-"+time.Now().Format("150405.000"))
	foreignFixture := testutil.New(testPool, otherWS, testUserID)
	foreignAgent := foreignFixture.Agent(t, "Foreign", "")

	linearWorkspaceCall(t, testHandler.StartLinearOAuth, http.MethodPost,
		"/api/workspaces/"+testWorkspaceID+"/linear/oauth/start",
		map[string]any{"agent_id": foreignAgent}).Want(http.StatusNotFound)
}

func TestLinearOAuthStartReturnsAuthorizeURL(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableLinear(t, "")
	runtimeID := dbfx.Runtime(t, "linear-rt")
	agentID := dbfx.Agent(t, "Linear Agent", runtimeID)

	var out struct {
		AuthorizeURL string `json:"authorize_url"`
	}
	linearWorkspaceCall(t, testHandler.StartLinearOAuth, http.MethodPost,
		"/api/workspaces/"+testWorkspaceID+"/linear/oauth/start",
		map[string]any{"agent_id": agentID}).Want(http.StatusOK).JSON(&out)

	if !strings.Contains(out.AuthorizeURL, "client_id=cid") || !strings.Contains(out.AuthorizeURL, "actor=app") {
		t.Fatalf("authorize_url = %q, want the app's client_id and actor=app", out.AuthorizeURL)
	}
	// The state must round-trip: it is the only thing the public callback has
	// to say which workspace and agent approved.
	state := ""
	for _, part := range strings.Split(out.AuthorizeURL, "&") {
		if strings.HasPrefix(part, "state=") {
			state = strings.TrimPrefix(part, "state=")
		}
	}
	claims, err := linear.VerifyState(testHandler.LinearStateSecret, state, time.Now())
	if err != nil {
		t.Fatalf("state does not verify: %v", err)
	}
	if claims.WorkspaceID != testWorkspaceID || claims.AgentID != agentID {
		t.Fatalf("state claims = %+v, want this workspace and agent", claims)
	}
}

func TestLinearOAuthCallbackPersistsInstallation(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	tokenSrv := linearTokenServer(t)
	enableLinear(t, tokenSrv.URL)
	linearCleanup(t)
	runtimeID := dbfx.Runtime(t, "linear-rt-cb")
	agentID := dbfx.Agent(t, "Linear Agent CB", runtimeID)

	state, err := linear.SignState(testHandler.LinearStateSecret, linear.StateClaims{
		WorkspaceID: testWorkspaceID, UserID: testUserID, AgentID: agentID,
		Redirect: "/settings/integrations", Exp: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("sign state: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/integrations/linear/oauth/callback?code=abc&state="+state, nil)
	resp := testutil.Call(t, testHandler.LinearOAuthCallback, req).Want(http.StatusFound)
	if location := resp.Header().Get("Location"); !strings.Contains(location, "linear=connected") {
		t.Fatalf("redirect Location = %q, want linear=connected", location)
	}

	var orgID, actor, status string
	dbfx.QueryRow(t, `SELECT linear_org_id, actor_user_id, status FROM linear_installation WHERE workspace_id = $1`, testWorkspaceID).
		Scan(&orgID, &actor, &status)
	if orgID != "org_k21" || actor != "user_app" || status != "active" {
		t.Fatalf("stored installation = (%q, %q, %q), want (org_k21, user_app, active)", orgID, actor, status)
	}
	// The token is sealed, never stored as the bytes the exchange returned.
	var sealed []byte
	dbfx.QueryRow(t, `SELECT access_token_encrypted FROM linear_installation WHERE workspace_id = $1`, testWorkspaceID).Scan(&sealed)
	if strings.Contains(string(sealed), "lin_oauth_token") {
		t.Fatal("access token is readable in the stored column; it must be sealed at rest")
	}

	// The settings endpoint now reports the connection without exposing it.
	var view LinearInstallationResponse
	linearWorkspaceCall(t, testHandler.GetLinearInstallation, http.MethodGet,
		"/api/workspaces/"+testWorkspaceID+"/linear/installation", nil).Want(http.StatusOK).JSON(&view)
	if !view.Connected || view.OrgName != "Acme" || view.AgentID != agentID {
		t.Fatalf("installation view = %+v, want connected to Acme on the chosen agent", view)
	}
}

func TestLinearOAuthCallbackRejectsTamperedState(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableLinear(t, linearTokenServer(t).URL)

	req := httptest.NewRequest(http.MethodGet, "/api/integrations/linear/oauth/callback?code=abc&state=not.a.valid.state", nil)
	resp := testutil.Call(t, testHandler.LinearOAuthCallback, req).Want(http.StatusFound)
	location := resp.Header().Get("Location")
	if !strings.Contains(location, "linear_error=invalid_state") {
		t.Fatalf("redirect Location = %q, want linear_error=invalid_state", location)
	}
	// Nothing may be written before the state verifies.
	if n := dbfx.Count(t, `SELECT count(*) FROM linear_installation WHERE workspace_id = $1`, testWorkspaceID); n != 0 {
		t.Fatalf("%d installations written for a tampered state, want 0", n)
	}
}

func TestLinearDisconnectRemovesInstallationAndLinks(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableLinear(t, linearTokenServer(t).URL)
	linearCleanup(t)
	instID, _, _ := seedLinearInstallation(t)

	issueID := dbfx.Issue(t, "Mirrored issue")
	dbfx.Insert(t, "linear_issue_link", testutil.Cols{
		"workspace_id": testWorkspaceID, "installation_id": instID, "issue_id": issueID,
		"linear_issue_id": "iss_disc", "linear_team_id": "team_1",
	})

	linearWorkspaceCall(t, testHandler.DeleteLinearInstallation, http.MethodDelete,
		"/api/workspaces/"+testWorkspaceID+"/linear/installation", nil).Want(http.StatusOK)

	if n := dbfx.Count(t, `SELECT count(*) FROM linear_installation WHERE workspace_id = $1`, testWorkspaceID); n != 0 {
		t.Fatalf("%d installations left after disconnect, want 0", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM linear_issue_link WHERE workspace_id = $1`, testWorkspaceID); n != 0 {
		t.Fatalf("%d issue links left after disconnect, want 0", n)
	}
}

func TestLinearStatusMapRejectsUnknownKeys(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableLinear(t, linearTokenServer(t).URL)
	linearCleanup(t)
	seedLinearInstallation(t)

	path := "/api/workspaces/" + testWorkspaceID + "/linear/installation/status-map"
	linearWorkspaceCall(t, testHandler.UpdateLinearStatusMap, http.MethodPut, path,
		map[string]any{"status_map": map[string]string{"not_a_linear_type": "todo"}}).Want(http.StatusBadRequest)
	linearWorkspaceCall(t, testHandler.UpdateLinearStatusMap, http.MethodPut, path,
		map[string]any{"status_map": map[string]string{"started": "not_a_status"}}).Want(http.StatusBadRequest)

	var out LinearInstallationResponse
	linearWorkspaceCall(t, testHandler.UpdateLinearStatusMap, http.MethodPut, path,
		map[string]any{"status_map": map[string]string{"started": "in_review"}}).Want(http.StatusOK).JSON(&out)
	if out.StatusMap["started"] != "in_review" {
		t.Fatalf("status map = %v, want started -> in_review", out.StatusMap)
	}
}

func TestLinearLinkLookupAndResync(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableLinear(t, linearTokenServer(t).URL)
	linearCleanup(t)
	instID, _, _ := seedLinearInstallation(t)

	issueID := dbfx.Issue(t, "Mirror for resync")
	linkID := dbfx.Insert(t, "linear_issue_link", testutil.Cols{
		"workspace_id": testWorkspaceID, "installation_id": instID, "issue_id": issueID,
		"linear_issue_id": "iss_resync", "linear_issue_identifier": "ENG-1",
		"linear_team_id": "team_1", "sync_state": "broken", "last_error": "boom",
	})

	var found struct {
		Link *LinearLinkResponse `json:"link"`
	}
	testutil.Call(t, testHandler.GetLinearLink,
		withURLParam(newRequest(http.MethodGet,
			"/api/workspaces/"+testWorkspaceID+"/linear/links?issue_id="+issueID, nil), "id", testWorkspaceID),
	).Want(http.StatusOK).JSON(&found)
	if found.Link == nil || found.Link.Identifier != "ENG-1" {
		t.Fatalf("link lookup = %+v, want the ENG-1 link", found.Link)
	}

	// Resync re-reads Linear and clears the broken state.
	req := withURLParam(newRequest(http.MethodPost,
		"/api/workspaces/"+testWorkspaceID+"/linear/links/"+linkID+"/resync", nil), "id", testWorkspaceID)
	req = testutil.WithURLParams(req, "id", testWorkspaceID, "linkId", linkID)
	var after struct {
		Link LinearLinkResponse `json:"link"`
	}
	testutil.Call(t, testHandler.ResyncLinearLink, req).Want(http.StatusOK).JSON(&after)
	if after.Link.SyncState != "active" {
		t.Fatalf("sync_state = %q after a successful resync, want active", after.Link.SyncState)
	}
	var title string
	dbfx.QueryRow(t, `SELECT title FROM issue WHERE id = $1`, issueID).Scan(&title)
	if title != "Resynced" {
		t.Fatalf("issue title = %q after resync, want the Linear title", title)
	}
}

func TestLinearWebhookRejectsUnsignedAndAcceptsSigned(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	enableLinear(t, linearTokenServer(t).URL)
	linearCleanup(t)
	instID, _, secret := seedLinearInstallation(t)
	agentID := ""
	dbfx.QueryRow(t, `SELECT agent_id::text FROM linear_installation WHERE id = $1`, instID).Scan(&agentID)

	body := fmt.Sprintf(`{"action":"create","type":"Issue","organizationId":"org_k21","webhookTimestamp":%d,`+
		`"data":{"id":"iss_hook","identifier":"ENG-9","title":"From Linear","description":"d",`+
		`"url":"https://linear.app/acme/issue/ENG-9","teamId":"team_1","assigneeId":"user_app",`+
		`"state":{"id":"s","name":"Todo","type":"unstarted"}}}`, time.Now().UnixMilli())

	// Unsigned: 401 and nothing written. The signature IS the credential on
	// this route, so a missing one must never reach the bridge.
	unsigned := httptest.NewRequest(http.MethodPost, "/api/integrations/linear/webhook", strings.NewReader(body))
	testutil.Call(t, testHandler.LinearWebhook, unsigned).Want(http.StatusUnauthorized)
	if n := dbfx.Count(t, `SELECT count(*) FROM linear_issue_link WHERE workspace_id = $1`, testWorkspaceID); n != 0 {
		t.Fatalf("%d links created by an unsigned delivery, want 0", n)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	signed := httptest.NewRequest(http.MethodPost, "/api/integrations/linear/webhook", strings.NewReader(body))
	signed.Header.Set(linear.SignatureHeader, hex.EncodeToString(mac.Sum(nil)))
	testutil.Call(t, testHandler.LinearWebhook, signed).Want(http.StatusOK)

	var mirroredID, mirroredTitle, assigneeType string
	dbfx.QueryRow(t, `SELECT i.id::text, i.title, COALESCE(i.assignee_type, '')
	                  FROM issue i JOIN linear_issue_link l ON l.issue_id = i.id
	                  WHERE l.workspace_id = $1 AND l.linear_issue_id = 'iss_hook'`, testWorkspaceID).
		Scan(&mirroredID, &mirroredTitle, &assigneeType)
	if mirroredTitle != "From Linear" || assigneeType != "agent" {
		t.Fatalf("mirrored issue = (%q, %q), want the Linear title assigned to an agent", mirroredTitle, assigneeType)
	}
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE issue_id = $1`, mirroredID)
		testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, mirroredID)
	})

	// The assignment is what enqueues the agent's run: the bridge never
	// enqueues anything itself, so a missing task means the mirror was created
	// through a path that skips the ordinary auto-enqueue.
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, mirroredID); n != 1 {
		t.Fatalf("%d tasks queued for the mirrored issue, want exactly 1", n)
	}

	// Redelivery: Linear retries on any non-2xx, so the same payload arriving
	// twice must not produce a second issue or a second run.
	signedAgain := httptest.NewRequest(http.MethodPost, "/api/integrations/linear/webhook", strings.NewReader(body))
	signedAgain.Header.Set(linear.SignatureHeader, hex.EncodeToString(mac.Sum(nil)))
	testutil.Call(t, testHandler.LinearWebhook, signedAgain).Want(http.StatusOK)
	if n := dbfx.Count(t, `SELECT count(*) FROM linear_issue_link WHERE workspace_id = $1 AND linear_issue_id = 'iss_hook'`, testWorkspaceID); n != 1 {
		t.Fatalf("%d links after a redelivery, want 1", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_task_queue WHERE issue_id = $1`, mirroredID); n != 1 {
		t.Fatalf("%d tasks after a redelivery, want still 1", n)
	}
}

// seedLinearInstallation writes an active installation for the test workspace
// and returns its id, agent id and plaintext webhook secret.
func seedLinearInstallation(t *testing.T) (instID, agentID, secret string) {
	t.Helper()
	runtimeID := dbfx.Runtime(t, "linear-rt-"+time.Now().Format("150405.000000"))
	agentID = dbfx.Agent(t, "Linear Bridge Agent", runtimeID)
	secret = "webhook-secret"

	sealedToken, err := testHandler.LinearSecretBox.Seal([]byte("token"))
	if err != nil {
		t.Fatalf("seal token: %v", err)
	}
	sealedSecret, err := testHandler.LinearSecretBox.Seal([]byte(secret))
	if err != nil {
		t.Fatalf("seal secret: %v", err)
	}
	statusMap, _ := json.Marshal(linear.DefaultStatusMap())
	instID = dbfx.Insert(t, "linear_installation", testutil.Cols{
		"workspace_id": testWorkspaceID, "agent_id": agentID,
		"linear_org_id": "org_k21", "linear_org_name": "Acme", "actor_user_id": "user_app",
		"access_token_encrypted": sealedToken, "webhook_secret_encrypted": sealedSecret,
		"status_map": statusMap, "installed_by": testUserID,
	})
	return instID, agentID, secret
}
