package handler

import (
	"net/http"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// The SCIM flow end to end (token lifecycle, list/get/patch/delete, audit)
// lives in access_k60_test.go. This pins the two things a provider and an open
// client rely on from a single provisioning call: the SCIM shape of the 201
// body, and the member:added fanout carrying `member.user_id` exactly like the
// invitation path does, so a connected client refreshes its member list
// instead of ignoring an event it cannot attribute.
func TestScimCreateUserPublishesMemberAddedWithUserID(t *testing.T) {
	ws := func(req *http.Request) *http.Request { return testutil.WithURLParams(req, "id", testWorkspaceID) }
	dbfx.Cleanup(t, `DELETE FROM scim_token WHERE workspace_id = $1`, testWorkspaceID)
	var tok ScimTokenResponse
	testutil.Call(t, testHandler.CreateScimToken, ws(newRequest(http.MethodPost, "/x", nil))).Want(http.StatusCreated).JSON(&tok)

	email := "scim-event-" + uuid.NewString()[:8] + "@example.test"
	dbfx.Cleanup(t, `DELETE FROM member WHERE workspace_id = $1 AND user_id IN (SELECT id FROM "user" WHERE email = $2)`, testWorkspaceID, email)
	dbfx.Cleanup(t, `DELETE FROM "user" WHERE email = $1`, email)

	// The bus is synchronous, so the event is recorded before the handler
	// returns. Filtering on this email keeps the assertion deterministic even
	// if another test publishes on the same topic.
	var mu sync.Mutex
	var added []events.Event
	testHandler.Bus.Subscribe(protocol.EventMemberAdded, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		member, ok := payload["member"].(MemberWithUserResponse)
		if !ok || member.Email != email {
			return
		}
		mu.Lock()
		added = append(added, e)
		mu.Unlock()
	})

	scim := middleware.SCIMBearerOnly(testHandler.Queries)
	body := map[string]any{
		"schemas":    []string{scimUserSchema},
		"userName":   email,
		"externalId": "okta-42",
		"name":       map[string]string{"givenName": "Grace", "familyName": "Hopper"},
		"emails":     []map[string]any{{"value": email, "primary": true}},
		"active":     true,
	}
	res := testutil.Call(t, scim(http.HandlerFunc(testHandler.ScimCreateUser)).ServeHTTP, scimRequest(http.MethodPost, "/scim/v2/Users", tok.Token, body)).Want(http.StatusCreated)
	if ct := res.Header().Get("Content-Type"); ct != "application/scim+json" {
		t.Fatalf("Content-Type = %q, want application/scim+json", ct)
	}
	var created scimUser
	res.JSON(&created)
	if len(created.Schemas) != 1 || created.Schemas[0] != scimUserSchema || created.ID == "" || created.UserName != email || created.ExternalID != "okta-42" || !created.Active {
		t.Fatalf("SCIM user: %s", res.Body.String())
	}
	if created.DisplayName != "Grace Hopper" || created.Name.Formatted != "Grace Hopper" {
		t.Fatalf("display name from givenName+familyName: %s", res.Body.String())
	}
	if len(created.Emails) != 1 || created.Emails[0].Value != email || !created.Emails[0].Primary {
		t.Fatalf("emails: %s", res.Body.String())
	}
	if created.Meta.ResourceType != "User" || created.Meta.Location != "/scim/v2/Users/"+created.ID || created.Meta.Created == "" {
		t.Fatalf("meta: %s", res.Body.String())
	}

	// The response id is the membership; the member row is a plain member of
	// this workspace for a user carrying the provisioned email.
	var userID string
	dbfx.QueryRow(t, `SELECT m.user_id::text FROM member m JOIN "user" u ON u.id = m.user_id WHERE m.id = $1 AND m.workspace_id = $2 AND u.email = $3 AND m.role = 'member' AND m.scim_external_id = 'okta-42'`, created.ID, testWorkspaceID, email).Scan(&userID)

	mu.Lock()
	defer mu.Unlock()
	if len(added) != 1 {
		t.Fatalf("member:added events for %s = %d, want 1", email, len(added))
	}
	e := added[0]
	if e.WorkspaceID != testWorkspaceID || e.ActorType != "system" {
		t.Fatalf("event envelope: workspace %q actor %q", e.WorkspaceID, e.ActorType)
	}
	payload := e.Payload.(map[string]any)
	member := payload["member"].(MemberWithUserResponse)
	if member.UserID != userID || member.ID != created.ID || member.WorkspaceID != testWorkspaceID || member.Role != "member" {
		t.Fatalf("event member = %+v, want user_id %s / member %s", member, userID, created.ID)
	}
	if name, _ := payload["workspace_name"].(string); name == "" {
		t.Fatalf("event carries no workspace_name: %v", payload)
	}
}
