package telegram

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// Inline approvals on Telegram: an ask renders as an inline keyboard, and a
// press comes back as a callback_query that is answered with a toast and an
// edit of the message that carried the buttons.

func TestInlineKeyboardRendersActions(t *testing.T) {
	long := "decide|" + strings.Repeat("x", 70)
	kb := InlineKeyboard([]channel.DigestAction{
		{Label: "Keep it", Value: "decide|d|aaa|bbb|0"},
		{Label: "Open the issue", URL: "https://app/acme/issues/i1"},
		{Label: "Too long to carry", Value: long},
	})
	if kb == nil || len(kb.InlineKeyboard) != 2 {
		t.Fatalf("keyboard = %+v", kb)
	}
	if b := kb.InlineKeyboard[0][0]; b.Text != "Keep it" || b.CallbackData != "decide|d|aaa|bbb|0" || b.URL != "" {
		t.Fatalf("callback button = %+v", b)
	}
	if b := kb.InlineKeyboard[1][0]; b.URL != "https://app/acme/issues/i1" || b.CallbackData != "" {
		t.Fatalf("link button = %+v", b)
	}
	// A payload over Telegram's 64-byte callback_data cap is dropped rather
	// than sent as a button the API would reject.
	if len(long) <= telegramCallbackDataCap {
		t.Fatal("the oversized fixture is not oversized")
	}
	if InlineKeyboard(nil) != nil {
		t.Fatal("no actions must render no keyboard")
	}
}

func TestCallbackQueryDecidesAndEditsMessage(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var got map[string]any
		_ = json.Unmarshal(body, &got)
		mu.Lock()
		calls[r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]] = got
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	var seen []string
	c := &telegramChannel{
		botID: 999, botUsername: "my_bot",
		api:    newBotAPI(srv.URL, "999:abc", srv.Client()),
		logger: testLogger(),
		onApproval: func(_ context.Context, appID, userID, data string) string {
			seen = append(seen, appID+"/"+userID+"/"+data)
			return "Answered «Ship it?» with \"Keep it\"."
		},
	}
	err := c.dispatch(context.Background(), Update{UpdateID: 1, CallbackQuery: &CallbackQuery{
		ID: "cbq-1", From: &User{ID: 77}, Data: "decide|d|aaa|bbb|0",
		Message: &Message{MessageID: 12, Chat: Chat{ID: 42, Type: "group"}, Text: "Decision needed"},
	}})
	if err != nil {
		t.Fatalf("dispatch = %v", err)
	}
	if len(seen) != 1 || seen[0] != "999/77/decide|d|aaa|bbb|0" {
		t.Fatalf("decide calls = %v", seen)
	}
	mu.Lock()
	answer, ok := calls["answerCallbackQuery"]
	if !ok || answer["callback_query_id"] != "cbq-1" || !strings.Contains(answer["text"].(string), "Keep it") {
		t.Fatalf("answerCallbackQuery = %+v", answer)
	}
	edit, ok := calls["editMessageText"]
	if !ok || edit["message_id"].(float64) != 12 || !strings.Contains(edit["text"].(string), "Keep it") {
		t.Fatalf("editMessageText = %+v", edit)
	}
	// Omitting reply_markup is what retires the buttons.
	if _, has := edit["reply_markup"]; has {
		t.Fatalf("the edit must clear the keyboard: %+v", edit)
	}
	mu.Unlock()

	// A press that is not ours is left entirely alone.
	seen = nil
	_ = c.dispatch(context.Background(), Update{UpdateID: 2, CallbackQuery: &CallbackQuery{
		ID: "cbq-2", From: &User{ID: 77}, Data: "some_other_feature|x",
	}})
	if len(seen) != 0 {
		t.Fatalf("a foreign callback must not decide: %v", seen)
	}
}
