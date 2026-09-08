package agent

import (
	"runtime"
	"testing"
	"time"
)

// F03 · Codex emits the same `response` message as Claude Code. The decision
// matrix lives in TestEmitFinalResponse (claude_response_type_test.go); this
// proves Codex reaches the seam with the deliverable the app-server labelled
// `phase: "final_answer"`, and not with its intermediate narration.

func TestCodexExecuteEmitsFinalAnswerAsResponse(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	const (
		narration = "Reading the retry loop..."
		answer    = "The retry loop is the cause."
	)
	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-response"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-response","item":{"type":"agentMessage","id":"m1","text":"`+narration+`"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-response","item":{"type":"agentMessage","id":"m2","text":"`+answer+`","phase":"final_answer"}}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"turn/completed","params":{"threadId":"thr-response","turn":{"id":"turn-1","status":"completed"}}}'`+"\n")

	result, msgs := executeFakeCodexCollectingMessages(t, fakePath,
		ExecOptions{Timeout: 5 * time.Second}, 10*time.Second)
	if result.Status != "completed" {
		t.Fatalf("status = %q, want completed (error %q)", result.Status, result.Error)
	}
	if result.Output != answer {
		t.Fatalf("output = %q, want the labelled final answer %q", result.Output, answer)
	}

	responses := messagesOfType(msgs, MessageResponse)
	if len(responses) != 1 {
		t.Fatalf("response messages = %d, want exactly 1: %+v", len(responses), msgs)
	}
	if responses[0].Content != answer {
		t.Errorf("response content = %q, want %q — the narration must not be promoted",
			responses[0].Content, answer)
	}
}

func TestCodexExecuteEmitsNoResponseWhenTheTurnFails(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fixture is POSIX-only")
	}

	// The app-server dies after the turn starts: the run fails, so whatever
	// text it managed to stream is narration of an abandoned attempt, never a
	// deliverable.
	fakePath := writeFakeCodexAppServer(t, ""+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":1,"result":{}}'`+"\n"+
		`read line`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":2,"result":{"thread":{"id":"thr-fail"}}}'`+"\n"+
		`read line`+"\n"+
		`echo '{"jsonrpc":"2.0","id":3,"result":{}}'`+"\n"+
		`echo '{"jsonrpc":"2.0","method":"item/completed","params":{"threadId":"thr-fail","item":{"type":"agentMessage","id":"m1","text":"Half a thought"}}}'`+"\n"+
		`exit 3`+"\n")

	result, msgs := executeFakeCodexCollectingMessages(t, fakePath,
		ExecOptions{Timeout: 5 * time.Second, SemanticInactivityTimeout: 3 * time.Second},
		15*time.Second)
	if result.Status == "completed" {
		t.Fatalf("fixture was supposed to fail, got completed with output %q", result.Output)
	}
	if got := messagesOfType(msgs, MessageResponse); len(got) != 0 {
		t.Fatalf("a failed run emitted %d response message(s): %+v", len(got), got)
	}
}
