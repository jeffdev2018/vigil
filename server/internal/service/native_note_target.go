package service

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Living documents (N15): a native run can target a workspace note as its
// deliverable. The note already supports optimistic revisions via
// update_note; this wires the brief (and autopilot descriptions) so the
// model knows the note — not a comment — is what must evolve.

const NoteTargetContextType = "note_target"

// NoteTargetContext is stored on agent_task_queue.context when the run's
// job is to enrich a dedicated note.
type NoteTargetContext struct {
	Type        string `json:"type"`
	NoteID      string `json:"note_id"`
	Instruction string `json:"instruction,omitempty"`
}

// ParseNoteTargetContext reads a note_target (or a loose note_id) from task
// context JSON. Missing or malformed context yields ok=false.
func ParseNoteTargetContext(raw []byte) (NoteTargetContext, bool) {
	if len(raw) == 0 {
		return NoteTargetContext{}, false
	}
	var nt NoteTargetContext
	if err := json.Unmarshal(raw, &nt); err != nil {
		return NoteTargetContext{}, false
	}
	nt.NoteID = strings.TrimSpace(nt.NoteID)
	nt.Instruction = strings.TrimSpace(nt.Instruction)
	if nt.NoteID == "" {
		return NoteTargetContext{}, false
	}
	if nt.Type != "" && nt.Type != NoteTargetContextType {
		return NoteTargetContext{}, false
	}
	nt.Type = NoteTargetContextType
	return nt, true
}

// Autopilot descriptions may pin a living document with either
// [[target_note:<uuid>]] or a line `target_note: <uuid>`.
var nativeAutopilotNoteTargetRe = regexp.MustCompile(`(?i)(?:\[\[target_note:([0-9a-f-]{36})\]\]|(?m)^target_note:\s*([0-9a-f-]{36})\s*$)`)

// ParseNoteTargetFromAutopilotDescription extracts a note id from the
// autopilot's instructions, if authors pinned one.
func ParseNoteTargetFromAutopilotDescription(description string) (NoteTargetContext, bool) {
	m := nativeAutopilotNoteTargetRe.FindStringSubmatch(description)
	if m == nil {
		return NoteTargetContext{}, false
	}
	id := m[1]
	if id == "" {
		id = m[2]
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return NoteTargetContext{}, false
	}
	return NoteTargetContext{Type: NoteTargetContextType, NoteID: id}, true
}

// nativeAppendNoteTargetBrief loads the note and appends the living-document
// contract to the brief. Failures are soft: a missing note must not kill a
// run that still has other work.
func nativeAppendNoteTargetBrief(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, nt NoteTargetContext, b *strings.Builder) {
	if q == nil || b == nil || nt.NoteID == "" {
		return
	}
	noteUUID, err := util.ParseUUID(nt.NoteID)
	if err != nil {
		fmt.Fprintf(b, "\nLiving document: note_id %q is not a valid uuid — create or find the note, then update_note.\n", nt.NoteID)
		return
	}
	note, err := q.GetWorkspaceNote(ctx, db.GetWorkspaceNoteParams{ID: noteUUID, WorkspaceID: workspaceID})
	if err != nil {
		fmt.Fprintf(b, "\nLiving document: note %s could not be loaded — search_notes or save_note, then update it.\n", nt.NoteID)
		return
	}
	b.WriteString("\nLiving document (your deliverable for this run):\n")
	fmt.Fprintf(b, "- note_id: %s\n", util.UUIDToString(note.ID))
	fmt.Fprintf(b, "- title: %s\n", note.Title)
	fmt.Fprintf(b, "- revision: %d (pass this back via update_note after you read it — optimistic concurrency)\n", note.Revision)
	if nt.Instruction != "" {
		b.WriteString("- instruction: " + nativeDataFence("note instruction", clampString(nt.Instruction, 2000)) + "\n")
	}
	b.WriteString("Current content:\n")
	b.WriteString(nativeDataFence("workspace note", nativeHeadTail(note.Content, nativeBriefDescriptionCap)))
	b.WriteString("\nUpdate this note with the update_note tool (note_id + new content). ")
	b.WriteString("A comment alone is not enough: the note must evolve (revision +1) so the next run inherits it.\n")
}
