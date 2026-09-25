package execenv

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/multica-ai/multica/server/internal/brainknowledge"
)

// Workspace Brain injection: the workspace's shared knowledge notes are
// written into the run's workdir as plain markdown files, and the brief points
// at them. Files rather than brief text because a Brain can hold far more than
// a prompt should: the index is always cheap to read, and the run pays for a
// note's body only when it opens it.

// KnowledgeDirRelPath is the workdir-relative directory the notes land in.
// The byte budget, file names and note bodies come from brainknowledge, which
// the claim handler applies too.
const KnowledgeDirRelPath = ".multica/knowledge"

// knowledgeIndex is .multica/knowledge/README.md: every injected note's title,
// tags, id and file name, so the run can pick what to open without reading
// every body.
func knowledgeIndex(notes []WorkspaceNoteForEnv, omitted int, query string) string {
	var b strings.Builder
	b.WriteString("# Workspace knowledge\n\n")
	b.WriteString("Durable notes this workspace shares: decisions, conventions, facts about the codebase, contacts.\n")
	b.WriteString("Open a file below to read one. Save a new one with `multica brain save`.\n\n")
	for _, note := range notes {
		fmt.Fprintf(&b, "- **%s** — `%s`", note.Title, brainknowledge.FileName(note))
		if len(note.Tags) > 0 {
			fmt.Fprintf(&b, " · tags: %s", strings.Join(note.Tags, ", "))
		}
		if note.Pinned {
			b.WriteString(" · pinned")
		}
		b.WriteString(noteReasonSuffix(note, query))
		fmt.Fprintf(&b, " · id: `%s`\n", note.ID)
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "\n%d older note(s) were left out of this run to stay within the knowledge size budget; find them with `multica brain list`.\n", omitted)
	}
	return b.String()
}

// noteReasonSuffix says why this note is in the run's selection: relevant to
// what the run is doing (with the score that ranked it), or merely recent.
// ReasonPinned adds nothing — the pinned marker above already said it — and an
// empty Reason, which is what an older server sends, adds nothing either, so
// the index stays byte-identical to the one that predates this.
func noteReasonSuffix(note WorkspaceNoteForEnv, query string) string {
	switch note.Reason {
	case brainknowledge.ReasonRelevant:
		var b strings.Builder
		if query != "" {
			fmt.Fprintf(&b, " · relevant to %q", query)
		} else {
			b.WriteString(" · relevant")
		}
		if note.Score != 0 {
			fmt.Fprintf(&b, " (score %.2f)", note.Score)
		}
		return b.String()
	case brainknowledge.ReasonRecent:
		return " · recent"
	default:
		return ""
	}
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

	// A current server sends only what the budget keeps, so this selection
	// keeps everything and its own count adds nothing; an older server sends
	// the whole list and this is the only selection there is.
	notes, omitted := brainknowledge.Select(ctx.WorkspaceNotes)
	omitted += ctx.WorkspaceNotesOmitted
	if err := recordWriteFile(filepath.Join(dir, "README.md"), []byte(knowledgeIndex(notes, omitted, ctx.WorkspaceNotesQuery)), 0o644, manifest); err != nil {
		if !errors.Is(err, errPathPreExists) {
			return err
		}
	}
	for _, note := range notes {
		path := filepath.Join(dir, brainknowledge.FileName(note))
		if err := recordWriteFile(path, []byte(brainknowledge.Body(note)), 0o644, manifest); err != nil {
			if errors.Is(err, errPathPreExists) {
				continue
			}
			return err
		}
	}
	return nil
}
