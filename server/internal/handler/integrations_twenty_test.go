package handler

// Twenty CRM integration: the credential story end to end — an admin connects
// with a key the fake instance accepts, the inbound token and HMAC gate the
// webhook, an accepted item is linked back as a Task on the CRM record, and a
// disconnect revokes everything. Signature matrix and event mapping live in
// internal/integrations/twenty/service_test.go.

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/integrations/twenty"
	"github.com/multica-ai/multica/server/internal/integrations/twenty/twentytest"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/triage"
	"github.com/multica-ai/multica/server/internal/util/secretbox"
)

// installTwenty wires a fake instance and a service into the shared handler
// for one test, and removes the connection afterwards.
func installTwenty(t *testing.T, publicURL string, members ...twentytest.Member) *twentytest.Server {
	t.Helper()
	fake := twentytest.New("key-1", members...)
	t.Cleanup(fake.Close)
	box, err := secretbox.New(bytes.Repeat([]byte("w"), secretbox.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	prev := testHandler.Twenty
	testHandler.Twenty = twenty.NewService(testHandler.Queries, box, publicURL)
	cleanupTriageSourceKind(t, triage.SourceTwenty, testWorkspaceID)
	dbfx.Cleanup(t, `DELETE FROM workspace_twenty_connection WHERE workspace_id = $1`, testWorkspaceID)
	dbfx.Cleanup(t, `DELETE FROM audit_log_entry WHERE workspace_id = $1 AND action LIKE 'twenty.%'`, testWorkspaceID)
	t.Cleanup(func() { testHandler.Twenty = prev })
	return fake
}

func connectTwenty(t *testing.T, fake *twentytest.Server, body map[string]any) twenty.Connection {
	t.Helper()
	if body == nil {
		body = map[string]any{}
	}
	body["base_url"], body["api_key"] = fake.URL, "key-1"
	var conn twenty.Connection
	testutil.Call(t, testHandler.ConnectTwenty, newRequest(http.MethodPost, "/api/integrations/twenty/connect", body)).Want(http.StatusCreated).JSON(&conn)
	return conn
}

func postTwentyWebhook(t *testing.T, token string, payload []byte, headers http.Header) *testutil.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, twenty.InboundPathPrefix+token, bytes.NewReader(payload))
	for k, v := range headers {
		req.Header[k] = v
	}
	return testutil.Call(t, testHandler.HandleInboundTwentyWebhook, testutil.WithURLParams(req, "token", token))
}

func TestTwentyAnswers503UntilConfigured(t *testing.T) {
	prev := testHandler.Twenty
	testHandler.Twenty = nil
	t.Cleanup(func() { testHandler.Twenty = prev })

	var status TwentyStatusResponse
	testutil.Call(t, testHandler.GetTwentyConnection, newRequest(http.MethodGet, "/api/integrations/twenty", nil)).Want(http.StatusOK).JSON(&status)
	if status.Available || status.Connected || len(status.Events) == 0 {
		t.Fatalf("status = %+v, want unavailable with the default events listed", status)
	}
	testutil.Call(t, testHandler.ConnectTwenty, newRequest(http.MethodPost, "/api/integrations/twenty/connect", map[string]any{"base_url": "https://x", "api_key": "k"})).Want(http.StatusServiceUnavailable)
	testutil.Call(t, testHandler.HandleInboundTwentyWebhook, testutil.WithURLParams(newRequest(http.MethodPost, twenty.InboundPathPrefix+"mtw_x", map[string]any{}), "token", "mtw_x")).Want(http.StatusNotFound)
}

