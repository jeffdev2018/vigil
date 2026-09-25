package service

// Brain injection by relevance (JEF-414). A run used to receive every pinned
// note plus the 20 most recently updated others, with no link to the task it
// was about to do: an old but decisive note never arrived, and twenty
// off-topic ones spent the budget. This picks the notes that answer the run's
// own subject instead, and keeps a short recent tail so a fresh note nobody
// is searching for is still seen.

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/brainknowledge"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// brainClaimQueryDescriptionBudget is the head of the description the
	// query keeps, in runes. The head, not the whole body, for the reason the
	// repo index query truncates the same way: a long issue's tail is
	// discussion and acceptance criteria, which match every note equally and
	// only dilute the ranking.
	brainClaimQueryDescriptionBudget = 500
	// brainClaimQueryLabelLimit bounds how many label names join the query, so
	// a heavily labelled issue cannot turn its query into a tag soup.
	brainClaimQueryLabelLimit = 8
	// brainInjectRelevantLimit is how many notes relevance may contribute.
	brainInjectRelevantLimit = 8
	// brainInjectRecentLimit is the recent tail that rides along so a note
	// nobody thought to search for is still visible.
	brainInjectRecentLimit = 4
)

// brainKindWeight multiplies a search hit's score by how much its kind
// matters to a run's briefing: a decision or a procedure earns its place
// above a plain fact more readily, an episode (a one-off run log) less so.
// Unlisted or unknown kinds (an older row, a future kind) weigh like a fact.
var brainKindWeight = map[string]float64{
	NoteKindDecision:  1.2,
	NoteKindProcedure: 1.15,
	NoteKindGlossary:  1.0,
	NoteKindFact:      1.0,
	NoteKindEpisode:   0.85,
}

// weighBrainHitsByKind applies brainKindWeight to each hit's score in place.
func weighBrainHitsByKind(hits []BrainSearchHit) []BrainSearchHit {
	for i, hit := range hits {
		w, ok := brainKindWeight[hit.Note.Kind]
		if !ok {
			w = 1.0
		}
		hits[i].Score = hit.Score * w
	}
	return hits
}

// BriefNoteReason is why one note is in a run's selection: a
// brainknowledge.Reason* constant, plus the search score behind
// brainknowledge.ReasonRelevant.
type BriefNoteReason struct {
	Reason string
	Score  float64
	// Heading is the section of the passage that answered the query, for a
	// ReasonRelevant note. Empty otherwise and for a note with no heading.
	Heading string
}

// BrainClaimQuery is what the workspace Brain is searched WITH for one run:
// the task's title, the head of its description, its project name and its
// label names, one per line. The daemon claim and the native runtime both
// call it, so the two runtimes cannot select from different questions.
//
// The result is empty when the run has no usable subject — a chat turn with no
// title, say — which sends the selection back to pinned-plus-recent.
func BrainClaimQuery(title, description, projectName string, labels []string) string {
	parts := make([]string, 0, 3+len(labels))
	if t := strings.TrimSpace(title); t != "" {
		// A title is normally short; a chat turn or quick-create prompt
		// standing in for one is not, so it pays the same budget.
		parts = append(parts, util.TruncateUTF8Runes(t, brainClaimQueryDescriptionBudget))
	}
	// Project and labels come BEFORE the description even though they are the
	// smaller signal: ParseBrainQuery keeps the first brainQueryMaxItems terms
	// it meets, so anything after a 500-rune description head is dropped
	// before the search ever sees it. Few terms, high signal, so they go
	// first; the description then fills whatever budget is left.
	if p := strings.TrimSpace(projectName); p != "" {
		parts = append(parts, p)
	}
	used := 0
	for _, label := range labels {
		if used >= brainClaimQueryLabelLimit {
			break
		}
		if l := strings.TrimSpace(label); l != "" {
			parts = append(parts, l)
			used++
		}
	}
	if d := strings.TrimSpace(description); d != "" {
		parts = append(parts, util.TruncateUTF8Runes(d, brainClaimQueryDescriptionBudget))
	}
	return strings.Join(parts, "\n")
}

