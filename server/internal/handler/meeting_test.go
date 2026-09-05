package handler

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
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

// A meeting that finished without an LLM keeps its transcript and no summary.
// Re-summarizing it later — once a model is configured — must produce both the
// summary and the action items, without queueing a second copy of an item the
// same run already captured.
func TestMeetingResummarize(t *testing.T) {
	stubSTT(t, "Paul livre le connecteur Stripe vendredi.")
	var created MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", map[string]string{"title": "Point Stripe"})).
		Want(http.StatusCreated).JSON(&created)
	cleanupMeeting(t, created.ID)
	testutil.Call(t, testHandler.AppendMeetingSegment,
		testutil.WithURLParams(audioUploadRequest(t, "/api/meetings/"+created.ID+"/segments", "1"), "id", created.ID)).
		Want(http.StatusOK)

	// Finish with no LLM: transcript kept, nothing extracted.
	var done MeetingResponse
	testutil.Call(t, testHandler.FinishMeeting,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+created.ID+"/finish", nil), "id", created.ID)).
		Want(http.StatusOK).JSON(&done)
	if done.SummaryMarkdown != "" || len(done.Actions) != 0 {
		t.Fatalf("finish without LLM = %+v", done)
	}

	resummarize := func(t *testing.T, userID string) *testutil.Response {
		t.Helper()
		req := testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+created.ID+"/resummarize", nil), "id", created.ID)
		if userID != "" {
			req.Header.Set("X-User-ID", userID)
		}
		return testutil.Call(t, testHandler.ResummarizeMeeting, req)
	}

	plainUser := dbfx.User(t, "Resummarize Member", "meeting-resummarize-member@example.com")
	dbfx.Member(t, testWorkspaceID, plainUser, "member")
	resummarize(t, plainUser).Want(http.StatusForbidden)

	withStubLLM(t, stubLLMCompletion(t, http.StatusOK,
		`{"summary_markdown":"- Livraison Stripe vendredi","actions":[`+
			`{"title":"Livrer le connecteur Stripe","owner":"Paul","evidence":"Paul livre le connecteur Stripe vendredi."}]}`))

	var again MeetingResponse
	resummarize(t, "").Want(http.StatusOK).JSON(&again)
	if again.Status != "done" || again.SummaryMarkdown != "- Livraison Stripe vendredi" || len(again.Actions) != 1 {
		t.Fatalf("resummarize = %+v", again)
	}
	if again.SummaryUnavailable {
		t.Fatalf("summary_unavailable = true after a successful resummarize")
	}

	// A second run over the same transcript folds into the pending item that
	// already exists rather than queueing a duplicate.
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK,
		`{"summary_markdown":"- Livraison Stripe vendredi","actions":[`+
			`{"title":"Livrer le connecteur Stripe","owner":"Paul","evidence":"Paul livre le connecteur Stripe vendredi."}]}`))
	var third MeetingResponse
	resummarize(t, "").Want(http.StatusOK).JSON(&third)
	if len(third.Actions) != 1 || third.Actions[0].TriageItemID != again.Actions[0].TriageItemID {
		t.Fatalf("resummarize duplicated the action item: %+v", third.Actions)
	}
}

// A meeting still recording must be finished, not summarized behind the
// recorder's back.
func TestMeetingResummarizeRefusesRecording(t *testing.T) {
	stubSTT(t, "x")
	var created MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", nil)).
		Want(http.StatusCreated).JSON(&created)
	cleanupMeeting(t, created.ID)
	body := testutil.Call(t, testHandler.ResummarizeMeeting,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+created.ID+"/resummarize", nil), "id", created.ID)).
		Want(http.StatusConflict).Map()
	if body["code"] != "meeting_recording" {
		t.Fatalf("code = %v", body["code"])
	}
}

// A recorder who closes their tab leaves the meeting stuck in `recording`, and
// nothing else in the product can close it — so an admin has to be able to.
func TestMeetingFinishAllowsAdminAndRefusesPlainMember(t *testing.T) {
	stubSTT(t, "On reparle du budget lundi.")
	newMeeting := func(t *testing.T) string {
		t.Helper()
		var created MeetingResponse
		testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", nil)).
			Want(http.StatusCreated).JSON(&created)
		cleanupMeeting(t, created.ID)
		testutil.Call(t, testHandler.AppendMeetingSegment,
			testutil.WithURLParams(audioUploadRequest(t, "/api/meetings/"+created.ID+"/segments", "1"), "id", created.ID)).
			Want(http.StatusOK)
		return created.ID
	}
	finishAs := func(t *testing.T, id, userID string) *testutil.Response {
		t.Helper()
		req := testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+id+"/finish", nil), "id", id)
		req.Header.Set("X-User-ID", userID)
		return testutil.Call(t, testHandler.FinishMeeting, req)
	}

	plainUser := dbfx.User(t, "Finish Member", "meeting-finish-member@example.com")
	dbfx.Member(t, testWorkspaceID, plainUser, "member")
	adminUser := dbfx.User(t, "Finish Admin", "meeting-finish-admin@example.com")
	dbfx.Member(t, testWorkspaceID, adminUser, "admin")

	id := newMeeting(t)
	finishAs(t, id, plainUser).Want(http.StatusForbidden)

	var done MeetingResponse
	finishAs(t, id, adminUser).Want(http.StatusOK).JSON(&done)
	if done.Status != "done" {
		t.Fatalf("admin finish = %+v", done)
	}
}

