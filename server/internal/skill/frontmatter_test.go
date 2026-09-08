package skill

import (
	"strings"
	"testing"
)

func TestParseSkillFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantName string
		wantDesc string
	}{
		{
			name:     "single line",
			content:  "---\nname: foo\ndescription: bar\n---\nbody",
			wantName: "foo",
			wantDesc: "bar",
		},
		{
			name:     "double quoted",
			content:  "---\nname: \"foo\"\ndescription: \"hello world\"\n---\nbody",
			wantName: "foo",
			wantDesc: "hello world",
		},
		{
			name:     "single quoted",
			content:  "---\nname: 'foo'\ndescription: 'hello world'\n---\nbody",
			wantName: "foo",
			wantDesc: "hello world",
		},
		{
			// Interior newlines survive; only the chomped trailing one is dropped.
			name:     "literal block scalar keeps interior newlines",
			content:  "---\nname: foo\ndescription: |\n  line1\n  line2\n---\nbody",
			wantName: "foo",
			wantDesc: "line1\nline2",
		},
		{
			name:     "literal strip chomping drops trailing newline",
			content:  "---\nname: foo\ndescription: |-\n  line1\n  line2\n---\nbody",
			wantName: "foo",
			wantDesc: "line1\nline2",
		},
		{
			name:     "folded block scalar joins with spaces",
			content:  "---\nname: foo\ndescription: >\n  line1\n  line2\n---\nbody",
			wantName: "foo",
			wantDesc: "line1 line2",
		},
		{
			name:     "CRLF line endings",
			content:  "---\r\nname: foo\r\ndescription: bar\r\n---\r\nbody",
			wantName: "foo",
			wantDesc: "bar",
		},
		{
			name:     "no frontmatter returns empty",
			content:  "no frontmatter here",
			wantName: "",
			wantDesc: "",
		},
		{
			name:     "unterminated frontmatter returns empty",
			content:  "---\nname: foo\ndescription: bar\n",
			wantName: "",
			wantDesc: "",
		},
		{
			name:     "invalid YAML falls back to empty",
			content:  "---\n: : : not valid\n---\nbody",
			wantName: "",
			wantDesc: "",
		},
		{
			name:     "name only",
			content:  "---\nname: foo\n---\nbody",
			wantName: "foo",
			wantDesc: "",
		},
		{
			name:     "description only",
			content:  "---\ndescription: bar\n---\nbody",
			wantName: "",
			wantDesc: "bar",
		},
		{
			name:     "leading blank line is not frontmatter",
			content:  "\n---\nname: foo\ndescription: bar\n---\nbody",
			wantName: "",
			wantDesc: "",
		},
		{
			// The non-greedy capture stops at the first closing fence, so a
			// later "---" in the body must not extend the frontmatter block.
			name:     "triple dash in body stops at first fence",
			content:  "---\nname: foo\ndescription: bar\n---\nintro\n---\nmore",
			wantName: "foo",
			wantDesc: "bar",
		},
		{
			// Parity with the TS coercion: non-string scalars render as their
			// literal form rather than being dropped.
			name:     "non-string scalars coerce to literal",
			content:  "---\nname: 123\ndescription: 456\n---\nbody",
			wantName: "123",
			wantDesc: "456",
		},
		{
			// A structured value where a scalar belongs (authoring mistake) must
			// not discard the sibling name; the value is JSON-encoded, matching
			// the TS parseFrontmatter behaviour.
			name:     "sequence description keeps name and is JSON-encoded",
			content:  "---\nname: my-skill\ndescription:\n  - first feature\n  - second feature\n---\nbody",
			wantName: "my-skill",
			wantDesc: `["first feature","second feature"]`,
		},
		{
			name:     "mapping description keeps name and is JSON-encoded",
			content:  "---\nname: my-skill\ndescription:\n  a: 1\n  b: 2\n---\nbody",
			wantName: "my-skill",
			wantDesc: `{"a":1,"b":2}`,
		},
		{
			// MUL-5645: padding never reaches storage, so an imported skill can
			// never differ from its own trimmed form in the editor.
			name:     "surrounding whitespace is trimmed off both fields",
			content:  "---\nname: \"  foo  \"\ndescription: \"  hello world\\n\"\n---\nbody",
			wantName: "foo",
			wantDesc: "hello world",
		},
		{
			// Reproduction for issue #3495: Chinese block scalar.
			name: "issue 3495 chinese literal block scalar",
			content: "---\n" +
				"name: requirements-workshop\n" +
				"description: |\n" +
				"  当用户想要开发新功能、讨论需求、梳理业务逻辑时触发。通过多轮对话将模糊的想法转化为结构化的需求文档，为后续技术方案设计提供输入。\n" +
				"  适用场景：用户说\"我想实现XX功能\"、\"讨论一下这个需求\"、\"帮我分析一下怎么做\"、\"这个需求该怎么设计\"、\"写个需求文档\"等。\n" +
				"  本skill只负责产出需求文档，不进入技术设计或代码实现阶段。\n" +
				"---\nbody",
			wantName: "requirements-workshop",
			wantDesc: "当用户想要开发新功能、讨论需求、梳理业务逻辑时触发。通过多轮对话将模糊的想法转化为结构化的需求文档，为后续技术方案设计提供输入。\n" +
				"适用场景：用户说\"我想实现XX功能\"、\"讨论一下这个需求\"、\"帮我分析一下怎么做\"、\"这个需求该怎么设计\"、\"写个需求文档\"等。\n" +
				"本skill只负责产出需求文档，不进入技术设计或代码实现阶段。",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotDesc := ParseSkillFrontmatter(tt.content)
			if gotName != tt.wantName {
				t.Errorf("name: got %q, want %q", gotName, tt.wantName)
			}
			if gotDesc != tt.wantDesc {
				t.Errorf("description: got %q, want %q", gotDesc, tt.wantDesc)
			}
		})
	}
}

