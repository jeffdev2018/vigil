package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Workspace Brain curation. A knowledge base that only ever grows decays:
// near-duplicates accumulate, titles stay vague, tags drift, and notes that
// stopped being true keep reaching every run. A daily pass asks the
// deployment's assist-layer LLM for a plan over the workspace's notes and
// applies it.
//
// The prompt below is derived from Rowboat's knowledge-curation ("gardener")
// agent — rowboatlabs/rowboat, apps/x/packages/core/src/knowledge/note_curation.ts,
// licensed Apache-2.0. Its per-note rewrite is adapted here into a
// whole-corpus plan: Multica's notes are already short and single-idea, so the
// decay that matters is duplication and staleness across notes rather than
// bloat inside one. The quality contract it enforces — no new facts, no
// deleted substance, temporal hygiene, honest staleness — is kept verbatim in
// spirit. See NOTICE at the repository root.

const (
	// A workspace is curated only when at least this many live notes changed
	// since the previous pass. One edit is somebody fixing a typo.
	brainCurationMinChangedNotes = 2
	// How far back "since the last pass" reaches. The job runs daily, and the
	// window is deliberately a little wider than the cadence so a late tick
	// does not skip a day's edits.
	brainCurationWindow = 26 * time.Hour
	// Notes sent upstream in one pass.
	brainCurationMaxNotes = 60
	// Per-note body budget in the prompt. The pass decides on titles, tags,
	// duplication and staleness; it does not need a whole note to see those.
	brainCurationNoteBudget = 1200
	brainCurationTimeout    = 60 * time.Second
	// A plan larger than this is a model that misread the corpus, not a
	// workspace that needed 200 edits.
	brainCurationMaxPlanOps = 40
)

// WorkspaceNoteCurationLLM is the seam TaskService uses for the curation pass,
// satisfied by *llm.Client. Same shape as AgentMemoryLLM: an interface so
// tests can drive the pass without an HTTP upstream.
type WorkspaceNoteCurationLLM interface {
	Enabled() bool
	GenerateJSON(ctx context.Context, model, systemPrompt, userPrompt string, temperature float64, maxCompletionTokens int64) (string, error)
}

// brainCurationDisabledOnce keeps the "no LLM configured" line to one per
// process. The pass runs daily forever on a self-hosted deployment with no
// MULTICA_LLM_* configuration; saying so every day is noise.
var brainCurationDisabledOnce sync.Once

