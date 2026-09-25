package service

// Brain note kinds (JEF-415 / B04): the shape of durable knowledge a note
// holds. Injection weighs a decision or a procedure above a plain fact; the
// REST, MCP, native tool and CLI surfaces all validate against this one list
// so "unknown kind" means the same thing everywhere.
const (
	NoteKindFact      = "fact"
	NoteKindDecision  = "decision"
	NoteKindProcedure = "procedure"
	NoteKindGlossary  = "glossary"
	NoteKindEpisode   = "episode"

	// DefaultNoteKind is what a note gets when none is given, and what an
	// older export or an invalid stored value falls back to.
	DefaultNoteKind = NoteKindFact
)

// NoteKinds is every valid kind, in the order surfaces should list them.
var NoteKinds = []string{NoteKindFact, NoteKindDecision, NoteKindProcedure, NoteKindGlossary, NoteKindEpisode}

// NoteKindToolDesc is the one-line explanation of each kind shown to a model
// choosing between them in an MCP or native tool schema.
const NoteKindToolDesc = "What shape of knowledge this is: fact (a durable fact about the workspace or codebase), decision (a choice and why it was made), procedure (steps to do something), glossary (a term and its meaning), episode (a one-off run log or event worth keeping, least durable)."

// ValidNoteKind reports whether k is one of NoteKinds.
func ValidNoteKind(k string) bool {
	for _, v := range NoteKinds {
		if v == k {
			return true
		}
	}
	return false
}
