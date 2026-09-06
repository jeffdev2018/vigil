package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// asWorkspaceMember rewrites req to be made BY userID: the X-User-ID header
// the handlers read, plus the member context the chi middleware chain would
// have set. Acting as a second member is the whole point of these tests, so
// withChatTestWorkspaceCtx (hardcoded to the suite user) is not enough.
func asWorkspaceMember(t *testing.T, req *http.Request, userID string) *http.Request {
	t.Helper()
	memberRow, err := testHandler.Queries.GetMemberByUserAndWorkspace(context.Background(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      util.MustParseUUID(userID),
		WorkspaceID: util.MustParseUUID(testWorkspaceID),
	})
	if err != nil {
		t.Fatalf("load member row for %s: %v", userID, err)
	}
	req.Header.Set("X-User-ID", userID)
	return req.WithContext(middleware.SetMemberContext(req.Context(), testWorkspaceID, memberRow))
}

// Multiplayer chat sessions (K31 / JEF-181). The canonical layer for the
// access rule itself is chatSessionAccess; these tests pin what the HTTP
// surface does with it — who may read, send, reshape, and leave.

// participantFixture builds a chat session owned by the suite user plus a
// second workspace member to share it with. chat_session_participant carries
// no FK, so its rows are swept before the session fixture is torn down.
func participantFixture(t *testing.T) (sessionID, peerUserID string) {
	t.Helper()
	agentID := createHandlerTestAgent(t, "participant-agent", nil)
	// explicitly_created_at marks a first-party Chat: without it the public
	// gate (GetPublicChatSessionInWorkspace) treats an empty session as not
	// yet a Chat and every gated handler answers 404 instead of the access
	// verdict these tests are about.
	sessionID = dbfx.ChatSession(t, agentID, testutil.Cols{
		"title":                 "shared chat",
		"explicitly_created_at": testutil.Raw("now()"),
	})
	t.Cleanup(func() {
		testPool.Exec(context.Background(),
			`DELETE FROM chat_session_participant WHERE chat_session_id = $1`, sessionID)
	})
	peerUserID = dbfx.User(t, "Peer Member", "peer-"+sessionID+"@example.test")
	dbfx.Member(t, testWorkspaceID, peerUserID, "member")
	return sessionID, peerUserID
}

func participantRequest(t *testing.T, method, sessionID, userID string, body any, actingUserID string) *http.Request {
	t.Helper()
	req := newRequest(method, "/api/chat/sessions/"+sessionID+"/participants", body)
	if actingUserID == "" {
		actingUserID = testUserID
	}
	req = asWorkspaceMember(t, req, actingUserID)
	if userID != "" {
		return testutil.WithURLParams(req, "sessionId", sessionID, "userId", userID)
	}
	return testutil.WithURLParams(req, "sessionId", sessionID)
}

func addParticipant(t *testing.T, sessionID, peerUserID string) {
	t.Helper()
	testutil.Call(t, testHandler.AddChatParticipant,
		participantRequest(t, "POST", sessionID, "", map[string]any{"user_id": peerUserID}, "")).
		Want(http.StatusOK)
}