// brainCurationPlan is what the model returns.
type brainCurationPlan struct {
	Merge []struct {
		Into string   `json:"into"`
		From []string `json:"from"`
	} `json:"merge"`
	Retitle []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"retitle"`
	Tag []struct {
		ID   string   `json:"id"`
		Tags []string `json:"tags"`
	} `json:"tag"`
	Archive []struct {
		ID     string `json:"id"`
		Reason string `json:"reason"`
	} `json:"archive"`
}

func (p brainCurationPlan) opCount() int {
	n := len(p.Retitle) + len(p.Tag) + len(p.Archive)
	for _, m := range p.Merge {
		n += len(m.From)
	}
	return n
}

const brainCurationSystemPrompt = `You are the curator of a software team's shared knowledge base.

Each note is one durable fact about the team's workspace: a decision and its reason, a convention, a hard fact about the codebase, who owns what. Notes are written by both humans and by agent runs, so the same fact often arrives several times under different titles, titles are often vague, tags drift, and notes that stopped being true keep being read.

You are given the whole corpus and you return a PLAN. You never rewrite a note's body.

NON-NEGOTIABLE RULES

1. No new facts. Every title you propose must be derivable from the note it belongs to.
2. No deleted substance. Merge two notes only when the target ALREADY states everything the source does. If the source carries anything the target does not, leave both alone.
3. When in doubt, do nothing. An empty plan is a correct answer for a healthy corpus.
4. Never touch a note only because it is old. Age is not staleness; a convention from last year is still the convention.

WHAT TO PROPOSE

- merge: near-duplicates. "into" is the note that survives (prefer the one with the fuller body, then the more recently updated); "from" lists the ids it already covers. Each source is archived with a pointer to the target, so nothing is lost.
- retitle: a title that does not state the fact. "Deploys" becomes "Deploys go through the release tag". Keep it under 200 characters and derivable from the body. Do not retitle a title that already states its fact.
- tag: normalize tags so the same subject carries the same tag across notes. Lowercase, at most 10 per note, no near-synonyms (pick one of "ci"/"ci-cd"). Only propose a change when it actually differs from the current tags.
- archive: notes that have become false, notes superseded by another note, and notes that were never durable knowledge in the first place (a run log, a status report, "the build is red"). Give a one-line reason.

Do not propose an operation on a pinned note unless it is a duplicate of another pinned note.

OUTPUT

Return ONLY a JSON object of this exact shape, with no prose around it:

{"merge":[{"into":"<id>","from":["<id>"]}],"retitle":[{"id":"<id>","title":"<new title>"}],"tag":[{"id":"<id>","tags":["a","b"]}],"archive":[{"id":"<id>","reason":"<one line>"}]}

Every array may be empty. Use only ids that appear in the input.`

// renderBrainCurationPrompt builds the user message: an index line per note
// followed by each note's (budgeted) body.
func renderBrainCurationPrompt(notes []db.WorkspaceNote) string {
	var b strings.Builder
	b.WriteString("# Index\n\n")
	for _, n := range notes {
		fmt.Fprintf(&b, "- id: %s | title: %s | tags: %s | pinned: %t | updated: %s\n",
			util.UUIDToString(n.ID), n.Title, strings.Join(n.Tags, ","), n.Pinned,
			n.UpdatedAt.Time.UTC().Format(time.RFC3339))
	}
	b.WriteString("\n# Notes\n")
	for _, n := range notes {
		body := n.Content
		if utf8.RuneCountInString(body) > brainCurationNoteBudget {
			body = string([]rune(body)[:brainCurationNoteBudget]) + "\n…(truncated)"
		}
		fmt.Fprintf(&b, "\n## %s\n\nid: %s\n\n%s\n", n.Title, util.UUIDToString(n.ID), body)
	}
	return b.String()
}

// CurateWorkspaceBrains runs one curation pass over every workspace whose
// Brain changed enough to be worth it. It returns the number of plan
// operations actually applied.
//
// The whole path is a nicety: a disabled or failing LLM must cost the
// deployment nothing, so every failure is logged and skipped rather than
// returned — except a failure to enumerate workspaces, which means the pass
// did not run at all.
func (s *TaskService) CurateWorkspaceBrains(ctx context.Context, now time.Time) (int, error) {
	if s.BrainCuration == nil || !s.BrainCuration.Enabled() {
		brainCurationDisabledOnce.Do(func() {
			slog.Info("workspace brain curation disabled: no assist-layer LLM configured")
		})
		return 0, nil
	}

	workspaceIDs, err := s.Queries.ListWorkspaceIDsWithNotes(ctx)
	if err != nil {
		return 0, fmt.Errorf("list workspaces with notes: %w", err)
	}

	applied := 0
	for _, workspaceID := range workspaceIDs {
		n, err := s.curateWorkspaceBrain(ctx, workspaceID, now)
		if err != nil {
			slog.Warn("workspace brain curation failed for one workspace; continuing",
				"workspace_id", util.UUIDToString(workspaceID), "error", err)
			continue
		}
		applied += n
	}
	return applied, nil
}

func (s *TaskService) curateWorkspaceBrain(ctx context.Context, workspaceID pgtype.UUID, now time.Time) (int, error) {
	changed, err := s.Queries.CountWorkspaceNotesUpdatedSince(ctx, db.CountWorkspaceNotesUpdatedSinceParams{
		WorkspaceID: workspaceID,
		UpdatedAt:   pgtype.Timestamptz{Time: now.Add(-brainCurationWindow), Valid: true},
	})
	if err != nil {
		return 0, fmt.Errorf("count changed notes: %w", err)
	}
	if changed < brainCurationMinChangedNotes {
		return 0, nil
	}

	notes, err := s.Queries.ListPinnedAndRecentWorkspaceNotesForBrief(ctx, db.ListPinnedAndRecentWorkspaceNotesForBriefParams{
		WorkspaceID: workspaceID,
		NoteLimit:   brainCurationMaxNotes,
	})
	if err != nil {
		return 0, fmt.Errorf("list notes: %w", err)
	}
	if len(notes) < brainCurationMinChangedNotes {
		return 0, nil
	}

	llmCtx, cancel := context.WithTimeout(ctx, brainCurationTimeout)
	defer cancel()
	raw, err := s.BrainCuration.GenerateJSON(llmCtx,
		"", // deployment default: MULTICA_LLM_DEFAULT_MODEL, else llm.FallbackModel
		brainCurationSystemPrompt,
		renderBrainCurationPrompt(notes),
		0.1,
		2048,
	)
	if err != nil {
		return 0, fmt.Errorf("curation llm: %w", err)
	}

	var plan brainCurationPlan
	if err := json.Unmarshal([]byte(raw), &plan); err != nil {
		return 0, fmt.Errorf("parse curation plan: %w", err)
	}
	if ops := plan.opCount(); ops > brainCurationMaxPlanOps {
		return 0, fmt.Errorf("curation plan has %d operations, over the %d ceiling; refusing to apply", ops, brainCurationMaxPlanOps)
	}

	return s.applyBrainCurationPlan(ctx, workspaceID, notes, plan), nil
}

// applyBrainCurationPlan applies what the model proposed, one operation at a
// time. Every id is checked against the notes actually sent upstream, so a
// hallucinated or cross-workspace id is dropped instead of reaching a write.
// A failed operation is logged and skipped: a half-applied plan is a tidier
// Brain than an abandoned one.
func (s *TaskService) applyBrainCurationPlan(ctx context.Context, workspaceID pgtype.UUID, notes []db.WorkspaceNote, plan brainCurationPlan) int {
	known := make(map[string]db.WorkspaceNote, len(notes))
	for _, n := range notes {
		known[util.UUIDToString(n.ID)] = n
	}
	// Revisions move as the plan is applied (retitle then tag on the same
	// note), so track the live one instead of the value read before the pass.
	revision := make(map[string]int64, len(notes))
	for id, n := range known {
		revision[id] = n.Revision
	}
	archived := make(map[string]bool, len(notes))
	applied := 0

	logSkip := func(op, id string, err error) {
		slog.Warn("workspace brain curation: operation skipped",
			"op", op, "note_id", id, "workspace_id", util.UUIDToString(workspaceID), "error", err)
	}

	for _, m := range plan.Merge {
		target, ok := known[m.Into]
		if !ok {
			continue
		}
		for _, sourceID := range m.From {
			source, ok := known[sourceID]
			if !ok || sourceID == m.Into || archived[sourceID] {
				continue
			}
			// The source keeps its body and becomes readable-but-inert, with a
			// pointer to the note that now carries the fact. That is what makes
			// a wrong merge recoverable.
			if _, err := s.Queries.SetWorkspaceNoteArchived(ctx, db.SetWorkspaceNoteArchivedParams{
				ID:          source.ID,
				WorkspaceID: workspaceID,
				ArchivedAt:  pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
				MergedInto:  target.ID,
			}); err != nil {
				logSkip("merge", sourceID, err)
				continue
			}
			archived[sourceID] = true
			applied++
		}
	}

	for _, rt := range plan.Retitle {
		note, ok := known[rt.ID]
		if !ok || archived[rt.ID] {
			continue
		}
		title := strings.TrimSpace(util.SanitizeTextForPostgres(rt.Title))
		if title == "" || utf8.RuneCountInString(title) > 200 || title == note.Title {
			continue
		}
		updated, err := s.Queries.UpdateWorkspaceNote(ctx, db.UpdateWorkspaceNoteParams{
			ID:               note.ID,
			WorkspaceID:      workspaceID,
			Title:            pgtype.Text{String: title, Valid: true},
			ExpectedRevision: revision[rt.ID],
		})
		if err != nil {
			logSkip("retitle", rt.ID, err)
			continue
		}
		revision[rt.ID] = updated.Revision
		applied++
	}

	for _, tg := range plan.Tag {
		note, ok := known[tg.ID]
		if !ok || archived[tg.ID] {
			continue
		}
		tags := normalizeCurationTags(tg.Tags)
		if sameTags(tags, note.Tags) {
			continue
		}
		updated, err := s.Queries.UpdateWorkspaceNote(ctx, db.UpdateWorkspaceNoteParams{
			ID:               note.ID,
			WorkspaceID:      workspaceID,
			Tags:             tags,
			ExpectedRevision: revision[tg.ID],
		})
		if err != nil {
			logSkip("tag", tg.ID, err)
			continue
		}
		revision[tg.ID] = updated.Revision
		applied++
	}

	for _, ar := range plan.Archive {
		note, ok := known[ar.ID]
		if !ok || archived[ar.ID] {
			continue
		}
		if _, err := s.Queries.SetWorkspaceNoteArchived(ctx, db.SetWorkspaceNoteArchivedParams{
			ID:          note.ID,
			WorkspaceID: workspaceID,
			ArchivedAt:  pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		}); err != nil {
			logSkip("archive", ar.ID, err)
			continue
		}
		archived[ar.ID] = true
		applied++
	}

	return applied
}

// normalizeCurationTags mirrors the REST normalization so a model-proposed tag
// set is stored exactly as a human-typed one would be.
func normalizeCurationTags(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, r := range raw {
		tag := strings.ToLower(strings.TrimSpace(util.SanitizeTextForPostgres(r)))
		if tag == "" || utf8.RuneCountInString(tag) > 50 {
			continue
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
		if len(out) == 10 {
			break
		}
	}
	sort.Strings(out)
	return out
}

func sameTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