// ── DAEMON.md (F24 / JEF-15) ────────────────────────────────────────────────

const validDaemon = `---
name: Nightly triage
role: |
  Sort yesterday's inbound issues and label them.
agent: Nova
outputs: issue
issue_title_template: "Triage {{date}}"
triggers:
  - kind: schedule
    cron: "0 9 * * *"
    timezone: Europe/Paris
    label: Morning
  - kind: webhook
    label: Push
budget:
  runs_per_day: 3
  max_minutes: 20
---

# Nightly triage

Read the queue, then label.
`

func TestParseDaemonMarkdown(t *testing.T) {
	doc, errs := ParseDaemonMarkdown(validDaemon)
	if len(errs) != 0 {
		t.Fatalf("valid document reported errors: %v", errs)
	}
	if doc.Name != "Nightly triage" {
		t.Errorf("name = %q, want %q", doc.Name, "Nightly triage")
	}
	if doc.Role != "Sort yesterday's inbound issues and label them." {
		t.Errorf("role = %q", doc.Role)
	}
	if doc.Agent != "Nova" {
		t.Errorf("agent = %q, want Nova", doc.Agent)
	}
	if doc.Outputs != DaemonOutputIssue {
		t.Errorf("outputs = %q, want %q", doc.Outputs, DaemonOutputIssue)
	}
	if doc.IssueTitleTemplate != "Triage {{date}}" {
		t.Errorf("issue_title_template = %q", doc.IssueTitleTemplate)
	}
	if len(doc.Triggers) != 2 {
		t.Fatalf("triggers = %d, want 2", len(doc.Triggers))
	}
	if doc.Triggers[0].Kind != "schedule" || doc.Triggers[0].Cron != "0 9 * * *" ||
		doc.Triggers[0].Timezone != "Europe/Paris" || doc.Triggers[0].Label != "Morning" {
		t.Errorf("schedule trigger = %+v", doc.Triggers[0])
	}
	if doc.Triggers[1].Kind != "webhook" || doc.Triggers[1].Label != "Push" {
		t.Errorf("webhook trigger = %+v", doc.Triggers[1])
	}
	if doc.Budget == nil || doc.Budget.RunsPerDay == nil || *doc.Budget.RunsPerDay != 3 {
		t.Errorf("budget.runs_per_day = %+v, want 3", doc.Budget)
	}
	if doc.Budget.MaxMinutes == nil || *doc.Budget.MaxMinutes != 20 {
		t.Errorf("budget.max_minutes = %+v, want 20", doc.Budget)
	}
	// The body is the skill content: everything below the closing ---, with
	// the blank separator line eaten but the markdown itself untouched.
	if want := "# Nightly triage\n\nRead the queue, then label.\n"; doc.Body != want {
		t.Errorf("body = %q, want %q", doc.Body, want)
	}
	// Line numbers address the DOCUMENT, not the frontmatter block: line 1 is
	// the opening ---, so `name` is on line 2.
	if doc.Lines["name"] != 2 {
		t.Errorf("name declared on line %d, want 2", doc.Lines["name"])
	}
}

