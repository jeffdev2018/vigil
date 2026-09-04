package handler

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/stt"
)

// stubSTT serves an OpenAI-compatible transcription endpoint that answers
// `text` for every upload, and points testHandler.STT at it for the test.
func stubSTT(t *testing.T, text string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/transcriptions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"text":`+jsonString(text)+`}`)
	}))
	t.Cleanup(srv.Close)
	prev := testHandler.STT
	testHandler.STT = stt.New(stt.Config{BaseURL: srv.URL, Model: "stub"})
	t.Cleanup(func() { testHandler.STT = prev })
	return srv
}

func TestMeetingLiveTranscriptSegmentsAndRealtimeSession(t *testing.T) {
	// A provider that also mints realtime client tokens.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/client/sessions" {
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"client_secret":{"value":"rt_test","expires_at":"2026-09-04T10:01:00Z"}}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	prev := testHandler.STT
	testHandler.STT = stt.New(stt.Config{BaseURL: srv.URL, APIKey: "k", Model: "stub", RealtimeModel: "rt-model"})
	t.Cleanup(func() { testHandler.STT = prev })

	var session map[string]any
	testutil.Call(t, testHandler.RealtimeVoiceSession, newRequest(http.MethodPost, "/api/voice/realtime-session", nil)).
		Want(http.StatusOK).JSON(&session)
	if session["token"] != "rt_test" || session["sample_rate"] != float64(16000) || !strings.Contains(session["url"].(string), "/v1/audio/transcriptions/realtime?model=rt-model") {
		t.Fatalf("session = %v", session)
	}

	var created MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", map[string]string{"title": "Live"})).
		Want(http.StatusCreated).JSON(&created)
	cleanupMeeting(t, created.ID)
	// The live client sends text it already has; no audio hits the provider.
	seg := testutil.Call(t, testHandler.AppendMeetingSegment,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+created.ID+"/segments", map[string]string{"seq": "1", "text": "  On livre vendredi.  "}), "id", created.ID)).
		Want(http.StatusOK).Map()
	if seg["text"] != "On livre vendredi." || seg["segment_count"] != float64(1) {
		t.Fatalf("segment = %v", seg)
	}
	var fetched MeetingResponse
	testutil.Call(t, testHandler.GetMeeting,
		testutil.WithURLParams(newRequest(http.MethodGet, "/api/meetings/"+created.ID, nil), "id", created.ID)).
		Want(http.StatusOK).JSON(&fetched)
	if fetched.Transcript != "On livre vendredi." {
		t.Fatalf("transcript = %q", fetched.Transcript)
	}
}

func TestRealtimeSessionRequiresRealtimeModel(t *testing.T) {
	stubSTT(t, "x") // batch only
	body := testutil.Call(t, testHandler.RealtimeVoiceSession, newRequest(http.MethodPost, "/api/voice/realtime-session", nil)).
		Want(http.StatusConflict).Map()
	if body["code"] != "realtime_not_configured" {
		t.Fatalf("code = %v", body["code"])
	}
}

func audioUploadRequest(t *testing.T, path string, seq string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("file", "seg.webm")
	if err != nil {
		t.Fatalf("form file: %v", err)
	}
	_, _ = part.Write([]byte("OPUSBYTES"))
	if seq != "" {
		_ = mw.WriteField("seq", seq)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	return req
}

func cleanupMeeting(t *testing.T, id string) {
	t.Helper()
	dbfx.Cleanup(t, `DELETE FROM triage_item WHERE origin_type = 'meeting' AND origin_id = $1`, id)
	dbfx.Cleanup(t, `DELETE FROM triage_source WHERE kind = 'meeting' AND ref_id = $1`, id)
	dbfx.Cleanup(t, `DELETE FROM meeting WHERE id = $1`, id)
}

func TestMeetingRecordFinishQueuesActions(t *testing.T) {
	stubSTT(t, "Paul livre le connecteur Stripe vendredi.")
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK,
		`{"summary_markdown":"- Livraison Stripe vendredi","actions":[`+
			`{"title":"Livrer le connecteur Stripe","owner":"Paul","evidence":"Paul livre le connecteur Stripe vendredi."},`+
			`{"title":"Action sans preuve","owner":"","evidence":""}]}`))

	var created MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", map[string]string{"title": "Point Stripe", "app_name": "Zoom"})).
		Want(http.StatusCreated).JSON(&created)
	cleanupMeeting(t, created.ID)
	if created.Status != "recording" || created.Title != "Point Stripe" {
		t.Fatalf("created = %+v", created)
	}

	seg := testutil.Call(t, testHandler.AppendMeetingSegment,
		testutil.WithURLParams(audioUploadRequest(t, "/api/meetings/"+created.ID+"/segments", "1"), "id", created.ID)).
		Want(http.StatusOK).Map()
	if seg["text"] != "Paul livre le connecteur Stripe vendredi." || seg["segment_count"] != float64(1) {
		t.Fatalf("segment = %v", seg)
	}

	var done MeetingResponse
	testutil.Call(t, testHandler.FinishMeeting,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+created.ID+"/finish", nil), "id", created.ID)).
		Want(http.StatusOK).JSON(&done)
	if done.Status != "done" || done.EndedAt == nil {
		t.Fatalf("finish = %+v", done)
	}
	if !strings.Contains(done.SummaryMarkdown, "Stripe") {
		t.Fatalf("summary = %q", done.SummaryMarkdown)
	}
	if len(done.Actions) != 1 || done.Actions[0].Title != "Livrer le connecteur Stripe" || done.Actions[0].State != "pending" {
		t.Fatalf("actions = %+v (the evidence-less action must be skipped)", done.Actions)
	}
	if got := dbfx.Count(t, `SELECT COUNT(*) FROM triage_source WHERE kind = 'meeting' AND ref_id = $1 AND name = 'Point Stripe'`, created.ID); got != 1 {
		t.Fatalf("triage sources for meeting = %d, want 1", got)
	}

	// Accepting an action creates an issue that carries the meeting origin.
	// This is the path that used to trip the issue.origin_type CHECK.
	var accepted map[string]any
	testutil.Call(t, testHandler.AcceptTriageItem,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/triage/items/"+done.Actions[0].TriageItemID+"/accept", map[string]any{}), "id", done.Actions[0].TriageItemID)).
		WantOneOf(http.StatusOK, http.StatusCreated).JSON(&accepted)
	var issueID, originType string
	dbfx.QueryRow(t, `SELECT i.id::text, i.origin_type FROM triage_item ti JOIN issue i ON i.id = ti.issue_id WHERE ti.id = $1`, done.Actions[0].TriageItemID).Scan(&issueID, &originType)
	dbfx.Cleanup(t, `DELETE FROM issue WHERE id = $1`, issueID)
	if originType != "meeting" {
		t.Fatalf("accepted issue origin_type = %q, want meeting", originType)
	}

	// Idempotent: finishing again returns the same state, no new items.
	var again MeetingResponse
	testutil.Call(t, testHandler.FinishMeeting,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+created.ID+"/finish", nil), "id", created.ID)).
		Want(http.StatusOK).JSON(&again)
	if again.Status != "done" || len(again.Actions) != 1 || again.Actions[0].State != "accepted" {
		t.Fatalf("second finish = %+v", again)
	}
	// A late segment is refused once the meeting is done.
	testutil.Call(t, testHandler.AppendMeetingSegment,
		testutil.WithURLParams(audioUploadRequest(t, "/api/meetings/"+created.ID+"/segments", "2"), "id", created.ID)).
		Want(http.StatusConflict)

	var fetched MeetingResponse
	testutil.Call(t, testHandler.GetMeeting,
		testutil.WithURLParams(newRequest(http.MethodGet, "/api/meetings/"+created.ID, nil), "id", created.ID)).
		Want(http.StatusOK).JSON(&fetched)
	if fetched.Transcript == "" || len(fetched.Actions) != 1 {
		t.Fatalf("get = %+v", fetched)
	}
	var list MeetingListResponse
	testutil.Call(t, testHandler.ListMeetings, newRequest(http.MethodGet, "/api/meetings?limit=5", nil)).
		Want(http.StatusOK).JSON(&list)
	found := false
	for _, m := range list.Meetings {
		if m.ID == created.ID {
			found = true
			if m.Transcript != "" {
				t.Fatalf("list must omit transcripts")
			}
		}
	}
	if !found {
		t.Fatalf("created meeting missing from list")
	}
}

func TestMeetingFinishWithoutLLMKeepsTranscript(t *testing.T) {
	stubSTT(t, "On reparle du budget lundi.")
	// testHandler.LLM is the disabled default outside withStubLLM.
	var created MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", nil)).
		Want(http.StatusCreated).JSON(&created)
	cleanupMeeting(t, created.ID)
	if !strings.HasPrefix(created.Title, "Meeting ") {
		t.Fatalf("default title = %q", created.Title)
	}
	testutil.Call(t, testHandler.AppendMeetingSegment,
		testutil.WithURLParams(audioUploadRequest(t, "/api/meetings/"+created.ID+"/segments", ""), "id", created.ID)).
		Want(http.StatusOK)
	var done MeetingResponse
	testutil.Call(t, testHandler.FinishMeeting,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+created.ID+"/finish", nil), "id", created.ID)).
		Want(http.StatusOK).JSON(&done)
	if done.Status != "done" || !done.SummaryUnavailable || len(done.Actions) != 0 || !strings.Contains(done.Transcript, "budget") {
		t.Fatalf("finish without LLM = %+v", done)
	}
}

func TestMeetingRequiresConfiguredSTT(t *testing.T) {
	prev := testHandler.STT
	testHandler.STT = stt.New(stt.Config{})
	t.Cleanup(func() { testHandler.STT = prev })

	body := testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", nil)).
		Want(http.StatusConflict).Map()
	if body["code"] != "stt_not_configured" {
		t.Fatalf("code = %v", body["code"])
	}
	testutil.Call(t, testHandler.TranscribeVoice, audioUploadRequest(t, "/api/voice/transcribe", "")).
		Want(http.StatusConflict)
}

func TestMeetingSegmentsAreCreatorOnly(t *testing.T) {
	stubSTT(t, "x")
	var created MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", nil)).
		Want(http.StatusCreated).JSON(&created)
	cleanupMeeting(t, created.ID)

	otherUser := dbfx.User(t, "Other Recorder", "other-recorder@example.com")
	dbfx.Member(t, testWorkspaceID, otherUser, "member")
	req := testutil.WithURLParams(audioUploadRequest(t, "/api/meetings/"+created.ID+"/segments", "1"), "id", created.ID)
	req.Header.Set("X-User-ID", otherUser)
	testutil.Call(t, testHandler.AppendMeetingSegment, req).Want(http.StatusForbidden)
	// Reading is open to any member.
	get := testutil.WithURLParams(newRequest(http.MethodGet, "/api/meetings/"+created.ID, nil), "id", created.ID)
	get.Header.Set("X-User-ID", otherUser)
	testutil.Call(t, testHandler.GetMeeting, get).Want(http.StatusOK)
}

// Delete is the recorder's, or a workspace admin/owner's. A plain member who
// did not record it gets 403, and `can_manage` tells the client which one it is
// before it renders the affordance.
func TestMeetingDeletePermissions(t *testing.T) {
	stubSTT(t, "x")
	newMeeting := func(t *testing.T) string {
		t.Helper()
		var created MeetingResponse
		testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", nil)).
			Want(http.StatusCreated).JSON(&created)
		cleanupMeeting(t, created.ID)
		return created.ID
	}

	plainUser := dbfx.User(t, "Plain Member", "meeting-delete-member@example.com")
	dbfx.Member(t, testWorkspaceID, plainUser, "member")
	adminUser := dbfx.User(t, "Meeting Admin", "meeting-delete-admin@example.com")
	dbfx.Member(t, testWorkspaceID, adminUser, "admin")

	// A non-recorder member sees can_manage=false and is refused.
	id := newMeeting(t)
	var seen MeetingResponse
	get := testutil.WithURLParams(newRequest(http.MethodGet, "/api/meetings/"+id, nil), "id", id)
	get.Header.Set("X-User-ID", plainUser)
	testutil.Call(t, testHandler.GetMeeting, get).Want(http.StatusOK).JSON(&seen)
	if seen.CanManage {
		t.Fatalf("plain member: can_manage = true, want false")
	}
	del := testutil.WithURLParams(newRequest(http.MethodDelete, "/api/meetings/"+id, nil), "id", id)
	del.Header.Set("X-User-ID", plainUser)
	testutil.Call(t, testHandler.DeleteMeeting, del).Want(http.StatusForbidden)

	// The admin may, even though someone else recorded it.
	del = testutil.WithURLParams(newRequest(http.MethodDelete, "/api/meetings/"+id, nil), "id", id)
	del.Header.Set("X-User-ID", adminUser)
	testutil.Call(t, testHandler.DeleteMeeting, del).Want(http.StatusNoContent)
	testutil.Call(t, testHandler.GetMeeting,
		testutil.WithURLParams(newRequest(http.MethodGet, "/api/meetings/"+id, nil), "id", id)).
		Want(http.StatusNotFound)

	// The recorder may delete their own.
	id = newMeeting(t)
	testutil.Call(t, testHandler.DeleteMeeting,
		testutil.WithURLParams(newRequest(http.MethodDelete, "/api/meetings/"+id, nil), "id", id)).
		Want(http.StatusNoContent)
}

func TestMeetingRename(t *testing.T) {
	stubSTT(t, "x")
	var created MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", map[string]string{"title": "Sync"})).
		Want(http.StatusCreated).JSON(&created)
	cleanupMeeting(t, created.ID)

	rename := func(t *testing.T, userID string, body any) *testutil.Response {
		t.Helper()
		req := testutil.WithURLParams(newRequest(http.MethodPatch, "/api/meetings/"+created.ID, body), "id", created.ID)
		if userID != "" {
			req.Header.Set("X-User-ID", userID)
		}
		return testutil.Call(t, testHandler.UpdateMeeting, req)
	}

	var renamed MeetingResponse
	rename(t, "", map[string]string{"title": "  Sprint review  "}).Want(http.StatusOK).JSON(&renamed)
	if renamed.Title != "Sprint review" {
		t.Fatalf("title = %q, want trimmed %q", renamed.Title, "Sprint review")
	}
	// An empty title would leave the meeting unnamed in every list.
	rename(t, "", map[string]string{"title": "   "}).Want(http.StatusBadRequest)
	rename(t, "", map[string]any{}).Want(http.StatusBadRequest)

	plainUser := dbfx.User(t, "Rename Member", "meeting-rename-member@example.com")
	dbfx.Member(t, testWorkspaceID, plainUser, "member")
	rename(t, plainUser, map[string]string{"title": "Hijacked"}).Want(http.StatusForbidden)

	// Over-long titles are cut, not refused: the client already caps them.
	rename(t, "", map[string]string{"title": strings.Repeat("é", 300)}).Want(http.StatusOK).JSON(&renamed)
	if n := len([]rune(renamed.Title)); n != meetingMaxTitleRunes {
		t.Fatalf("title runes = %d, want %d", n, meetingMaxTitleRunes)
	}
}