// SelectWorkspaceNotesForBrief picks the Brain notes one run receives: every
// pinned note, then the notes the query ranks, then a short recent tail.
// reasons maps a note id to why it was picked, for the run's knowledge index.
//
// Relevance is best-effort by design. An empty query, a failed search or a
// search that ranks nothing falls back to the pinned-plus-recent selection
// this replaced: knowledge is briefing context, so no failure here may cost a
// claim or a run. Only the underlying note read is returned as an error, and
// its caller already treats that as "this run gets no Workspace Knowledge".
func (s *TaskService) SelectWorkspaceNotesForBrief(ctx context.Context, workspaceID pgtype.UUID, query string) ([]db.WorkspaceNote, map[string]BriefNoteReason, error) {
	rows, err := s.LoadWorkspaceNotesForBrief(ctx, workspaceID)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return nil, nil, nil
	}
	query = strings.TrimSpace(query)
	if query == "" {
		notes, reasons := brainInjectFallback(rows)
		return notes, reasons, nil
	}
	hits, err := s.searchBrainForBrief(ctx, workspaceID, query)
	if err != nil {
		slog.Warn("brain inject: relevance search failed; briefing the pinned and recent notes instead",
			"workspace_id", util.UUIDToString(workspaceID), "error", err)
		notes, reasons := brainInjectFallback(rows)
		return notes, reasons, nil
	}

	notes := make([]db.WorkspaceNote, 0, len(rows))
	reasons := make(map[string]BriefNoteReason, len(rows))
	add := func(note db.WorkspaceNote, reason BriefNoteReason) bool {
		id := util.UUIDToString(note.ID)
		if _, seen := reasons[id]; seen {
			return false
		}
		notes = append(notes, note)
		reasons[id] = reason
		return true
	}
	// Pinned first, in the order the pinned-plus-recent query already
	// established: the workspace's explicit "always know this" set.
	for _, row := range rows {
		if row.Pinned {
			add(row, BriefNoteReason{Reason: brainknowledge.ReasonPinned})
		}
	}
	relevant := 0
	for _, hit := range hits {
		if add(hit.Note, BriefNoteReason{Reason: brainknowledge.ReasonRelevant, Score: hit.Score, Heading: hit.PassageHeading}) {
			relevant++
		}
	}
	if relevant == 0 {
		// Nothing the query found is new to this run. Falling back rather than
		// shipping pinned-plus-four keeps a workspace whose notes simply do not
		// match its tickets on the selection it had before.
		notes, reasons := brainInjectFallback(rows)
		return notes, reasons, nil
	}
	recent := 0
	for _, row := range rows {
		if recent >= brainInjectRecentLimit {
			break
		}
		if row.Pinned {
			continue
		}
		if add(row, BriefNoteReason{Reason: brainknowledge.ReasonRecent}) {
			recent++
		}
	}
	return notes, reasons, nil
}

// brainInjectFallback labels the pinned-plus-recent selection. rows is already
// in the order a run should see it, so only the reasons are added.
func brainInjectFallback(rows []db.WorkspaceNote) ([]db.WorkspaceNote, map[string]BriefNoteReason) {
	reasons := make(map[string]BriefNoteReason, len(rows))
	for _, row := range rows {
		reason := brainknowledge.ReasonRecent
		if row.Pinned {
			reason = brainknowledge.ReasonPinned
		}
		reasons[util.UUIDToString(row.ID)] = BriefNoteReason{Reason: reason}
	}
	return rows, reasons
}

// The native runtime writes no files, so its Brain arrives inside the brief and
// has to stay small: a handful of notes the run can act on, with the tools
// still there for the rest.
const (
	nativeBriefNoteLimit      = 6
	nativeBriefNoteBytes      = 6 * 1024
	nativeBriefNoteContentCap = 600
)

// nativeWorkspaceKnowledgeBrief renders this run's Brain notes for the native
// brief and records them as injected. Same selection as a daemon claim — one
// SelectWorkspaceNotesForBrief, one BrainClaimQuery — rendered compactly
// instead of written to disk. The system prompt still tells the run to search
// for what is not here.
//
// Returns "" on any failure or an empty Brain: knowledge is briefing context,
// never a reason to fail a run.
func (s *NativeAgentService) nativeWorkspaceKnowledgeBrief(ctx context.Context, task db.AgentTaskQueue, agent db.Agent, issue *db.Issue) string {
	if s.Tasks == nil {
		return ""
	}
	title, description, projectName := "", "", ""
	var labels []string
	if issue != nil {
		title, description = issue.Title, issue.Description.String
		rows, err := s.Queries.ListLabelsByIssue(ctx, db.ListLabelsByIssueParams{
			IssueID:     issue.ID,
			WorkspaceID: issue.WorkspaceID,
		})
		if err != nil {
			slog.Warn("native run: load issue labels for the Brain query failed; querying without them",
				"task_id", util.UUIDToString(task.ID), "issue_id", util.UUIDToString(issue.ID), "error", err)
		}
		for _, row := range rows {
			labels = append(labels, row.Name)
		}
		if issue.ProjectID.Valid {
			if project, err := s.Queries.GetProjectInWorkspace(ctx, db.GetProjectInWorkspaceParams{
				ID:          issue.ProjectID,
				WorkspaceID: issue.WorkspaceID,
			}); err == nil {
				projectName = project.Title
			}
		}
	}
	query := BrainClaimQuery(title, description, projectName, labels)
	notes, reasons, err := s.Tasks.SelectWorkspaceNotesForBrief(ctx, agent.WorkspaceID, query)
	if err != nil {
		slog.Warn("native run: load workspace notes failed; continuing without the Brain",
			"task_id", util.UUIDToString(task.ID), "workspace_id", util.UUIDToString(agent.WorkspaceID), "error", err)
		return ""
	}
	if len(notes) == 0 {
		return ""
	}

	var body strings.Builder
	injected := make([]db.WorkspaceNote, 0, nativeBriefNoteLimit)
	for _, note := range notes {
		if len(injected) >= nativeBriefNoteLimit {
			break
		}
		var entry strings.Builder
		if note.Kind != "" && note.Kind != NoteKindFact {
			fmt.Fprintf(&entry, "## [%s] %s\n", note.Kind, note.Title)
		} else {
			fmt.Fprintf(&entry, "## %s\n", note.Title)
		}
		reason := reasons[util.UUIDToString(note.ID)]
		switch {
		case reason.Reason == brainknowledge.ReasonRelevant && reason.Heading != "":
			fmt.Fprintf(&entry, "(relevant · %s)\n", reason.Heading)
		case reason.Reason != "":
			fmt.Fprintf(&entry, "(%s)\n", reason.Reason)
		}
		entry.WriteString(clampString(note.Content, nativeBriefNoteContentCap) + "\n")
		if body.Len() > 0 && body.Len()+entry.Len() > nativeBriefNoteBytes {
			break
		}
		body.WriteString(entry.String())
		injected = append(injected, note)
	}
	if len(injected) == 0 {
		return ""
	}
	RecordRunNoteUsage(ctx, s.Queries, task.ID, agent.ID, "injected", "native_brief", NoteVersionsOf(injected...))

	var b strings.Builder
	b.WriteString("\nWorkspace Knowledge — durable notes this workspace shares, as records:\n")
	b.WriteString(nativeDataFence("workspace knowledge", strings.TrimRight(body.String(), "\n")) + "\n")
	if omitted := len(notes) - len(injected); omitted > 0 {
		fmt.Fprintf(&b, "(%d more note(s) not included here — use search_notes to reach them.)\n", omitted)
	}
	return b.String()
}

