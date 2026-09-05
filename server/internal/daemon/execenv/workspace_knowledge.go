package execenv

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Workspace Brain injection: the workspace's shared knowledge notes are
// written into the run's workdir as plain markdown files, and the brief points
// at them. Files rather than brief text because a Brain can hold far more than
// a prompt should: the index is always cheap to read, and the run pays for a
// note's body only when it opens it.

const (
	// KnowledgeDirRelPath is the workdir-relative directory the notes land in.
	KnowledgeDirRelPath = ".multica/knowledge"
	// knowledgeByteBudget caps the total bytes written across every note file
	// (the index is not counted; it is bounded by the note count). Notes are
	// written in server order — pinned first, then most recently updated — so
	// what the budget drops is always the least recently touched.
	knowledgeByteBudget = 200 * 1024
)

var knowledgeSlugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// knowledgeNoteFileName derives a stable, collision-free file name for one
// note: a slug of the title plus the first 8 characters of the id. Two notes
// with the same title still get distinct files, and the id stays visible so an
// agent can pass it back to `multica brain show`.
func knowledgeNoteFileName(note WorkspaceNoteForEnv) string {
	slug := knowledgeSlugUnsafe.ReplaceAllString(strings.ToLower(note.Title), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 60 {
		slug = strings.Trim(slug[:60], "-")
	}
	id := strings.ReplaceAll(note.ID, "-", "")
	if len(id) > 8 {
		id = id[:8]
	}
	if slug == "" {
		slug = "note"
	}
	if id == "" {
		return slug + ".md"
	}
	return slug + "-" + id + ".md"
}

// knowledgeNoteBody is the markdown one note is written as: an H1 title, the
// metadata an agent needs to act on it (id, tags, source), then the body.
func knowledgeNoteBody(note WorkspaceNoteForEnv) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", note.Title)
	fmt.Fprintf(&b, "- id: `%s`\n", note.ID)
	if len(note.Tags) > 0 {
		fmt.Fprintf(&b, "- tags: %s\n", strings.Join(note.Tags, ", "))
	}
	if note.Source != "" {
		fmt.Fprintf(&b, "- source: %s\n", note.Source)
	}
	if note.Pinned {
		b.WriteString("- pinned: true\n")
	}
	if note.Updated != "" {
		fmt.Fprintf(&b, "- updated: %s\n", note.Updated)
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimRight(note.Content, "\n"))
	b.WriteString("\n")
	return b.String()
}

// selectKnowledgeNotes applies the byte budget. Notes arrive in the order the
// run should see them (pinned first, then newest); the first note is always
// kept even if it alone exceeds the budget, so a workspace whose only note is
// huge still gets it rather than an empty knowledge directory.
func selectKnowledgeNotes(notes []WorkspaceNoteForEnv) ([]WorkspaceNoteForEnv, int) {
	kept := make([]WorkspaceNoteForEnv, 0, len(notes))
	used := 0
	for _, note := range notes {
		size := len(knowledgeNoteBody(note))
		if len(kept) > 0 && used+size > knowledgeByteBudget {
			continue
		}
		kept = append(kept, note)
		used += size
	}
	return kept, len(notes) - len(kept)
}

// knowledgeIndex is .multica/knowledge/README.md: every injected note's title,
// tags, id and file name, so the run can pick what to open without reading
// every body.
func knowledgeIndex(notes []WorkspaceNoteForEnv, omitted int) string {
	var b strings.Builder
	b.WriteString("# Workspace knowledge\n\n")
	b.WriteString("Durable notes this workspace shares: decisions, conventions, facts about the codebase, contacts.\n")
	b.WriteString("Open a file below to read one. Save a new one with `multica brain save`.\n\n")
	for _, note := range notes {
		fmt.Fprintf(&b, "- **%s** — `%s`", note.Title, knowledgeNoteFileName(note))
		if len(note.Tags) > 0 {
			fmt.Fprintf(&b, " · tags: %s", strings.Join(note.Tags, ", "))
		}
		if note.Pinned {
			b.WriteString(" · pinned")
		}
		fmt.Fprintf(&b, " · id: `%s`\n", note.ID)
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "\n%d older note(s) were left out of this run to stay within the knowledge size budget; find them with `multica brain list`.\n", omitted)
	}
	return b.String()
}

// writeWorkspaceKnowledge materializes the Brain under
// {workDir}/.multica/knowledge: one README.md index plus one file per note.
//
// manifest, when non-nil, records what was created so CleanupSidecars can roll
// a local_directory workdir back. A pre-existing path is a collision the
// manifest must not destroy — the run then simply sees fewer files, which the
// brief's Workspace Knowledge section survives (it points at the directory,
// not at a fixed list).
func writeWorkspaceKnowledge(workDir string, ctx TaskContextForEnv, manifest *sidecarManifest) error {
	if len(ctx.WorkspaceNotes) == 0 {
		return nil
	}
	dir := filepath.Join(workDir, filepath.FromSlash(KnowledgeDirRelPath))
	if err := recordMkdirAll(dir, 0o755, manifest); err != nil {
		return fmt.Errorf("create knowledge dir: %w", err)
	}

	notes, omitted := selectKnowledgeNotes(ctx.WorkspaceNotes)
	if err := recordWriteFile(filepath.Join(dir, "README.md"), []byte(knowledgeIndex(notes, omitted)), 0o644, manifest); err != nil {
		if !errors.Is(err, errPathPreExists) {
			return err
		}
	}
	for _, note := range notes {
		path := filepath.Join(dir, knowledgeNoteFileName(note))
		if err := recordWriteFile(path, []byte(knowledgeNoteBody(note)), 0o644, manifest); err != nil {
			if errors.Is(err, errPathPreExists) {
				continue
			}
			return err
		}
	}
	return nil
}
