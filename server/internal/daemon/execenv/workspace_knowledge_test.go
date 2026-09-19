package execenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/brainknowledge"
)

func note(id, title, body string, pinned bool) WorkspaceNoteForEnv {
	return WorkspaceNoteForEnv{
		ID: id, Title: title, Content: body, Pinned: pinned,
		Tags: []string{"deploy"}, Source: "manual", Updated: "2026-01-01T00:00:00Z",
	}
}

func TestWriteWorkspaceKnowledgeWritesIndexAndOneFilePerNote(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()

	notes := []WorkspaceNoteForEnv{
		note("11111111-2222-3333-4444-555555555555", "Deploys go through the release tag", "Push v0.x.x on main.", true),
		note("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", "Postgres connection pooling", "pgbouncer sits in front.", false),
	}
	if err := writeWorkspaceKnowledge(workDir, TaskContextForEnv{WorkspaceNotes: notes}, nil); err != nil {
		t.Fatalf("writeWorkspaceKnowledge: %v", err)
	}

	dir := filepath.Join(workDir, ".multica", "knowledge")
	index, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	for _, want := range []string{
		"# Workspace knowledge",
		"Deploys go through the release tag",
		"deploys-go-through-the-release-tag-11111111.md",
		"postgres-connection-pooling-aaaaaaaa.md",
		"multica brain save",
	} {
		if !strings.Contains(string(index), want) {
			t.Errorf("index missing %q\n---\n%s", want, index)
		}
	}

	body, err := os.ReadFile(filepath.Join(dir, "deploys-go-through-the-release-tag-11111111.md"))
	if err != nil {
		t.Fatalf("read note file: %v", err)
	}
	for _, want := range []string{
		"# Deploys go through the release tag",
		"- id: `11111111-2222-3333-4444-555555555555`",
		"- tags: deploy",
		"- pinned: true",
		"Push v0.x.x on main.",
	} {
		if !strings.Contains(string(body), want) {
			t.Errorf("note file missing %q\n---\n%s", want, body)
		}
	}
}

// The byte budget is what keeps a large Brain from filling the workdir. Notes
// arrive pinned-first / newest-first, so what it drops is always the least
// recently touched — and the index says so instead of silently shrinking.
func TestWriteWorkspaceKnowledgeRespectsTheByteBudget(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()

	big := strings.Repeat("x", 60*1024)
	var notes []WorkspaceNoteForEnv
	for i := 0; i < 6; i++ {
		notes = append(notes, note(strings.Repeat(string(rune('a'+i)), 8)+"-2222-3333-4444-555555555555", "Note "+string(rune('A'+i)), big, false))
	}
	if err := writeWorkspaceKnowledge(workDir, TaskContextForEnv{WorkspaceNotes: notes}, nil); err != nil {
		t.Fatalf("writeWorkspaceKnowledge: %v", err)
	}

	dir := filepath.Join(workDir, ".multica", "knowledge")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, e := range entries {
		if e.Name() == "README.md" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			t.Fatal(err)
		}
		total += int(info.Size())
	}
	if total > brainknowledge.ByteBudget {
		t.Fatalf("wrote %d bytes of notes, over the %d budget", total, brainknowledge.ByteBudget)
	}
	// 6 × 60 KiB cannot fit in 200 KiB, so the tail must have been dropped.
	if len(entries) >= len(notes)+1 {
		t.Fatalf("wrote %d entries; the budget dropped nothing", len(entries))
	}
	index, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "left out of this run") {
		t.Errorf("index does not disclose the dropped notes\n---\n%s", index)
	}
}

// A single oversized note is still written: an empty knowledge directory is
// worse than one large file, and the run can always stop reading.
func TestWriteWorkspaceKnowledgeKeepsAnOversizedFirstNote(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()

	notes := []WorkspaceNoteForEnv{note("11111111-2222-3333-4444-555555555555", "Huge", strings.Repeat("y", brainknowledge.ByteBudget+1024), false)}
	if err := writeWorkspaceKnowledge(workDir, TaskContextForEnv{WorkspaceNotes: notes}, nil); err != nil {
		t.Fatalf("writeWorkspaceKnowledge: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".multica", "knowledge", "huge-11111111.md")); err != nil {
		t.Fatalf("oversized single note was dropped: %v", err)
	}
}

func TestWriteWorkspaceKnowledgeIsANoOpWithoutNotes(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()
	if err := writeWorkspaceKnowledge(workDir, TaskContextForEnv{}, nil); err != nil {
		t.Fatalf("writeWorkspaceKnowledge: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workDir, ".multica", "knowledge")); !os.IsNotExist(err) {
		t.Fatalf("empty Brain still created the knowledge directory (err=%v)", err)
	}
}

func TestWorkspaceKnowledgeBriefSection(t *testing.T) {
	t.Parallel()

	out := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:        "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentName:      "Brain agent",
		WorkspaceNotes: []WorkspaceNoteForEnv{note("11111111-2222-3333-4444-555555555555", "A note", "body", false)},
	})
	for _, want := range []string{
		"## Workspace Knowledge\n",
		".multica/knowledge",
		"README.md",
		"multica brain save",
		"multica brain list --search",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("brief missing %q\n---\n%s", want, out)
		}
	}

	empty := buildMetaSkillContent("claude", TaskContextForEnv{
		IssueID:   "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		AgentName: "Brain agent",
	})
	if strings.Contains(empty, "## Workspace Knowledge") {
		t.Errorf("a workspace with an empty Brain must keep a byte-identical brief\n---\n%s", empty)
	}
}