func TestTwentyConnectShowsTheTokenOnceAndTheStatusAfter(t *testing.T) {
	fake := installTwenty(t, "https://vigil.example", twentytest.Member{ID: "m1", Email: "someone@example.com", FirstName: "Some", LastName: "One"})

	conn := connectTwenty(t, fake, map[string]any{"events": []string{"person.created"}})
	if !strings.HasPrefix(conn.InboundToken, "mtw_") || !conn.WebhookRegistered || conn.Status != "connected" {
		t.Fatalf("connection = %+v", conn)
	}
	var status TwentyStatusResponse
	testutil.Call(t, testHandler.GetTwentyConnection, newRequest(http.MethodGet, "/api/integrations/twenty", nil)).Want(http.StatusOK).JSON(&status)
	if !status.Available || !status.Connected || status.Connection == nil {
		t.Fatalf("status = %+v", status)
	}
	if status.Connection.InboundToken != "" || status.Connection.BaseURL != fake.URL || strings.Join(status.Connection.Events, ",") != "person.created" {
		t.Fatalf("connection on GET = %+v, want no clear token", *status.Connection)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2`, testWorkspaceID, AuditTwentyConnected); n != 1 {
		t.Fatalf("audit entries = %d, want 1", n)
	}

	// Bad input and a refused key are the caller's problem, not a 500.
	testutil.Call(t, testHandler.ConnectTwenty, newRequest(http.MethodPost, "/api/integrations/twenty/connect", map[string]any{"base_url": fake.URL, "api_key": "nope"})).Want(http.StatusBadGateway)
	testutil.Call(t, testHandler.ConnectTwenty, newRequest(http.MethodPost, "/api/integrations/twenty/connect", map[string]any{"base_url": fake.URL, "api_key": "key-1", "events": []string{"bad"}})).Want(http.StatusBadRequest)
	testutil.Call(t, testHandler.ConnectTwenty, newRequest(http.MethodPost, "/api/integrations/twenty/connect", map[string]any{"api_key": "key-1"})).Want(http.StatusBadRequest)
}

func TestTwentyManagementNeedsAnAdmin(t *testing.T) {
	fake := installTwenty(t, "https://vigil.example")
	dbfx.Exec(t, `UPDATE member SET role = 'member' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)
	dbfx.Cleanup(t, `UPDATE member SET role = 'owner' WHERE workspace_id = $1 AND user_id = $2`, testWorkspaceID, testUserID)

	testutil.Call(t, testHandler.ConnectTwenty, newRequest(http.MethodPost, "/api/integrations/twenty/connect", map[string]any{"base_url": fake.URL, "api_key": "key-1"})).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.PutTwentySettings, newRequest(http.MethodPut, "/api/integrations/twenty/settings", map[string]any{})).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.DisconnectTwenty, newRequest(http.MethodDelete, "/api/integrations/twenty", nil)).Want(http.StatusForbidden)
	// Reading stays a member's right.
	testutil.Call(t, testHandler.GetTwentyConnection, newRequest(http.MethodGet, "/api/integrations/twenty", nil)).Want(http.StatusOK)
}

func TestTwentyWebhookAdmitsOnlySignedDeliveriesFromTheSubscription(t *testing.T) {
	fake := installTwenty(t, "https://vigil.example")
	conn := connectTwenty(t, fake, map[string]any{"events": []string{"person.created", "opportunity.*"}})
	secret := fake.Webhooks()[0].Secret
	payload := twentytest.Event("person.created", "person", map[string]any{"id": "p-1", "name": map[string]any{"firstName": "Grace", "lastName": "Hopper"}, "email": "grace@example.com"})

	postTwentyWebhook(t, "mtw_unknown", payload, twentytest.Sign(secret, payload, time.Now())).Want(http.StatusNotFound)
	postTwentyWebhook(t, conn.InboundToken, payload, twentytest.Sign("wrong", payload, time.Now())).Want(http.StatusUnauthorized)
	postTwentyWebhook(t, conn.InboundToken, payload, twentytest.Sign(secret, payload, time.Now().Add(-time.Hour))).Want(http.StatusUnauthorized)
	postTwentyWebhook(t, conn.InboundToken, payload, http.Header{"Content-Type": {"application/json"}}).Want(http.StatusUnauthorized)
	if n := dbfx.Count(t, `SELECT count(*) FROM triage_item WHERE workspace_id = $1 AND origin_type = 'twenty'`, testWorkspaceID); n != 0 {
		t.Fatalf("items after refused deliveries = %d", n)
	}

	var out map[string]string
	postTwentyWebhook(t, conn.InboundToken, payload, twentytest.Sign(secret, payload, time.Now())).Want(http.StatusAccepted).JSON(&out)
	if out["status"] != "queued" {
		t.Fatalf("status = %q", out["status"])
	}
	dbfx.Cleanup(t, `DELETE FROM triage_item WHERE workspace_id = $1 AND origin_type = 'twenty'`, testWorkspaceID)
	var title, state string
	dbfx.QueryRow(t, `SELECT title, state FROM triage_item WHERE workspace_id = $1 AND origin_type = 'twenty'`, testWorkspaceID).Scan(&title, &state)
	if title != "Twenty · Person created: Grace Hopper" || state != triage.StatePending {
		t.Fatalf("item = %q / %q", title, state)
	}

	// Subscribed in Twenty but not in the workspace's settings: acknowledged, not queued.
	other := twentytest.Event("company.created", "company", map[string]any{"id": "c-1", "name": "Acme"})
	postTwentyWebhook(t, conn.InboundToken, other, twentytest.Sign(secret, other, time.Now())).Want(http.StatusAccepted).JSON(&out)
	if out["status"] != "ignored" {
		t.Fatalf("status = %q, want ignored", out["status"])
	}
	postTwentyWebhook(t, conn.InboundToken, []byte(`{"eventDate":"x"}`), twentytest.Sign(secret, []byte(`{"eventDate":"x"}`), time.Now())).Want(http.StatusBadRequest)
}