func TestChatSessionParticipantAddListRemove(t *testing.T) {
	sessionID, peerUserID := participantFixture(t)

	// Solo: the creator is synthesized as an implicit owner, never stored.
	var solo ChatParticipantListResponse
	testutil.Call(t, testHandler.ListChatParticipants,
		participantRequest(t, "GET", sessionID, "", nil, "")).
		Want(http.StatusOK).JSON(&solo)
	if len(solo.Participants) != 1 || solo.Participants[0].UserID != testUserID {
		t.Fatalf("solo roster = %#v, want just the creator %s", solo.Participants, testUserID)
	}
	if solo.Participants[0].Role != "owner" {
		t.Fatalf("creator role = %q, want owner", solo.Participants[0].Role)
	}
	var stored int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM chat_session_participant WHERE chat_session_id = $1`, sessionID).Scan(&stored)
	if stored != 0 {
		t.Fatalf("creator was stored as a participant row (count=%d); it must stay implicit", stored)
	}

	addParticipant(t, sessionID, peerUserID)
	// Idempotent: re-adding is a plain success, not a 409 on the unique index.
	addParticipant(t, sessionID, peerUserID)
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM chat_session_participant WHERE chat_session_id = $1`, sessionID).Scan(&stored)
	if stored != 1 {
		t.Fatalf("re-adding the same member wrote %d rows, want 1", stored)
	}

	var shared ChatParticipantListResponse
	testutil.Call(t, testHandler.ListChatParticipants,
		participantRequest(t, "GET", sessionID, "", nil, "")).
		Want(http.StatusOK).JSON(&shared)
	if len(shared.Participants) != 2 {
		t.Fatalf("shared roster = %#v, want creator + peer", shared.Participants)
	}
	if shared.Participants[1].UserID != peerUserID || shared.Participants[1].Role != "participant" {
		t.Fatalf("second roster entry = %#v, want %s as participant", shared.Participants[1], peerUserID)
	}
	if shared.Participants[1].Name != "Peer Member" {
		t.Fatalf("roster did not join the user row: name = %q", shared.Participants[1].Name)
	}

	testutil.Call(t, testHandler.RemoveChatParticipant,
		participantRequest(t, "DELETE", sessionID, peerUserID, nil, "")).
		Want(http.StatusNoContent)
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM chat_session_participant WHERE chat_session_id = $1`, sessionID).Scan(&stored)
	if stored != 0 {
		t.Fatalf("remove left %d participant rows, want 0", stored)
	}
}

func TestChatSessionParticipantAddIsCreatorOnly(t *testing.T) {
	sessionID, peerUserID := participantFixture(t)
	addParticipant(t, sessionID, peerUserID)

	other := dbfx.User(t, "Third Member", "third-"+sessionID+"@example.test")
	dbfx.Member(t, testWorkspaceID, other, "member")

	// A participant can read the session but must not grow its roster.
	testutil.Call(t, testHandler.AddChatParticipant,
		participantRequest(t, "POST", sessionID, "", map[string]any{"user_id": other}, peerUserID)).
		Want(http.StatusForbidden)
}

func TestChatSessionParticipantAddRejectsNonMember(t *testing.T) {
	sessionID, _ := participantFixture(t)
	outsider := dbfx.User(t, "Outsider", "outsider-"+sessionID+"@example.test")

	testutil.Call(t, testHandler.AddChatParticipant,
		participantRequest(t, "POST", sessionID, "", map[string]any{"user_id": outsider}, "")).
		Want(http.StatusNotFound)

	testutil.Call(t, testHandler.AddChatParticipant,
		participantRequest(t, "POST", sessionID, "", map[string]any{"user_id": "not-a-uuid"}, "")).
		Want(http.StatusBadRequest)
}

func TestChatSessionParticipantCanReadButNotReshape(t *testing.T) {
	sessionID, peerUserID := participantFixture(t)

	// Before being added, the peer is a stranger to this session.
	testutil.Call(t, testHandler.ListChatParticipants,
		participantRequest(t, "GET", sessionID, "", nil, peerUserID)).
		Want(http.StatusForbidden)

	addParticipant(t, sessionID, peerUserID)

	testutil.Call(t, testHandler.ListChatParticipants,
		participantRequest(t, "GET", sessionID, "", nil, peerUserID)).
		Want(http.StatusOK)
	// Ephemeral typing ping: any participant may send one.
	testutil.Call(t, testHandler.SendChatTyping,
		participantRequest(t, "POST", sessionID, "", nil, peerUserID)).
		Want(http.StatusNoContent)

	// Reshaping the session stays with the creator.
	deleteReq := testutil.WithURLParams(
		asWorkspaceMember(t, newRequest("DELETE", "/api/chat/sessions/"+sessionID, nil), peerUserID),
		"sessionId", sessionID)
	testutil.Call(t, testHandler.DeleteChatSession, deleteReq).Want(http.StatusForbidden)

	archiveReq := testutil.WithURLParams(
		asWorkspaceMember(t, newRequest("PATCH", "/api/chat/sessions/"+sessionID+"/archive",
			map[string]any{"archived": true}), peerUserID),
		"sessionId", sessionID)
	testutil.Call(t, testHandler.SetChatSessionArchived, archiveReq).Want(http.StatusForbidden)

	renameReq := testutil.WithURLParams(
		asWorkspaceMember(t, newRequest("PATCH", "/api/chat/sessions/"+sessionID,
			map[string]any{"title": "hijacked"}), peerUserID),
		"sessionId", sessionID)
	testutil.Call(t, testHandler.UpdateChatSession, renameReq).Want(http.StatusForbidden)
}

func TestChatSessionParticipantSelfLeave(t *testing.T) {
	sessionID, peerUserID := participantFixture(t)
	addParticipant(t, sessionID, peerUserID)

	stranger := dbfx.User(t, "Stranger", "stranger-"+sessionID+"@example.test")
	dbfx.Member(t, testWorkspaceID, stranger, "member")

	// A non-creator may not remove somebody else.
	testutil.Call(t, testHandler.RemoveChatParticipant,
		participantRequest(t, "DELETE", sessionID, peerUserID, nil, stranger)).
		Want(http.StatusForbidden)

	// …but may remove themselves.
	testutil.Call(t, testHandler.RemoveChatParticipant,
		participantRequest(t, "DELETE", sessionID, peerUserID, nil, peerUserID)).
		Want(http.StatusNoContent)

	var stored int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM chat_session_participant WHERE chat_session_id = $1`, sessionID).Scan(&stored)
	if stored != 0 {
		t.Fatalf("self-leave left %d participant rows, want 0", stored)
	}
}