// A current server applies the budget itself and sends only the kept notes
// plus how many it dropped. The daemon writes exactly what it received and
// the README still discloses the server's count.
func TestWriteWorkspaceKnowledgeDisclosesTheServerOmittedCount(t *testing.T) {
	t.Parallel()
	workDir := t.TempDir()

	notes := []WorkspaceNoteForEnv{note("11111111-2222-3333-4444-555555555555", "Kept", "body", false)}
	if err := writeWorkspaceKnowledge(workDir, TaskContextForEnv{WorkspaceNotes: notes, WorkspaceNotesOmitted: 3}, nil); err != nil {
		t.Fatalf("writeWorkspaceKnowledge: %v", err)
	}
	dir := filepath.Join(workDir, ".multica", "knowledge")
	index, err := os.ReadFile(filepath.Join(dir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(index), "3 older note(s) were left out") {
		t.Errorf("index does not carry the server's omitted count\n---\n%s", index)
	}
	if _, err := os.Stat(filepath.Join(dir, "kept-11111111.md")); err != nil {
		t.Fatalf("kept note not written: %v", err)
	}
}

// The index says why each note is there (JEF-414), so a run can tell the note
// that answers its task from the one that is merely recent — and an older
// server, which sends no reason at all, still gets the index it always got.
func TestKnowledgeIndexRendersWhyEachNoteIsThere(t *testing.T) {
	t.Parallel()

	pinned := note("11111111-2222-3333-4444-555555555555", "Astreinte", "call ops", true)
	pinned.Reason = brainknowledge.ReasonPinned
	relevant := note("22222222-2222-3333-4444-555555555555", "Deploy procedure", "signed tag", false)
	relevant.Reason, relevant.Score = brainknowledge.ReasonRelevant, 0.8312
	recent := note("33333333-2222-3333-4444-555555555555", "Weekly notes", "nothing", false)
	recent.Reason = brainknowledge.ReasonRecent

	index := knowledgeIndex([]WorkspaceNoteForEnv{pinned, relevant, recent}, 0, "deploy fails from a branch")
	for _, want := range []string{
		`· relevant to "deploy fails from a branch" (score 0.83)`,
		"· recent",
		"· pinned",
	} {
		if !strings.Contains(index, want) {
			t.Errorf("index does not carry %q\n---\n%s", want, index)
		}
	}
	// ReasonPinned adds nothing of its own: the pinned marker already said it.
	if strings.Count(index, "pinned") != 1 {
		t.Errorf("the pinned note is labelled twice\n---\n%s", index)
	}

	// No reasons and no query: byte-identical to the index that predates this.
	bare := []WorkspaceNoteForEnv{
		note("11111111-2222-3333-4444-555555555555", "Astreinte", "call ops", true),
		note("22222222-2222-3333-4444-555555555555", "Deploy procedure", "signed tag", false),
	}
	withReason := knowledgeIndex(bare, 0, "")
	if strings.Contains(withReason, "relevant") || strings.Contains(withReason, "recent") {
		t.Errorf("an index built from reasonless notes gained a reason\n---\n%s", withReason)
	}
}

// A relevant note without a query still says so, rather than printing an
// empty pair of quotes: an older daemon field, or a claim whose query was
// dropped, must not corrupt the line.
func TestKnowledgeIndexRelevantWithoutQuery(t *testing.T) {
	t.Parallel()
	relevant := note("22222222-2222-3333-4444-555555555555", "Deploy procedure", "signed tag", false)
	relevant.Reason = brainknowledge.ReasonRelevant
	index := knowledgeIndex([]WorkspaceNoteForEnv{relevant}, 0, "")
	if !strings.Contains(index, "· relevant ·") {
		t.Errorf("index line = %q, want a bare relevant marker", index)
	}
	if strings.Contains(index, `""`) {
		t.Errorf("index printed empty quotes\n---\n%s", index)
	}
}
