package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Multichannel digest (K64): the briefing reaches every configured channel
// once per day, narrated when an LLM answers and structured either way; a
// channel type without a sender or without an active installation is
// skipped and audited; the day's send record lists the channels reached.

type fakeDigestSender struct{ calls []string }

func (f *fakeDigestSender) SendDigest(_ context.Context, inst db.ChannelInstallation, chatID, text string) (string, error) {
	f.calls = append(f.calls, chatID+"|"+text)
	return "msg-" + uuidToString(inst.ID)[:8], nil
}

func TestBriefingDigestFormat(t *testing.T) {
	b := MorningBriefingResponse{Date: "2026-09-04", Narrative: "One PR merged, one decision waits.", Merged: []BriefingItem{{IssueID: "i1", Identifier: "ACME-1", Title: "Ship it"}}, Blocked: []BriefingItem{{IssueID: "i2", Identifier: "ACME-2", Title: "Stuck", Reason: "waiting on a decision", PendingDecisions: 1}}}
	text := formatBriefingDigest(b, "https://app.example.test/", "acme")
	for _, want := range []string{"Morning briefing — 2026-09-04", "One PR merged, one decision waits.", "Done in the last 24 hours (1)", "ACME-1 Ship it — https://app.example.test/acme/issues/i1", "↳ waiting on a decision", "1 decision(s) wait for you — answer them: https://app.example.test/acme/inbox?view=decisions"} {
		if !strings.Contains(text, want) {
			t.Fatalf("digest missing %q:\n%s", want, text)
		}
	}
	empty := formatBriefingDigest(MorningBriefingResponse{Date: "2026-09-04"}, "https://app.example.test", "acme")
	if !strings.Contains(empty, "Nothing done overnight") || strings.Contains(empty, "decision(s) wait") {
		t.Fatalf("empty digest = %q", empty)
	}
	cfg, ok := service.MorningBriefingSettings([]byte(`{"morning_briefing":{"enabled":true,"channels":[{"type":"slack","chat_id":"C1"},{"type":"","chat_id":"x"},{"type":"telegram"}]}}`))
	if !ok || len(cfg.Channels) != 1 || cfg.Channels[0].ChatID != "C1" {
		t.Fatalf("channels = %+v", cfg.Channels)
	}
}

func TestBriefingDeliversToChannelsOncePerDay(t *testing.T) {
	rememberSettings(t)
	fake := &fakeDigestSender{}
	prevSenders, prevLLM := testHandler.DigestSenders, testHandler.LLM
	testHandler.DigestSenders = map[string]ChannelDigestSender{"slack": fake}
	t.Cleanup(func() { testHandler.DigestSenders, testHandler.LLM = prevSenders, prevLLM })
	inst := dbfx.Insert(t, "channel_installation", testutil.Cols{"workspace_id": testWorkspaceID, "agent_id": uuidToString(chatTitleTestAgentID(t)), "channel_type": "slack", "config": `{}`, "status": "active", "installer_user_id": testUserID})
	_ = inst
	issue := dbfx.Issue(t, "Digest merged issue "+uuid.NewString()[:6], testutil.Cols{"status": "done", "completed_at": testutil.Raw("now() - interval '1 hour'")})
	dbfx.Exec(t, `UPDATE workspace SET settings = COALESCE(settings, '{}'::jsonb) || '{"morning_briefing":{"enabled":true,"hour":0,"timezone":"UTC","channels":[{"type":"slack","chat_id":"C-digest"},{"type":"lark","chat_id":"oc_x"}]}}'::jsonb WHERE id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM morning_briefing_sent WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'morning_briefing'`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM channel_installation WHERE id = $1`, inst)
	})
	testPool.Exec(context.Background(), `DELETE FROM morning_briefing_sent WHERE workspace_id = $1`, testWorkspaceID)
	withStubLLM(t, stubLLMCompletion(t, http.StatusOK, `{"narrative":"One issue done overnight; nothing blocks the team."}`))
	ctx := context.Background()
	briefing, err := testHandler.composeBriefing(ctx, parseUUID(testWorkspaceID), time.Now(), time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if briefing.Narrative != "One issue done overnight; nothing blocks the team." {
		t.Fatalf("narrative = %q", briefing.Narrative)
	}
	cfg, _ := service.MorningBriefingSettings(mustWorkspaceSettings(t))
	did, err := testHandler.sendBriefing(ctx, parseUUID(testWorkspaceID), briefing, "member", testUserID, cfg.Channels)
	if err != nil || !did {
		t.Fatalf("send = %v %v", did, err)
	}
	if len(fake.calls) != 1 || !strings.HasPrefix(fake.calls[0], "C-digest|") || !strings.Contains(fake.calls[0], briefing.Narrative) || !strings.Contains(fake.calls[0], "Digest merged issue") {
		t.Fatalf("slack calls = %v", fake.calls)
	}
	_ = issue
	var delivered string
	dbfx.QueryRow(t, `SELECT channels_delivered::text FROM morning_briefing_sent WHERE workspace_id = $1 AND sent_for_date = $2`, testWorkspaceID, briefing.Date).Scan(&delivered)
	if !strings.Contains(delivered, `"inbox"`) || !strings.Contains(delivered, `"slack"`) || strings.Contains(delivered, `"lark"`) {
		t.Fatalf("channels_delivered = %s", delivered)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2 AND details->>'type' = 'slack' AND details->>'narrated' = 'true'`, testWorkspaceID, AuditBriefingChannelSent); n != 1 {
		t.Fatalf("channel_sent audit rows = %d", n)
	}
	// The API exposes where the day's briefing went.
	var today MorningBriefingResponse
	testutil.Call(t, inboxWorkspaceHandler(testHandler.GetMorningBriefingToday), inboxRequest(http.MethodGet, "/api/morning-briefing/today", testWorkspaceID)).Want(http.StatusOK).JSON(&today)
	if len(today.ChannelsDelivered) != 2 || today.ChannelsDelivered[1] != "slack" {
		t.Fatalf("today.channels_delivered = %v", today.ChannelsDelivered)
	}
	// A second send the same day is refused before any channel is hit.
	if did, _ := testHandler.sendBriefing(ctx, parseUUID(testWorkspaceID), briefing, "member", testUserID, cfg.Channels); did || len(fake.calls) != 1 {
		t.Fatalf("second send: did=%v calls=%d", did, len(fake.calls))
	}
	// Without an LLM the structured digest still goes out (next day).
	testHandler.LLM = nil
	testPool.Exec(ctx, `DELETE FROM morning_briefing_sent WHERE workspace_id = $1`, testWorkspaceID)
	plain, _ := testHandler.composeBriefing(ctx, parseUUID(testWorkspaceID), time.Now(), time.UTC)
	if plain.Narrative != "" {
		t.Fatalf("narrative without LLM = %q", plain.Narrative)
	}
	if did, err := testHandler.sendBriefing(ctx, parseUUID(testWorkspaceID), plain, "member", testUserID, cfg.Channels); err != nil || !did || len(fake.calls) != 2 || !strings.Contains(fake.calls[1], "Done in the last 24 hours") {
		t.Fatalf("plain send: did=%v err=%v calls=%v", did, err, fake.calls)
	}
}

func mustWorkspaceSettings(t *testing.T) []byte {
	t.Helper()
	ws, err := testHandler.Queries.GetWorkspace(context.Background(), parseUUID(testWorkspaceID))
	if err != nil {
		t.Fatal(err)
	}
	return ws.Settings
}