// The detail page refetches right after finish, so GET has to reach the same
// verdict finish did — otherwise "no summary was written" flips back to "no
// summary yet" a moment after the user reads it.
func TestMeetingGetReportsSummaryUnavailable(t *testing.T) {
	stubSTT(t, "On reparle du budget lundi.")
	get := func(t *testing.T, id string) MeetingResponse {
		t.Helper()
		var out MeetingResponse
		testutil.Call(t, testHandler.GetMeeting,
			testutil.WithURLParams(newRequest(http.MethodGet, "/api/meetings/"+id, nil), "id", id)).
			Want(http.StatusOK).JSON(&out)
		return out
	}

	// Finished with a transcript but no LLM: unavailable, on finish AND on get.
	var created MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", nil)).
		Want(http.StatusCreated).JSON(&created)
	cleanupMeeting(t, created.ID)
	if get(t, created.ID).SummaryUnavailable {
		t.Fatalf("a meeting still recording reported summary_unavailable")
	}
	testutil.Call(t, testHandler.AppendMeetingSegment,
		testutil.WithURLParams(audioUploadRequest(t, "/api/meetings/"+created.ID+"/segments", "1"), "id", created.ID)).
		Want(http.StatusOK)
	testutil.Call(t, testHandler.FinishMeeting,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+created.ID+"/finish", nil), "id", created.ID)).
		Want(http.StatusOK)
	if !get(t, created.ID).SummaryUnavailable {
		t.Fatalf("get did not report summary_unavailable after a finish without an LLM")
	}

	// A meeting nobody spoke in has nothing to summarize: empty, not missing.
	var silent MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", nil)).
		Want(http.StatusCreated).JSON(&silent)
	cleanupMeeting(t, silent.ID)
	testutil.Call(t, testHandler.FinishMeeting,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+silent.ID+"/finish", nil), "id", silent.ID)).
		Want(http.StatusOK)
	if get(t, silent.ID).SummaryUnavailable {
		t.Fatalf("a silent meeting reported summary_unavailable")
	}
}

// Accepting a meeting's action item stamps origin_type='meeting' on the issue
// it becomes. The detail response has to carry it, or the UI can never link the
// issue back to the recording that asked for the work.
func TestIssueDetailCarriesMeetingOrigin(t *testing.T) {
	meetingID := dbfx.Issue(t, "placeholder-origin") // any workspace uuid works as an origin id
	issueID := dbfx.Issue(t, "Livrer le connecteur Stripe", testutil.Cols{
		"origin_type": meetingOriginType,
		"origin_id":   meetingID,
	})

	var got IssueResponse
	testutil.Call(t, testHandler.GetIssue,
		testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+issueID, nil), "id", issueID)).
		Want(http.StatusOK).JSON(&got)
	if got.OriginType == nil || *got.OriginType != meetingOriginType {
		t.Fatalf("origin_type = %v, want %q", got.OriginType, meetingOriginType)
	}
	if got.OriginID == nil || *got.OriginID != meetingID {
		t.Fatalf("origin_id = %v, want %q", got.OriginID, meetingID)
	}

	// An issue with no origin omits the fields entirely: absent means "this
	// response did not resolve one", which is what the client keys off.
	plainID := dbfx.Issue(t, "Typed by hand")
	body := testutil.Call(t, testHandler.GetIssue,
		testutil.WithURLParams(newRequest(http.MethodGet, "/api/issues/"+plainID, nil), "id", plainID)).
		Want(http.StatusOK).Map()
	if _, ok := body["origin_type"]; ok {
		t.Fatalf("origin_type present on an issue with no origin: %v", body["origin_type"])
	}
}