func TestChatSessionParticipantCreatorCannotBeRemoved(t *testing.T) {
	sessionID, _ := participantFixture(t)
	// The creator has no row to delete; removing them would leave a session
	// whose owner cannot reach it. Delete or archive is the way out.
	testutil.Call(t, testHandler.RemoveChatParticipant,
		participantRequest(t, "DELETE", sessionID, testUserID, nil, "")).
		Want(http.StatusBadRequest)
}

// A shared session appears in the participant's own session list, with the
// same unread/preview shape as one they created.
func TestChatSessionParticipantSeesSessionInList(t *testing.T) {
	sessionID, peerUserID := participantFixture(t)

	listReq := asWorkspaceMember(t, newRequest("GET", "/api/chat/sessions?status=all", nil), peerUserID)
	var before []ChatSessionResponse
	testutil.Call(t, testHandler.ListChatSessions, listReq).Want(http.StatusOK).JSON(&before)
	for _, s := range before {
		if s.ID == sessionID {
			t.Fatalf("peer saw session %s before being added", sessionID)
		}
	}

	addParticipant(t, sessionID, peerUserID)

	listReq = asWorkspaceMember(t, newRequest("GET", "/api/chat/sessions?status=all", nil), peerUserID)
	var after []ChatSessionResponse
	testutil.Call(t, testHandler.ListChatSessions, listReq).Want(http.StatusOK).JSON(&after)
	found := false
	for _, s := range after {
		if s.ID == sessionID {
			found = true
			if s.CreatorID != testUserID {
				t.Fatalf("listed session creator = %q, want the original creator %q", s.CreatorID, testUserID)
			}
		}
	}
	if !found {
		t.Fatalf("peer's session list %#v does not contain the shared session %s", after, sessionID)
	}
}

// Per-message attribution: a user message carries the human who sent it, and
// assistant rows stay NULL.
func TestChatSessionParticipantMessageAuthor(t *testing.T) {
	sessionID, peerUserID := participantFixture(t)
	addParticipant(t, sessionID, peerUserID)

	// Distinct created_at: ListChatMessages orders by (created_at, id), and two
	// rows sharing one now() would let a random uuid decide which reads first.
	dbfx.Exec(t, `
		INSERT INTO chat_message (chat_session_id, role, content, author_user_id, created_at)
		VALUES ($1, 'user', 'from the peer', $2, now() - interval '1 second'),
		       ($1, 'assistant', 'reply', NULL, now())
	`, sessionID, peerUserID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM chat_message WHERE chat_session_id = $1`, sessionID)
	})

	req := testutil.WithURLParams(
		asWorkspaceMember(t, newRequest("GET", "/api/chat/sessions/"+sessionID+"/messages", nil), peerUserID),
		"sessionId", sessionID)
	var messages []ChatMessageResponse
	testutil.Call(t, testHandler.ListChatMessages, req).Want(http.StatusOK).JSON(&messages)
	if len(messages) != 2 {
		t.Fatalf("messages = %#v, want the user row and the assistant row", messages)
	}
	if messages[0].AuthorUserID == nil || *messages[0].AuthorUserID != peerUserID {
		t.Fatalf("user message author = %v, want %s", messages[0].AuthorUserID, peerUserID)
	}
	if messages[1].AuthorUserID != nil {
		t.Fatalf("assistant message author = %v, want null", *messages[1].AuthorUserID)
	}
}
