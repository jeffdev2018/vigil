// Package brainknowledge holds the pure rules that decide which workspace
// Brain notes a daemon run receives and how each one is written to disk.
//
// The claim handler and the daemon both apply them: the server to send (and
// record) exactly the notes the budget keeps, the daemon to write them. One
// copy keeps "what the server says was injected" and "what landed in
// .multica/knowledge" from drifting apart.
package brainknowledge

import (
	"fmt"
	"regexp"
	"strings"
)

// ByteBudget caps the total bytes written across every note file (the index
// is not counted; it is bounded by the note count). Notes arrive in server
// order — pinned first, then most recently updated — so what the budget drops
// is always the least recently touched.
const ByteBudget = 200 * 1024

// Note is one Brain note as a run receives it. It is the claim wire shape too,
// so the json tags are the contract between server and daemon.
type Note struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Content string   `json:"content,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Pinned  bool     `json:"pinned,omitempty"`
	Source  string   `json:"source,omitempty"`
	Updated string   `json:"updated_at,omitempty"`
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// IDPrefix is the part of a note id that appears in its file name: the first
// 8 hex characters of the id without dashes.
func IDPrefix(id string) string {
	id = strings.ReplaceAll(id, "-", "")
	if len(id) > 8 {
		id = id[:8]
	}
	return id
}

// FileName derives a stable, collision-free file name for one note: a slug of
// the title plus IDPrefix. Two notes with the same title still get distinct
// files, and the id stays visible so an agent can pass it back to
// `multica brain show`.
func FileName(note Note) string {
	slug := slugUnsafe.ReplaceAllString(strings.ToLower(note.Title), "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 60 {
		slug = strings.Trim(slug[:60], "-")
	}
	id := IDPrefix(note.ID)
	if slug == "" {
		slug = "note"
	}
	if id == "" {
		return slug + ".md"
	}
	return slug + "-" + id + ".md"
}

// Body is the markdown one note is written as: an H1 title, the metadata an
// agent needs to act on it (id, tags, source), then the body.
func Body(note Note) string {
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

// Select applies the byte budget and returns the kept prefix plus how many
// notes were dropped. Notes arrive in the order the run should see them
// (pinned first, then newest); the first note is always kept even if it alone
// exceeds the budget, so a workspace whose only note is huge still gets it
// rather than an empty knowledge directory.
func Select(notes []Note) ([]Note, int) {
	kept := make([]Note, 0, len(notes))
	used := 0
	for _, note := range notes {
		size := len(Body(note))
		if len(kept) > 0 && used+size > ByteBudget {
			// Prefix truncation, matching the README's "older note(s) were
			// left out" message: once the budget is spent, everything after
			// this point in the pinned-then-newest order is the tail being
			// dropped, never a later, higher-priority note skipped over an
			// earlier one kept.
			break
		}
		kept = append(kept, note)
		used += size
	}
	return kept, len(notes) - len(kept)
}