// TestParseDaemonMarkdownRejections is the boundary matrix: this is the file
// that decides what a declaration may say, and every rejection here is a 400
// with no write behind it.
func TestParseDaemonMarkdownRejections(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		wantLine int
		wantMsg  string
	}{
		{
			name:     "unknown top-level key",
			content:  "---\nname: a\nrole: b\nagent: c\ntriggerss: []\n---\nbody",
			wantLine: 5,
			wantMsg:  `unknown key "triggerss"`,
		},
		{
			name:     "unknown trigger key",
			content:  "---\nname: a\nrole: b\nagent: c\ntriggers:\n  - kind: schedule\n    cron: \"* * * * *\"\n    every: 5m\n---\nbody",
			wantLine: 8,
			wantMsg:  `unknown trigger key "every"`,
		},
		{
			name:     "unknown budget key",
			content:  "---\nname: a\nrole: b\nagent: c\nbudget:\n  tokens: 10\n---\nbody",
			wantLine: 6,
			wantMsg:  `unknown budget key "tokens"`,
		},
		{
			name:     "invalid trigger kind",
			content:  "---\nname: a\nrole: b\nagent: c\ntriggers:\n  - kind: cron\n---\nbody",
			wantLine: 6,
			wantMsg:  `invalid trigger kind "cron"`,
		},
		{
			name:     "retired api trigger kind",
			content:  "---\nname: a\nrole: b\nagent: c\ntriggers:\n  - kind: api\n---\nbody",
			wantLine: 6,
			wantMsg:  `trigger kind "api" is retired`,
		},
		{
			name:     "schedule without cron",
			content:  "---\nname: a\nrole: b\nagent: c\ntriggers:\n  - kind: schedule\n---\nbody",
			wantLine: 6,
			wantMsg:  "needs a cron expression",
		},
		{
			name:     "webhook with cron",
			content:  "---\nname: a\nrole: b\nagent: c\ntriggers:\n  - kind: webhook\n    cron: \"* * * * *\"\n---\nbody",
			wantLine: 6,
			wantMsg:  "cron is not valid for a webhook trigger",
		},
		{
			name:     "invalid outputs",
			content:  "---\nname: a\nrole: b\nagent: c\noutputs: pdf\n---\nbody",
			wantLine: 5,
			wantMsg:  `outputs must be "issue" or "run_only"`,
		},
		{
			name:     "missing name",
			content:  "---\nrole: b\nagent: c\n---\nbody",
			wantLine: 1,
			wantMsg:  "name is required",
		},
		{
			name:     "empty role",
			content:  "---\nname: a\nrole: \"\"\nagent: c\n---\nbody",
			wantLine: 3,
			wantMsg:  "role must not be empty",
		},
		{
			name:     "budget must be positive",
			content:  "---\nname: a\nrole: b\nagent: c\nbudget:\n  runs_per_day: 0\n---\nbody",
			wantLine: 6,
			wantMsg:  "runs_per_day must be a positive whole number",
		},
		{
			name:     "no frontmatter",
			content:  "# just a document\n",
			wantLine: 1,
			wantMsg:  "must start with a YAML frontmatter block",
		},
		{
			name:     "unclosed frontmatter",
			content:  "---\nname: a\nrole: b\n",
			wantLine: 1,
			wantMsg:  "not closed by a --- line",
		},
		{
			name:     "structured value where a string belongs",
			content:  "---\nname:\n  - a\n  - b\nrole: b\nagent: c\n---\nbody",
			wantLine: 2,
			wantMsg:  "name must be a single value",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, errs := ParseDaemonMarkdown(tt.content)
			if len(errs) == 0 {
				t.Fatalf("document was accepted; wanted %q", tt.wantMsg)
			}
			for _, e := range errs {
				if strings.Contains(e.Message, tt.wantMsg) {
					if e.Line != tt.wantLine {
						t.Errorf("error %q reported on line %d, want %d", e.Message, e.Line, tt.wantLine)
					}
					return
				}
			}
			t.Errorf("no error matched %q; got %v", tt.wantMsg, errs)
		})
	}
}

