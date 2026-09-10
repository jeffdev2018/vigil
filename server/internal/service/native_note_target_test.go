package service

import "testing"

func TestParseNoteTargetContext(t *testing.T) {
	nt, ok := ParseNoteTargetContext([]byte(`{"type":"note_target","note_id":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee","instruction":"Add the Q3 numbers"}`))
	if !ok || nt.NoteID != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" || nt.Instruction != "Add the Q3 numbers" {
		t.Fatalf("got %+v ok=%v", nt, ok)
	}
	if _, ok := ParseNoteTargetContext([]byte(`{"type":"quick_create","note_id":"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}`)); ok {
		t.Fatal("quick_create must not parse as note_target")
	}
}

func TestParseNoteTargetFromAutopilotDescription(t *testing.T) {
	nt, ok := ParseNoteTargetFromAutopilotDescription("Weekly CR\n[[target_note:aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee]]\nKeep it short.")
	if !ok || nt.NoteID != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("bracket form: %+v ok=%v", nt, ok)
	}
	nt, ok = ParseNoteTargetFromAutopilotDescription("Do the thing\ntarget_note: bbbbbbbb-bbbb-cccc-dddd-eeeeeeeeeeee\n")
	if !ok || nt.NoteID != "bbbbbbbb-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Fatalf("line form: %+v ok=%v", nt, ok)
	}
	if _, ok := ParseNoteTargetFromAutopilotDescription("no pin here"); ok {
		t.Fatal("expected no pin")
	}
}
