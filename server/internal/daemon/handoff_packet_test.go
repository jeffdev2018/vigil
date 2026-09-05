package daemon

import (
	"strings"
	"testing"
)

// Handoff packets (K17): the per-turn prompt shows the previous hand's
// record, failed attempts included, and asks for a packet in return.

func TestPromptRendersHandoffPacket(t *testing.T) {
	t.Parallel()
	task := Task{ID: "t", IssueID: "MUL-1", HandoffNote: "scope: only the API", ResumeFromCheckpointSeq: 12, HandoffPacket: &HandoffPacket{
		Objective: "Ship the fix", Decisions: []string{"keep the table"}, FailedAttempts: []string{"dropping it broke tests"}, NextAction: "open the PR",
	}}
	prompt := buildPromptBody(task, "claude")
	for _, want := range []string{"resumes automatically after an infrastructure interruption", "reached transcript message 12", "handoff note", "scope: only the API", "handoff packet", "- Objective: Ship the fix", "  - keep the table", "Failed attempts", "dropping it broke tests", "- Next action: open the PR", "POST /api/issues/{issue}/handoff-packet"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	if renderHandoffPacket(nil) != "" {
		t.Fatal("no packet, no section")
	}
}
