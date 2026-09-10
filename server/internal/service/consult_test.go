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