// TestParseDaemonMarkdownReportsEveryProblem: the import dialog marks up the
// whole document in one pass, so a document with three mistakes must not
// report only the first.
func TestParseDaemonMarkdownReportsEveryProblem(t *testing.T) {
	content := "---\nname: a\nrole: b\nagent: c\nnope: 1\nalso: 2\noutputs: pdf\n---\nbody"
	_, errs := ParseDaemonMarkdown(content)
	if len(errs) != 3 {
		t.Fatalf("got %d errors, want 3: %v", len(errs), errs)
	}
	for i := 1; i < len(errs); i++ {
		if errs[i-1].Line > errs[i].Line {
			t.Errorf("errors are not ordered by line: %v", errs)
		}
	}
}

// TestRenderDaemonMarkdownRoundTrips is the export contract: the rendered
// declaration must parse back to the same configuration, or "export then
// re-import" silently changes the daemon.
func TestRenderDaemonMarkdownRoundTrips(t *testing.T) {
	original, errs := ParseDaemonMarkdown(validDaemon)
	if len(errs) != 0 {
		t.Fatalf("fixture is invalid: %v", errs)
	}

	rendered, err := RenderDaemonMarkdown(original)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	round, errs := ParseDaemonMarkdown(rendered)
	if len(errs) != 0 {
		t.Fatalf("rendered document does not re-parse: %v\n---\n%s", errs, rendered)
	}

	if round.Name != original.Name || round.Role != original.Role || round.Agent != original.Agent ||
		round.Outputs != original.Outputs || round.IssueTitleTemplate != original.IssueTitleTemplate {
		t.Errorf("scalar fields drifted:\n got %+v\nwant %+v", round, original)
	}
	if len(round.Triggers) != len(original.Triggers) {
		t.Fatalf("triggers = %d, want %d", len(round.Triggers), len(original.Triggers))
	}
	for i := range original.Triggers {
		a, b := original.Triggers[i], round.Triggers[i]
		if a.Kind != b.Kind || a.Cron != b.Cron || a.Timezone != b.Timezone || a.Label != b.Label {
			t.Errorf("trigger %d drifted: got %+v, want %+v", i, b, a)
		}
	}
	if round.Budget == nil || *round.Budget.RunsPerDay != *original.Budget.RunsPerDay ||
		*round.Budget.MaxMinutes != *original.Budget.MaxMinutes {
		t.Errorf("budget drifted: got %+v", round.Budget)
	}
	if strings.TrimSpace(round.Body) != strings.TrimSpace(original.Body) {
		t.Errorf("body drifted:\n got %q\nwant %q", round.Body, original.Body)
	}
}

// TestRenderDaemonMarkdownEscapesAwkwardValues: a title starting with `[`, a
// role containing a colon and a cron full of asterisks are all YAML traps. The
// renderer goes through the encoder so they survive; concatenation would emit
// a document that no longer parses.
func TestRenderDaemonMarkdownEscapesAwkwardValues(t *testing.T) {
	doc := DaemonDoc{
		Name:    "[bot] nightly: sweep",
		Role:    "Do this: then that #now",
		Agent:   "Nova",
		Outputs: DaemonOutputRunOnly,
		Triggers: []DaemonTrigger{
			{Kind: "schedule", Cron: "*/5 * * * *", Timezone: "UTC"},
		},
		Body: "body\n",
	}
	rendered, err := RenderDaemonMarkdown(doc)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	round, errs := ParseDaemonMarkdown(rendered)
	if len(errs) != 0 {
		t.Fatalf("rendered document does not re-parse: %v\n---\n%s", errs, rendered)
	}
	if round.Name != doc.Name || round.Role != doc.Role || round.Outputs != doc.Outputs {
		t.Errorf("drifted: got %+v, want %+v", round, doc)
	}
	if len(round.Triggers) != 1 || round.Triggers[0].Cron != "*/5 * * * *" {
		t.Errorf("cron drifted: %+v", round.Triggers)
	}
}