// searchBrainForBrief asks the Brain twice and merges: once with the whole
// claim query, once with the run's subject line alone.
//
// Two passes because ParseBrainQuery ORs its terms with no weighting and keeps
// only the first brainQueryMaxItems of them. A title of six words competing
// against twenty words of description is outvoted by its own ticket, and the
// note that answers the subject ranks below notes that merely share a word
// with the body. The subject pass asks the narrow question on its own; the
// merge keeps each note's better score, which is comparable across the two
// because it is the same formula over the same index.
func (s *TaskService) searchBrainForBrief(ctx context.Context, workspaceID pgtype.UUID, query string) ([]BrainSearchHit, error) {
	// BrainClaimQuery puts the run's own title on the first line.
	subject, _, _ := strings.Cut(query, "\n")
	subject = strings.TrimSpace(subject)
	// 2x the final limit: the kind weight below reorders these candidates, so
	// a note the raw SQL score ranked just outside brainInjectRelevantLimit
	// still gets a chance to weigh its way back in before the cut.
	const candidateLimit = 2 * brainInjectRelevantLimit
	hits, err := SearchBrainNotes(ctx, s.Queries, s.NoteEmbedder, BrainSearchParams{
		WorkspaceID: workspaceID, Query: query, Limit: candidateLimit,
	})
	if err != nil {
		return nil, err
	}
	hits = weighBrainHitsByKind(hits)
	if subject != "" && subject != query {
		subjectHits, err := SearchBrainNotes(ctx, s.Queries, s.NoteEmbedder, BrainSearchParams{
			WorkspaceID: workspaceID, Query: subject, Limit: candidateLimit,
		})
		if err != nil {
			// The broad pass already answered; a failed narrow one costs
			// precision, not the selection.
			slog.Warn("brain inject: subject pass failed; keeping the full-query ranking",
				"workspace_id", util.UUIDToString(workspaceID), "error", err)
		} else {
			hits = mergeBrainHits(hits, weighBrainHitsByKind(subjectHits))
		}
	} else {
		// mergeBrainHits re-sorts by score; without it the weighting above
		// must re-order the single pass itself.
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	}
	if len(hits) > brainInjectRelevantLimit {
		hits = hits[:brainInjectRelevantLimit]
	}
	return hits, nil
}

// mergeBrainHits unions two rankings, keeping each note's better-scoring hit,
// and orders the result by score.
func mergeBrainHits(a, b []BrainSearchHit) []BrainSearchHit {
	merged := make([]BrainSearchHit, 0, len(a)+len(b))
	at := make(map[string]int, len(a)+len(b))
	for _, hit := range append(append([]BrainSearchHit{}, a...), b...) {
		id := util.UUIDToString(hit.Note.ID)
		if i, seen := at[id]; seen {
			if hit.Score > merged[i].Score {
				merged[i] = hit
			}
			continue
		}
		at[id] = len(merged)
		merged = append(merged, hit)
	}
	sort.SliceStable(merged, func(i, j int) bool { return merged[i].Score > merged[j].Score })
	return merged
}
