package agent

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

// F03 · the `response` message type. A run's deliverable answer is emitted once,
// separately from the MessageText turns that narrated the work, so a transcript
// can show which text was the conclusion.
//
// The canonical matrix for the emit/skip decision is TestEmitFinalResponse
// below; the end-to-end tests prove the backends actually reach it.

// TestEmitFinalResponse pins the whole contract of the shared seam.
func TestEmitFinalResponse(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		status string
		output string
		want   bool
	}{
		{"successful run with an answer", "completed", "Fixed the redirect.", true},
		// A successful run that produced no deliverable text: empty output is
		// the platform's "no final text" signal, and inventing a message here
		// would put an empty response bubble in every such transcript.
		{"successful run with no answer", "completed", "", false},
		// A failed run has no deliverable by contract (finalizeStreamResult
		// returns an empty output), and its error text is not an answer.
		{"failed run", "failed", "", false},
		{"failed run carrying text", "failed", "API Error: 401", false},
		{"timeout", "timeout", "", false},
		{"aborted", "aborted", "partial work", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ch := make(chan Message, 4)
			emitFinalResponse(ch, tc.status, tc.output)
			close(ch)

			var got []Message
			for msg := range ch {
				got = append(got, msg)
			}
			if !tc.want {
				if len(got) != 0 {
					t.Fatalf("emitFinalResponse(%q, %q) emitted %+v, want nothing",
						tc.status, tc.output, got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("emitFinalResponse(%q, %q) emitted %d messages, want exactly 1",
					tc.status, tc.output, len(got))
			}
			if got[0].Type != MessageResponse {
				t.Errorf("type = %q, want %q", got[0].Type, MessageResponse)
			}
			if got[0].Content != tc.output {
				t.Errorf("content = %q, want the run's output %q", got[0].Content, tc.output)
			}
		})
	}
}

// runClaudeFixtureCollectingMessages is runClaudeFixture with the message
// stream kept instead of discarded.
func runClaudeFixtureCollectingMessages(t *testing.T, script string) (Result, []Message) {
	t.Helper()

	fakePath := filepath.Join(t.TempDir(), "claude")
	writeTestExecutable(t, fakePath, []byte(script))

	backend, err := New("claude", Config{
		ExecutablePath: fakePath,
		Env:            map[string]string{"IS_SANDBOX": "1"},
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new claude backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session, err := backend.Execute(ctx, "prompt", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}

	var (
		mu       sync.Mutex
		messages []Message
		drained  = make(chan struct{})
	)
	go func() {
		defer close(drained)
		for msg := range session.Messages {
			mu.Lock()
			messages = append(messages, msg)
			mu.Unlock()
		}
	}()

	select {
	case result, ok := <-session.Result:
		if !ok {
			t.Fatal("result channel closed without a value")
		}
		<-drained
		mu.Lock()
		defer mu.Unlock()
		return result, append([]Message(nil), messages...)
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for result")
		return Result{}, nil
	}
}

func messagesOfType(msgs []Message, want MessageType) []Message {
	var out []Message
	for _, m := range msgs {
		if m.Type == want {
			out = append(out, m)
		}
	}
	return out
}

func TestClaudeExecuteEmitsFinalTextAsResponse(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	const answer = "Fixed the redirect and pushed."
	script := "#!/bin/sh\n" +
		"IFS= read -r _\n" +
		`echo '{"type":"system","subtype":"init","session_id":"sess-ok"}'` + "\n" +
		`echo '{"type":"assistant","message":{"model":"claude-x","content":[{"type":"text","text":"Looking at the router..."}]}}'` + "\n" +
		`echo '{"type":"assistant","message":{"model":"claude-x","content":[{"type":"text","text":"` + answer + `"}]}}'` + "\n" +
		`echo '{"type":"result","subtype":"success","is_error":false,"session_id":"sess-ok","result":"` + answer + `"}'` + "\n"

	result, msgs := runClaudeFixtureCollectingMessages(t, script)
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed (error %q)", result.Status, result.Error)
	}

	responses := messagesOfType(msgs, MessageResponse)
	if len(responses) != 1 {
		t.Fatalf("response messages = %d, want exactly 1: %+v", len(responses), msgs)
	}
	if responses[0].Content != answer {
		t.Errorf("response content = %q, want the run's deliverable %q", responses[0].Content, answer)
	}

	// The narration turns still stream as text — the new type separates the
	// conclusion from the narration, it does not replace the transcript.
	if len(messagesOfType(msgs, MessageText)) == 0 {
		t.Error("intermediate narration must still arrive as text messages")
	}
}

func TestClaudeExecuteEmitsNoResponseOnFailure(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	script := "#!/bin/sh\n" +
		"IFS= read -r _\n" +
		`echo '{"type":"system","subtype":"init","session_id":"sess-bad"}'` + "\n" +
		`echo '{"type":"assistant","message":{"model":"claude-x","content":[{"type":"text","text":"Trying..."}]}}'` + "\n" +
		`echo '{"type":"result","subtype":"error","is_error":true,"session_id":"sess-bad","result":"API Error: 401 Unauthorized"}'` + "\n"

	result, msgs := runClaudeFixtureCollectingMessages(t, script)
	if result.Status != "failed" {
		t.Fatalf("status = %q, want failed", result.Status)
	}
	if got := messagesOfType(msgs, MessageResponse); len(got) != 0 {
		t.Fatalf("a failed run emitted %d response message(s): %+v; an error is not an answer",
			len(got), got)
	}
}
