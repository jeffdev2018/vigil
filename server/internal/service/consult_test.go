package service

import (
	"strings"
	"testing"
)

func TestBuildConsultUserPrompt(t *testing.T) {
	got := BuildConsultUserPrompt("  Which index?  ", "")
	if got != "Question:\nWhich index?" {
		t.Fatalf("question-only prompt = %q", got)
	}

	got = BuildConsultUserPrompt("Which index?", "  table has 40M rows ")
	if !strings.Contains(got, "Which index?") || !strings.Contains(got, "table has 40M rows") {
		t.Fatalf("context prompt missing parts: %q", got)
	}
	// The context must be fenced off as data: a question smuggling instructions
	// into the context payload must land inside the <context> delimiters.
	if !strings.Contains(got, "<context>\ntable has 40M rows\n</context>") {
		t.Fatalf("context not delimited: %q", got)
	}
}

func TestExtractConsultAnswer(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"shaped reply", `{"answer":"use Postgres"}`, "use Postgres"},
		{"extra whitespace in answer kept as-is", `{"answer":"  spaced  "}`, "  spaced  "},
		// JSON-object mode guarantees valid JSON, never the shape: a reply
		// without a string answer field degrades to the raw text rather than
		// failing a consult that produced a usable reply.
		{"wrong shape", `{"text":"hello"}`, `{"text":"hello"}`},
		{"empty answer field", `{"answer":"  "}`, `{"answer":"  "}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExtractConsultAnswer(tc.raw); got != tc.want {
				t.Fatalf("ExtractConsultAnswer(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestConsultSystemPromptMentionsJSON(t *testing.T) {
	// llm.Client.GenerateJSON requests response_format=json_object, which
	// OpenAI-compatible endpoints reject unless the prompt mentions JSON.
	if !strings.Contains(ConsultSystemPrompt, "JSON") {
		t.Fatal("consult system prompt must mention JSON for json_object mode")
	}
}

// TestConsultPromptIsBoundedByTheServer covers the ceiling the caller cannot
// choose. Before it existed, the only limit on the context was the endpoint's
// 1 MB request body, so a running task decided by itself how much of what it
// could read left the deployment.
func TestConsultPromptIsBoundedByTheServer(t *testing.T) {
	head := strings.Repeat("H", 40*1024)
	tail := strings.Repeat("T", 40*1024)
	got := BuildConsultUserPrompt(strings.Repeat("q", 9000), head+tail)

	if len(got) > consultQuestionCap+consultContextCap+512 {
		t.Errorf("prompt is %d bytes, above the question and context budgets", len(got))
	}
	// Both ends survive: a pasted file puts the subject at the top and the
	// failure at the bottom, so keeping only the head would lose the point.
	if !strings.Contains(got, "HHHH") || !strings.Contains(got, "TTTT") {
		t.Error("the truncated context kept only one end")
	}
	if !strings.Contains(got, "middle truncated") {
		t.Error("truncation is silent; the model cannot tell a cut from the whole")
	}
	// A short consult is untouched, so the bound costs the common case nothing.
	short := BuildConsultUserPrompt("Which index?", "table has 40M rows")
	if !strings.Contains(short, "table has 40M rows") || strings.Contains(short, "truncated") {
		t.Errorf("a short context was altered: %q", short)
	}
}
