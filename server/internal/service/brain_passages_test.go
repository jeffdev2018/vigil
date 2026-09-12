package service

// Pure unit tests: no database.

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func headings(ps []NotePassage) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.Heading)
	}
	return out
}

func TestSplitNotePassagesSections(t *testing.T) {
	content := strings.Join([]string{
		"intro line",
		"# Setup",
		"```bash",
		"# not a heading",
		"```",
		"## Deploy",
		"~~~~",
		"## still code",
		"~~~",
		"~~~~",
		"step one",
		"### Rollback ###",
		"revert the tag",
		"## Empty",
		"# Contacts",
		"call ops",
	}, "\n")
	ps := SplitNotePassages("Runbook", content)
	want := []string{"", "Setup", "Setup › Deploy", "Setup › Deploy › Rollback", "Contacts"}
	if got := headings(ps); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("headings = %q, want %q", got, want)
	}
	if ps[1].Body != "```bash\n# not a heading\n```" {
		t.Errorf("code block body = %q", ps[1].Body)
	}
	if !strings.Contains(ps[2].Body, "## still code") || !strings.HasSuffix(ps[2].Body, "step one") {
		t.Errorf("a longer fence closes only on its own length: %q", ps[2].Body)
	}
	for i, p := range ps {
		if p.Ordinal != i+1 {
			t.Errorf("passage %d has ordinal %d", i, p.Ordinal)
		}
	}
}

func TestSplitNotePassagesTitleOnlyNote(t *testing.T) {
	for _, content := range []string{"", "  \n", "# Only\n## Headings"} {
		ps := SplitNotePassages("Title", content)
		if len(ps) != 1 || ps[0].Ordinal != 1 || ps[0].Body != "" {
			t.Errorf("SplitNotePassages(%q) = %+v, want one empty passage", content, ps)
		}
	}
}

func TestSplitNotePassagesLongSection(t *testing.T) {
	var b strings.Builder
	for i := 0; b.Len() < 4000; i++ {
		fmt.Fprintf(&b, "Sentence number %d talks about deploys and rollbacks. ", i)
	}
	ps := SplitNotePassages("", b.String())
	if len(ps) < 3 {
		t.Fatalf("got %d passages for a 4000-rune paragraph", len(ps))
	}
	for i, p := range ps {
		if n := utf8.RuneCountInString(p.Body); n > brainPassageMaxRunes {
			t.Errorf("passage %d is %d runes", i, n)
		}
		if i == 0 {
			continue
		}
		prev := []rune(ps[i-1].Body)
		tail := string(prev[max(0, len(prev)-brainPassageOverlapRunes):])
		head := []rune(p.Body)
		if !strings.Contains(tail, string(head[:40])) {
			t.Errorf("passage %d does not start inside the tail of %d: %q / %q", i, i-1, string(head[:40]), tail)
		}
		if i < len(ps)-1 && !strings.HasSuffix(p.Body, ".") {
			t.Errorf("passage %d is not cut at a sentence end: …%q", i, string(head[len(head)-20:]))
		}
	}
}

func TestSplitNotePassagesPrefersParagraphs(t *testing.T) {
	para := strings.TrimSpace(strings.Repeat("alpha beta gamma. ", 40)) // ~720 runes
	ps := SplitNotePassages("", para+"\n\n"+strings.Replace(para, "alpha", "delta", 1))
	if len(ps) != 2 || ps[0].Body != para {
		t.Fatalf("passages = %d, first = %q; want the first paragraph whole", len(ps), ps[0].Body)
	}
}

func TestSplitNotePassagesHardCutsCJKAndCaps(t *testing.T) {
	cjk := strings.Repeat("生产环境部署流程", 400) // 3200 runes, no punctuation
	for i, p := range SplitNotePassages("", cjk) {
		if n := utf8.RuneCountInString(p.Body); n > brainPassageMaxRunes || n == 0 {
			t.Errorf("CJK passage %d is %d runes", i, n)
		}
	}

	var b strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "## Section %d\ntext %d\n", i, i)
	}
	ps := SplitNotePassages("", b.String())
	if len(ps) != brainPassageMaxCount || ps[len(ps)-1].Ordinal != brainPassageMaxCount {
		t.Fatalf("got %d passages, want the cap %d", len(ps), brainPassageMaxCount)
	}
}
