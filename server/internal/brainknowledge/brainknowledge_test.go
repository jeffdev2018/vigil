package brainknowledge

import (
	"strings"
	"testing"
)

func note(id, title, body string, pinned bool) Note {
	return Note{ID: id, Title: title, Content: body, Pinned: pinned, Tags: []string{"deploy"}, Source: "manual", Updated: "2026-01-01T00:00:00Z"}
}

func TestFileNameAndIDPrefix(t *testing.T) {
	for _, tc := range []struct {
		note Note
		want string
	}{
		{note("0192abcd-2222-3333-4444-555555555555", "Deploy procedure", "", false), "deploy-procedure-0192abcd.md"},
		{note("0192abcd-2222-3333-4444-555555555555", "!!!", "", false), "note-0192abcd.md"},
		{note("", "Untitled id", "", false), "untitled-id.md"},
		{note("0192abcd", strings.Repeat("a", 80), "", false), strings.Repeat("a", 60) + "-0192abcd.md"},
	} {
		if got := FileName(tc.note); got != tc.want {
			t.Errorf("FileName(%q) = %q, want %q", tc.note.Title, got, tc.want)
		}
	}
	if got := IDPrefix("0192abcd-2222-3333-4444-555555555555"); got != "0192abcd" {
		t.Errorf("IDPrefix = %q", got)
	}
}

func TestBodyRendersMetadataThenContent(t *testing.T) {
	got := Body(note("11111111-2222-3333-4444-555555555555", "Title", "body\n\n", true))
	want := "# Title\n\n- id: `11111111-2222-3333-4444-555555555555`\n- tags: deploy\n- source: manual\n- pinned: true\n- updated: 2026-01-01T00:00:00Z\n\nbody\n"
	if got != want {
		t.Fatalf("Body =\n%q\nwant\n%q", got, want)
	}
}

// Regression test for the budget bug: once the running total would exceed
// the budget, selection must stop (prefix truncation), not skip the
// over-budget note and keep scanning for a smaller one further down the
// list. [pinned 150KiB, recent 60KiB, older 30KiB] against a 200KiB budget:
// pinned+recent already exceeds it, so recent AND older must both be
// dropped -- the buggy `continue` kept [pinned, older], silently admitting
// the older, lower-priority note while dropping the more recent one, which
// also contradicted the very README message it produced.
func TestSelectIsPrefixTruncation(t *testing.T) {
	pinned := note("11111111-1111-1111-1111-111111111111", "Pinned", strings.Repeat("p", 150*1024), true)
	recent := note("22222222-2222-2222-2222-222222222222", "Recent", strings.Repeat("r", 60*1024), false)
	older := note("33333333-3333-3333-3333-333333333333", "Older", strings.Repeat("o", 30*1024), false)

	pinnedSize, recentSize, olderSize := len(Body(pinned)), len(Body(recent)), len(Body(older))
	if pinnedSize+recentSize <= ByteBudget {
		t.Fatalf("test fixture assumption broken: pinned+recent (%d) must exceed the budget (%d)", pinnedSize+recentSize, ByteBudget)
	}
	if pinnedSize+olderSize > ByteBudget {
		t.Fatalf("test fixture assumption broken: pinned+older (%d) must fit the budget (%d) -- otherwise this doesn't distinguish break from continue", pinnedSize+olderSize, ByteBudget)
	}

	kept, omitted := Select([]Note{pinned, recent, older})
	if len(kept) != 1 || kept[0].ID != pinned.ID {
		t.Fatalf("kept %d notes, want only [Pinned] -- recent and older must both be dropped", len(kept))
	}
	if omitted != 2 {
		t.Fatalf("omitted = %d, want 2", omitted)
	}
}

// Selecting an already-selected list keeps everything: the daemon re-applies
// Select to what a current server sent, and must not drop anything twice.
func TestSelectIsIdempotentAndKeepsAnOversizedFirstNote(t *testing.T) {
	huge := note("11111111-1111-1111-1111-111111111111", "Huge", strings.Repeat("h", ByteBudget+1), false)
	small := note("22222222-2222-2222-2222-222222222222", "Small", "s", false)
	kept, omitted := Select([]Note{huge, small})
	if len(kept) != 1 || omitted != 1 {
		t.Fatalf("kept %d omitted %d, want the oversized first note alone and 1 omitted", len(kept), omitted)
	}
	again, omittedAgain := Select(kept)
	if len(again) != len(kept) || omittedAgain != 0 {
		t.Fatalf("re-selecting dropped notes: kept %d omitted %d", len(again), omittedAgain)
	}
	if kept, omitted := Select(nil); len(kept) != 0 || omitted != 0 {
		t.Fatalf("empty Brain: kept %d omitted %d", len(kept), omitted)
	}
}