func TestTwentyAcceptLinksTheIssueBackOnTheRecord(t *testing.T) {
	fake := installTwenty(t, "https://vigil.example")
	conn := connectTwenty(t, fake, nil)
	secret := fake.Webhooks()[0].Secret
	payload := twentytest.Event("opportunity.created", "opportunity", map[string]any{"id": "opp-9", "name": "Acme renewal", "stage": "NEW"})
	postTwentyWebhook(t, conn.InboundToken, payload, twentytest.Sign(secret, payload, time.Now())).Want(http.StatusAccepted)
	dbfx.Cleanup(t, `DELETE FROM triage_item WHERE workspace_id = $1 AND origin_type = 'twenty'`, testWorkspaceID)
	var itemID string
	dbfx.QueryRow(t, `SELECT id FROM triage_item WHERE workspace_id = $1 AND origin_type = 'twenty'`, testWorkspaceID).Scan(&itemID)

	var out struct {
		Issue struct {
			ID         string `json:"id"`
			Identifier string `json:"identifier"`
			Title      string `json:"title"`
		} `json:"issue"`
	}
	testutil.Call(t, testHandler.AcceptTriageItem, testutil.WithURLParams(
		newRequest(http.MethodPost, "/api/triage/items/"+itemID+"/accept", map[string]any{}), "id", itemID,
	)).Want(http.StatusOK).JSON(&out)
	dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, out.Issue.ID)

	deadline := time.Now().Add(5 * time.Second)
	for len(fake.Tasks()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	tasks := fake.Tasks()
	if len(tasks) != 1 {
		t.Fatalf("tasks filed on the CRM = %d, want 1", len(tasks))
	}
	if !strings.HasPrefix(tasks[0].Title, "Vigil "+out.Issue.Identifier+": ") || tasks[0].TargetField != "targetOpportunityId" || tasks[0].TargetID != "opp-9" {
		t.Fatalf("task = %+v (issue %s)", tasks[0], out.Issue.Identifier)
	}
	if !strings.Contains(tasks[0].Body, "/issues/"+out.Issue.ID) {
		t.Fatalf("task body = %q, want the issue URL", tasks[0].Body)
	}
	deadline = time.Now().Add(5 * time.Second)
	for dbfx.Count(t, `SELECT count(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2`, testWorkspaceID, AuditTwentyBacklinked) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2`, testWorkspaceID, AuditTwentyBacklinked); n != 1 {
		t.Fatalf("backlink audit entries = %d", n)
	}
}

func TestTwentySettingsMembersAndDisconnect(t *testing.T) {
	fake := installTwenty(t, "https://vigil.example", twentytest.Member{ID: "t-1", Email: strings.ToUpper(testUserEmailForTwenty(t)), FirstName: "Test", LastName: "Owner"})
	conn := connectTwenty(t, fake, nil)

	var updated twenty.Connection
	testutil.Call(t, testHandler.PutTwentySettings, newRequest(http.MethodPut, "/api/integrations/twenty/settings", map[string]any{"events": []string{"company.*"}, "expose_to_agents": false})).Want(http.StatusOK).JSON(&updated)
	if strings.Join(updated.Events, ",") != "company.*" || updated.ExposeToAgents || !updated.WebhookRegistered {
		t.Fatalf("updated = %+v", updated)
	}
	if hooks := fake.Webhooks(); len(hooks) != 1 || strings.Join(hooks[0].Operations, ",") != "company.*" {
		t.Fatalf("webhooks = %+v", hooks)
	}
	testutil.Call(t, testHandler.PutTwentySettings, newRequest(http.MethodPut, "/api/integrations/twenty/settings", map[string]any{"events": []string{"nope"}})).Want(http.StatusBadRequest)

	var members struct {
		Members []twenty.MemberLink `json:"members"`
	}
	testutil.Call(t, testHandler.ListTwentyMembers, newRequest(http.MethodGet, "/api/integrations/twenty/members", nil)).Want(http.StatusOK).JSON(&members)
	linked := 0
	for _, m := range members.Members {
		if m.Linked && m.TwentyID == "t-1" {
			linked++
		}
	}
	if linked != 1 {
		t.Fatalf("members = %+v, want the test owner paired with t-1", members.Members)
	}

	var checked twenty.Connection
	testutil.Call(t, testHandler.CheckTwentyConnection, newRequest(http.MethodPost, "/api/integrations/twenty/check", nil)).Want(http.StatusOK).JSON(&checked)
	if checked.Status != "connected" {
		t.Fatalf("checked = %+v", checked)
	}

	testutil.Call(t, testHandler.DisconnectTwenty, newRequest(http.MethodDelete, "/api/integrations/twenty", nil)).Want(http.StatusNoContent)
	if len(fake.Webhooks()) != 0 {
		t.Fatal("webhook survived the disconnect")
	}
	secret := "gone"
	payload := twentytest.Event("company.created", "company", map[string]any{"id": "c-1"})
	postTwentyWebhook(t, conn.InboundToken, payload, twentytest.Sign(secret, payload, time.Now())).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.ListTwentyMembers, newRequest(http.MethodGet, "/api/integrations/twenty/members", nil)).Want(http.StatusNotFound)
	testutil.Call(t, testHandler.DisconnectTwenty, newRequest(http.MethodDelete, "/api/integrations/twenty", nil)).Want(http.StatusNotFound)
}

func testUserEmailForTwenty(t *testing.T) string {
	t.Helper()
	var email string
	if err := testPool.QueryRow(context.Background(), `SELECT email FROM "user" WHERE id = $1`, testUserID).Scan(&email); err != nil {
		t.Fatal(err)
	}
	return email
}