// TestMeetingPublishesRealtimeEvents pins the push that lets a second client
// (and the tab that started the recording) see a meeting appear, finish
// summarizing, get renamed, and disappear — without waiting for the detail
// view's 3s poll, which only runs while a meeting is `summarizing` and never
// covers the list at all.
func TestMeetingPublishesRealtimeEvents(t *testing.T) {
	stubSTT(t, "Paul livre le connecteur Stripe vendredi.")
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK,
		`{"summary_markdown":"- Livraison Stripe vendredi","actions":[]}`))

	var mu sync.Mutex
	var seen []string
	record := func(e events.Event) {
		mu.Lock()
		defer mu.Unlock()
		payload, _ := e.Payload.(map[string]any)
		status, _ := payload["status"].(string)
		seen = append(seen, e.Type+":"+status)
	}
	for _, event := range []string{
		protocol.EventMeetingCreated,
		protocol.EventMeetingUpdated,
		protocol.EventMeetingDeleted,
	} {
		testHandler.Bus.Subscribe(event, record)
	}
	recorded := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
	var created MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting,
		newRequest(http.MethodPost, "/api/meetings", map[string]string{"title": "Realtime"})).
		Want(http.StatusCreated).JSON(&created)
	cleanupMeeting(t, created.ID)

	testutil.Call(t, testHandler.FinishMeeting,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+created.ID+"/finish", nil), "id", created.ID)).
		Want(http.StatusOK)

	testutil.Call(t, testHandler.UpdateMeeting,
		testutil.WithURLParams(newRequest(http.MethodPatch, "/api/meetings/"+created.ID, map[string]string{"title": "Renamed"}), "id", created.ID)).
		Want(http.StatusOK)

	testutil.Call(t, testHandler.DeleteMeeting,
		testutil.WithURLParams(newRequest(http.MethodDelete, "/api/meetings/"+created.ID, nil), "id", created.ID)).
		Want(http.StatusNoContent)

	want := []string{
		"meeting:created:recording",
		// Finish announces the entry into `summarizing` BEFORE the summary
		// runs: the clients still painting "recording" are the ones that most
		// need to stop.
		"meeting:updated:summarizing",
		"meeting:updated:done",
		"meeting:updated:done", // rename
		"meeting:deleted:done",
	}
	got := recorded()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %q, want %q (full sequence %v)", i, got[i], want[i], got)
		}
	}
}

// Editing one transcript paragraph after the meeting is done (JEF final A,
// point 1). `seq` is the line index; the summary is deliberately untouched.
func TestMeetingSegmentEdit(t *testing.T) {
	stubSTT(t, "unused") // create requires a configured provider
	var created MeetingResponse
	testutil.Call(t, testHandler.CreateMeeting, newRequest(http.MethodPost, "/api/meetings", map[string]string{"title": "Edit me"})).
		Want(http.StatusCreated).JSON(&created)
	cleanupMeeting(t, created.ID)

	for _, text := range []string{"Speaker 1: On livre vendredi.", "Speaker 2: Non, lundi."} {
		testutil.Call(t, testHandler.AppendMeetingSegment,
			testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+created.ID+"/segments", map[string]string{"text": text}), "id", created.ID)).
			Want(http.StatusOK)
	}

	editReq := func(seq string, body any, userID string) *http.Request {
		req := testutil.WithURLParams(
			newRequest(http.MethodPatch, "/api/meetings/"+created.ID+"/segments/"+seq, body),
			"id", created.ID, "seq", seq)
		if userID != "" {
			req.Header.Set("X-User-ID", userID)
		}
		return req
	}

	// Still recording: refused, with a code the client can branch on.
	conflict := testutil.Call(t, testHandler.UpdateMeetingSegment, editReq("0", map[string]string{"text": "x"}, "")).
		Want(http.StatusConflict).Map()
	if conflict["code"] != "meeting_not_done" {
		t.Fatalf("code = %v, want meeting_not_done", conflict["code"])
	}

	testutil.Call(t, testHandler.FinishMeeting,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/meetings/"+created.ID+"/finish", nil), "id", created.ID)).
		Want(http.StatusOK)

	var edited MeetingResponse
	testutil.Call(t, testHandler.UpdateMeetingSegment,
		editReq("1", map[string]string{"text": "  Speaker 2: Non,\nlundi matin.  "}, "")).
		Want(http.StatusOK).JSON(&edited)
	// The newline is collapsed: one segment is one line, or every following
	// index would shift.
	want := "Speaker 1: On livre vendredi.\nSpeaker 2: Non, lundi matin."
	if edited.Transcript != want {
		t.Fatalf("transcript = %q, want %q", edited.Transcript, want)
	}

	// Out of range, and empty text.
	testutil.Call(t, testHandler.UpdateMeetingSegment, editReq("2", map[string]string{"text": "x"}, "")).
		Want(http.StatusNotFound)
	testutil.Call(t, testHandler.UpdateMeetingSegment, editReq("0", map[string]string{"text": "   "}, "")).
		Want(http.StatusBadRequest)

	// A plain member who did not record it may not edit the transcript.
	otherUser := dbfx.User(t, "Other Editor", "other-editor@example.com")
	dbfx.Member(t, testWorkspaceID, otherUser, "member")
	testutil.Call(t, testHandler.UpdateMeetingSegment, editReq("0", map[string]string{"text": "x"}, otherUser)).
		Want(http.StatusForbidden)

	var fetched MeetingResponse
	testutil.Call(t, testHandler.GetMeeting,
		testutil.WithURLParams(newRequest(http.MethodGet, "/api/meetings/"+created.ID, nil), "id", created.ID)).
		Want(http.StatusOK).JSON(&fetched)
	if fetched.Transcript != want {
		t.Fatalf("re-read transcript = %q", fetched.Transcript)
	}
}
