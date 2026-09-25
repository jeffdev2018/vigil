package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/brainknowledge"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Pure unit tests for the claim query. The selection itself is DB-backed and
// lives below.

func TestBrainClaimQueryFields(t *testing.T) {
	got := BrainClaimQuery("  Deploy fails  ", "  the tag is unsigned  ", " Coralis Desk ", []string{" ops ", "", "release"})
	// The subject stays on the first line — searchBrainForBrief cuts it back
	// out to ask the narrow question — and project and labels come before the
	// description so ParseBrainQuery's item cap cannot drop them.
	want := "Deploy fails\nCoralis Desk\nops\nrelease\nthe tag is unsigned"
	if got != want {
		t.Fatalf("query =\n%q\nwant\n%q", got, want)
	}
}

func TestBrainClaimQueryTruncatesAndBoundsLabels(t *testing.T) {
	long := strings.Repeat("é", brainClaimQueryDescriptionBudget+50)
	labels := make([]string, brainClaimQueryLabelLimit+5)
	for i := range labels {
		labels[i] = fmt.Sprintf("label%d", i)
	}
	lines := strings.Split(BrainClaimQuery(long, long, "", labels), "\n")
	if len(lines) != 2+brainClaimQueryLabelLimit {
		t.Fatalf("got %d lines (%q), want title + %d labels + description", len(lines), lines, brainClaimQueryLabelLimit)
	}
	for _, i := range []int{0, len(lines) - 1} {
		if n := len([]rune(lines[i])); n != brainClaimQueryDescriptionBudget {
			t.Errorf("line %d kept %d runes, want the %d-rune budget", i, n, brainClaimQueryDescriptionBudget)
		}
	}
	if lines[1] != "label0" || lines[brainClaimQueryLabelLimit] != fmt.Sprintf("label%d", brainClaimQueryLabelLimit-1) {
		t.Errorf("labels = %q, want the first %d in order", lines[1:len(lines)-1], brainClaimQueryLabelLimit)
	}
}

// weighBrainHitsByKind is pure: this covers the ordering the injection weight
// table (JEF-415 / B04) produces without a database.
func TestWeighBrainHitsByKindOrdersByWeightedScore(t *testing.T) {
	kinds := []string{NoteKindEpisode, NoteKindFact, NoteKindDecision, NoteKindProcedure, NoteKindGlossary, "future-kind"}
	hits := make([]BrainSearchHit, len(kinds))
	for i, k := range kinds {
		hits[i] = BrainSearchHit{Note: db.WorkspaceNote{Kind: k}, Score: 1.0}
	}
	weighed := weighBrainHitsByKind(hits)
	want := map[string]float64{
		NoteKindEpisode: 0.85, NoteKindFact: 1.0, NoteKindDecision: 1.2,
		NoteKindProcedure: 1.15, NoteKindGlossary: 1.0, "future-kind": 1.0, // unknown weighs like fact
	}
	scoreOf := map[string]float64{}
	for _, h := range weighed {
		scoreOf[h.Note.Kind] = h.Score
		if h.Score != want[h.Note.Kind] {
			t.Errorf("kind %q score = %v, want %v", h.Note.Kind, h.Score, want[h.Note.Kind])
		}
	}
	// A decision must now outrank a same-relevance procedure, fact and episode.
	if !(scoreOf[NoteKindDecision] > scoreOf[NoteKindProcedure] &&
		scoreOf[NoteKindProcedure] > scoreOf[NoteKindFact] &&
		scoreOf[NoteKindFact] > scoreOf[NoteKindEpisode]) {
		t.Fatalf("weighted order wrong: %+v", scoreOf)
	}
}

func TestBrainClaimQueryEmptyWithoutText(t *testing.T) {
	for _, tc := range []struct{ title, description, project string }{
		{"", "", ""},
		{"  ", "\n\t", " "},
	} {
		if got := BrainClaimQuery(tc.title, tc.description, tc.project, nil); got != "" {
			t.Errorf("BrainClaimQuery(%q,%q,%q) = %q, want empty so the selection falls back", tc.title, tc.description, tc.project, got)
		}
	}
}

// brainInjectFixture is a workspace with notes, and the TaskService that
// selects from it.
type brainInjectFixture struct {
	svc  *TaskService
	fx   *testutil.Fixture
	wsID string
}

func newBrainInjectFixture(t *testing.T) brainInjectFixture {
	t.Helper()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("brain-inject-%d", suffix), fmt.Sprintf("brain-inject-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("brain-inject-%d", suffix), fmt.Sprintf("brain-inject-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	return brainInjectFixture{svc: &TaskService{Queries: db.New(pool)}, fx: fx, wsID: ws}
}

// note inserts one note. ageDays back-dates updated_at so "recent" is
// controllable; pinned notes are labelled by the pinned rule, not by age.
func (f brainInjectFixture) note(t *testing.T, title, content string, pinned bool, ageDays int) string {
	t.Helper()
	at := time.Now().UTC().AddDate(0, 0, -ageDays)
	return f.fx.Insert(t, "workspace_note", testutil.Cols{
		"id":           testutil.Raw("gen_random_uuid()"),
		"workspace_id": f.wsID,
		"title":        title,
		"content":      content,
		"pinned":       pinned,
		"created_at":   at,
		"updated_at":   at,
	})
}

func (f brainInjectFixture) selected(t *testing.T, query string) ([]string, map[string]BriefNoteReason) {
	t.Helper()
	notes, reasons, err := f.svc.SelectWorkspaceNotesForBrief(context.Background(), util.MustParseUUID(f.wsID), query)
	if err != nil {
		t.Fatalf("SelectWorkspaceNotesForBrief: %v", err)
	}
	ids := make([]string, 0, len(notes))
	for _, n := range notes {
		ids = append(ids, util.UUIDToString(n.ID))
	}
	return ids, reasons
}

func selectedNote(ids []string, id string) bool {
	for _, got := range ids {
		if got == id {
			return true
		}
	}
	return false
}

// The point of JEF-414: an old note that answers the task beats a fresh one
// that does not, and the pinned set is never traded away for either.
func TestSelectWorkspaceNotesForBriefPrefersRelevanceOverAge(t *testing.T) {
	f := newBrainInjectFixture(t)
	old := f.note(t, "Procédure de déploiement en production",
		"Le déploiement passe par un tag Git signé ; jamais depuis une branche.", false, 400)
	pinned := f.note(t, "Contacts d'astreinte", "Appeler l'astreinte ops au 0600.", true, 500)
	var freshOffTopic []string
	for i := 0; i < brainInjectRecentLimit+6; i++ {
		freshOffTopic = append(freshOffTopic, f.note(t, fmt.Sprintf("Note de réunion %d", i),
			fmt.Sprintf("Compte rendu de la réunion hebdomadaire numéro %d, rien à retenir.", i), false, i))
	}

	query := BrainClaimQuery("Le déploiement échoue depuis une branche", "", "", []string{"ops"})
	ids, reasons := f.selected(t, query)

	if !selectedNote(ids, old) {
		t.Fatalf("the 400-day-old deployment note is not in %v; relevance did not beat age", ids)
	}
	if got := reasons[old].Reason; got != brainknowledge.ReasonRelevant {
		t.Errorf("reason for the answering note = %q, want %q", got, brainknowledge.ReasonRelevant)
	}
	if reasons[old].Score <= 0 {
		t.Errorf("relevant note carries score %v, want the search score the index prints", reasons[old].Score)
	}
	if !selectedNote(ids, pinned) {
		t.Errorf("pinned note missing from %v", ids)
	}
	if got := reasons[pinned].Reason; got != brainknowledge.ReasonPinned {
		t.Errorf("reason for the pinned note = %q, want %q", got, brainknowledge.ReasonPinned)
	}
	if ids[0] != pinned {
		t.Errorf("selection starts with %s, want the pinned note first", ids[0])
	}
	// The off-topic notes are not all excluded — a short recent tail rides
	// along on purpose — but the tail is bounded, so most of them are gone.
	offTopic := 0
	for _, id := range freshOffTopic {
		if selectedNote(ids, id) {
			offTopic++
		}
	}
	if offTopic > brainInjectRecentLimit {
		t.Errorf("%d off-topic recent notes selected, want at most the %d-note tail", offTopic, brainInjectRecentLimit)
	}
	recent := 0
	for _, r := range reasons {
		if r.Reason == brainknowledge.ReasonRecent {
			recent++
		}
	}
	if recent > brainInjectRecentLimit {
		t.Errorf("%d notes labelled recent, want at most %d", recent, brainInjectRecentLimit)
	}
	if len(reasons) != len(ids) {
		t.Errorf("%d reasons for %d notes; every note must say why it is there", len(reasons), len(ids))
	}
}

// A note that both the pinned rule and the search would contribute appears
// once, as pinned: the reason the run reads is the stronger one.
func TestSelectWorkspaceNotesForBriefDeduplicates(t *testing.T) {
	f := newBrainInjectFixture(t)
	id := f.note(t, "Procédure de déploiement", "Le déploiement passe par un tag signé.", true, 10)
	f.note(t, "Autre sujet", "Rien à voir avec le déploiement du tout.", false, 1)

	ids, reasons := f.selected(t, BrainClaimQuery("déploiement tag signé", "", "", nil))
	seen := 0
	for _, got := range ids {
		if got == id {
			seen++
		}
	}
	if seen != 1 {
		t.Fatalf("note appears %d times in %v, want once", seen, ids)
	}
	if got := reasons[id].Reason; got != brainknowledge.ReasonPinned {
		t.Errorf("reason = %q, want %q for a pinned note the search also ranked", got, brainknowledge.ReasonPinned)
	}
}

// JEF-415 / B04: a note mirrored from a decision record already reaches a
// run through pinning or relevance search; it must not also ride the plain
// "recent" tail, or a workspace's steady stream of accepted decisions would
// crowd every other kind of note out of it.
func TestLoadWorkspaceNotesForBriefExcludesDecisionSourcedNotesFromTheRecentTail(t *testing.T) {
	f := newBrainInjectFixture(t)
	recentFact := f.note(t, "Fact note", "Some fact.", false, 1)
	recentDecision := f.fx.Insert(t, "workspace_note", testutil.Cols{
		"id":           testutil.Raw("gen_random_uuid()"),
		"workspace_id": f.wsID,
		"title":        "Mirrored decision",
		"content":      "A decision, mirrored as a note.",
		"source":       "decision",
	})
	pinnedDecision := f.fx.Insert(t, "workspace_note", testutil.Cols{
		"id":           testutil.Raw("gen_random_uuid()"),
		"workspace_id": f.wsID,
		"title":        "Pinned decision",
		"content":      "A decision worth always knowing.",
		"source":       "decision",
		"pinned":       true,
	})

	rows, err := f.svc.LoadWorkspaceNotesForBrief(context.Background(), util.MustParseUUID(f.wsID))
	if err != nil {
		t.Fatalf("LoadWorkspaceNotesForBrief: %v", err)
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = util.UUIDToString(r.ID)
	}
	if !selectedNote(ids, recentFact) {
		t.Errorf("plain recent note missing from %v", ids)
	}
	if selectedNote(ids, recentDecision) {
		t.Errorf("non-pinned decision-sourced note reached the recent tail: %v", ids)
	}
	if !selectedNote(ids, pinnedDecision) {
		t.Errorf("pinned decision-sourced note is missing (pinning must still work): %v", ids)
	}
}

// Empty query and a query nothing matches both fall back to the selection
// this replaced, so a workspace whose notes do not match its tickets keeps
// what it had.
func TestSelectWorkspaceNotesForBriefFallsBack(t *testing.T) {
	f := newBrainInjectFixture(t)
	pinned := f.note(t, "Contacts d'astreinte", "Appeler l'astreinte ops.", true, 300)
	var recent []string
	for i := 0; i < workspaceBriefNoteRecentLimit+5; i++ {
		recent = append(recent, f.note(t, fmt.Sprintf("Réunion %d", i), fmt.Sprintf("Compte rendu %d.", i), false, i))
	}
	legacy, err := f.svc.LoadWorkspaceNotesForBrief(context.Background(), util.MustParseUUID(f.wsID))
	if err != nil {
		t.Fatalf("LoadWorkspaceNotesForBrief: %v", err)
	}

	for name, query := range map[string]string{
		"empty query":  "",
		"blank query":  "   \n  ",
		"no match":     BrainClaimQuery("zzqqxx unmatchable subject", "", "", nil),
		"stopwords":    BrainClaimQuery("the and of", "", "", nil),
		"from a chat":  BrainClaimQuery("", "", "", nil),
		"only a label": BrainClaimQuery("", "", "", []string{"zzqqxx"}),
	} {
		ids, reasons := f.selected(t, query)
		if len(ids) != len(legacy) {
			t.Errorf("%s: selected %d notes, want the %d of the pinned-plus-recent fallback", name, len(ids), len(legacy))
			continue
		}
		for i, row := range legacy {
			if ids[i] != util.UUIDToString(row.ID) {
				t.Errorf("%s: position %d is %s, want %s (fallback order must be untouched)", name, i, ids[i], util.UUIDToString(row.ID))
			}
		}
		if got := reasons[pinned].Reason; got != brainknowledge.ReasonPinned {
			t.Errorf("%s: pinned note labelled %q", name, got)
		}
		if got := reasons[recent[0]].Reason; got != brainknowledge.ReasonRecent {
			t.Errorf("%s: recent note labelled %q, want %q", name, got, brainknowledge.ReasonRecent)
		}
		for _, r := range reasons {
			if r.Reason == brainknowledge.ReasonRelevant {
				t.Errorf("%s: a note is labelled relevant although nothing was found", name)
				break
			}
		}
	}
}

func TestSelectWorkspaceNotesForBriefEmptyBrain(t *testing.T) {
	f := newBrainInjectFixture(t)
	notes, reasons, err := f.svc.SelectWorkspaceNotesForBrief(context.Background(), util.MustParseUUID(f.wsID), "anything at all")
	if err != nil {
		t.Fatalf("SelectWorkspaceNotesForBrief: %v", err)
	}
	if len(notes) != 0 || len(reasons) != 0 {
		t.Fatalf("empty Brain returned %d notes and %d reasons, want nothing", len(notes), len(reasons))
	}
}

// Archived notes never reach a run, whichever leg would have contributed them.
func TestSelectWorkspaceNotesForBriefExcludesArchived(t *testing.T) {
	f := newBrainInjectFixture(t)
	live := f.note(t, "Procédure de déploiement", "Le déploiement passe par un tag signé.", false, 5)
	archived := f.fx.Insert(t, "workspace_note", testutil.Cols{
		"id":           testutil.Raw("gen_random_uuid()"),
		"workspace_id": f.wsID,
		"title":        "Ancienne procédure de déploiement",
		"content":      "Le déploiement passait par un tag signé, procédure abandonnée.",
		"pinned":       true,
		"archived_at":  time.Now().UTC(),
	})
	ids, _ := f.selected(t, BrainClaimQuery("déploiement tag signé", "", "", nil))
	if !selectedNote(ids, live) {
		t.Errorf("live note missing from %v", ids)
	}
	if selectedNote(ids, archived) {
		t.Errorf("archived note %s reached the run", archived)
	}
}

// JEF-415 / B04: weighBrainHitsByKind used to run AFTER the SQL query had
// already cut results to brainInjectRelevantLimit, so a note the raw fused
// score ranked just outside that window could never be weighted back in.
// searchBrainForBrief now asks for 2x the final limit so a candidate like
// that gets a chance to out-rank a weaker same-kind hit before the cut.
func TestSelectWorkspaceNotesForBriefWeighsCandidatesBeyondTheOldSQLLimit(t *testing.T) {
	f := newBrainInjectFixture(t)
	term := "warehouse"
	// 8 fact notes with strictly decreasing term frequency, so the lexical
	// rank (and the fused RRF score) orders them 1..8 by construction.
	for i, reps := range []int{100, 90, 80, 70, 60, 50, 40, 30} {
		f.fx.Insert(t, "workspace_note", testutil.Cols{
			"id":           testutil.Raw("gen_random_uuid()"),
			"workspace_id": f.wsID,
			"title":        fmt.Sprintf("Fact note %d", i),
			"content":      strings.Repeat(term+" ", reps),
		})
	}
	// Ranked 9th by raw score — just outside the old top-8 SQL window — but
	// its 1.2x decision weight is enough to out-rank the 8 fact notes above,
	// once it is even allowed to compete.
	decision := f.fx.Insert(t, "workspace_note", testutil.Cols{
		"id":           testutil.Raw("gen_random_uuid()"),
		"workspace_id": f.wsID,
		"title":        "Decision on warehouse capacity",
		"content":      strings.Repeat(term+" ", 5),
		"kind":         "decision",
	})

	ids, reasons := f.selected(t, term)

	if !selectedNote(ids, decision) {
		t.Fatalf("decision note ranked just outside the raw top %d did not enter after kind weighting; ids=%v", brainInjectRelevantLimit, ids)
	}
	if got := reasons[decision].Reason; got != brainknowledge.ReasonRelevant {
		t.Errorf("reason for the decision note = %q, want %q (it must win by relevance, not ride in on the recent tail)", got, brainknowledge.ReasonRelevant)
	}
}

// The native runtime gets the same selection as a daemon claim, rendered
// compactly into its brief and recorded as injected through its own channel.
func TestNativeBriefCarriesTheSelectedWorkspaceKnowledge(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("native-brain-%d", suffix), fmt.Sprintf("native-brain-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("native-brain-%d", suffix), fmt.Sprintf("native-brain-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	runtimeID := fx.Runtime(t, "native", testutil.Cols{"runtime_mode": "native", "daemon_id": "native", "provider": "native"})
	agentID := fx.Agent(t, "Native worker", runtimeID)
	queries := db.New(pool)

	bf := brainInjectFixture{svc: &TaskService{Queries: queries}, fx: fx, wsID: ws}
	answering := bf.note(t, "Procédure de déploiement en production",
		"Le déploiement passe par un tag Git signé, jamais depuis une branche. "+strings.Repeat("détail ", 400), false, 400)
	for i := 0; i < 10; i++ {
		bf.note(t, fmt.Sprintf("Compte rendu de réunion %d", i), strings.Repeat("rien à retenir ", 300), false, i)
	}
	// Pinned, so it is always in the brief regardless of relevance: proves a
	// non-fact kind still gets its "[kind]" heading prefix while the plain
	// (default kind=fact) notes above do not.
	fx.Insert(t, "workspace_note", testutil.Cols{
		"id":           testutil.Raw("gen_random_uuid()"),
		"workspace_id": ws, "title": "Rollback avant tag suivant",
		"content": "Décision: on ne revert jamais un tag déployé.",
		"pinned":  true, "kind": "decision",
	})

	issueID := fx.Issue(t, "Le déploiement échoue depuis une branche", testutil.Cols{
		"description": "Le pipeline a refusé le build lancé depuis une branche.",
	})
	taskID := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})
	fx.Cleanup(t, `DELETE FROM workspace_note_usage WHERE task_id = $1`, taskID)

	agent, err := queries.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	taskRow, err := queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	tasks := NewTaskService(queries, pool, nil, events.New())
	svc := NewNativeAgentService(queries, tasks, NewIssueService(queries, pool, events.New(), nil, tasks), &scriptedNativeLLM{}, events.New())

	brief, _, err := svc.nativeBriefForTask(ctx, taskRow, agent)
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	block, ok := cutWorkspaceKnowledgeBlock(brief)
	if !ok {
		t.Fatalf("native brief carries no Workspace Knowledge block:\n%s", brief)
	}
	if !strings.Contains(block, "Procédure de déploiement en production") {
		t.Errorf("the answering note is not in the block:\n%s", block)
	}
	// JEF-415 / B04: fact is the default, freeform kind and gets no "[kind]"
	// heading prefix (byte-identical to a pre-kinds brief); a non-fact kind
	// still does.
	if strings.Contains(block, "## [fact]") {
		t.Errorf("a fact note carries a redundant [fact] prefix:\n%s", block)
	}
	if !strings.Contains(block, "## [decision] Rollback avant tag suivant") {
		t.Errorf("the pinned decision note lost its [decision] prefix:\n%s", block)
	}
	if len(block) > nativeBriefNoteBytes+512 {
		t.Errorf("knowledge block is %d bytes, over the %d budget", len(block), nativeBriefNoteBytes)
	}
	if n := strings.Count(block, "\n## "); n > nativeBriefNoteLimit {
		t.Errorf("knowledge block holds %d notes, want at most %d", n, nativeBriefNoteLimit)
	}
	// Workspace records reach a native run only inside a fence.
	if !strings.Contains(brief, "<data workspace knowledge>") {
		t.Errorf("the knowledge block is not fenced as a record:\n%s", brief)
	}

	injected := fx.Count(t, `SELECT count(*) FROM workspace_note_usage WHERE task_id = $1 AND kind = 'injected' AND channel = 'native_brief'`, taskID)
	if injected == 0 || injected > nativeBriefNoteLimit {
		t.Errorf("recorded %d native_brief injections, want between 1 and %d", injected, nativeBriefNoteLimit)
	}
	if fx.Count(t, `SELECT count(*) FROM workspace_note_usage WHERE task_id = $1 AND note_id = $2`, taskID, answering) != 1 {
		t.Errorf("the note written into the brief is not recorded as injected")
	}
}

// An empty Brain leaves the native brief byte-identical to what it was.
func TestNativeBriefUnchangedByAnEmptyBrain(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	user := bootstrap.User(t, fmt.Sprintf("native-nobrain-%d", suffix), fmt.Sprintf("native-nobrain-%d@example.com", suffix))
	ws := bootstrap.Workspace(t, fmt.Sprintf("native-nobrain-%d", suffix), fmt.Sprintf("native-nobrain-%d", suffix))
	fx := testutil.New(pool, ws, user)
	fx.Member(t, ws, user, "owner")
	runtimeID := fx.Runtime(t, "native", testutil.Cols{"runtime_mode": "native", "daemon_id": "native", "provider": "native"})
	agentID := fx.Agent(t, "Native worker", runtimeID)
	queries := db.New(pool)
	issueID := fx.Issue(t, "Nothing to know", testutil.Cols{"description": "plain issue"})
	taskID := fx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})

	agent, err := queries.GetAgent(ctx, util.MustParseUUID(agentID))
	if err != nil {
		t.Fatalf("get agent: %v", err)
	}
	taskRow, err := queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	tasks := NewTaskService(queries, pool, nil, events.New())
	svc := NewNativeAgentService(queries, tasks, NewIssueService(queries, pool, events.New(), nil, tasks), &scriptedNativeLLM{}, events.New())

	brief, _, err := svc.nativeBriefForTask(ctx, taskRow, agent)
	if err != nil {
		t.Fatalf("brief: %v", err)
	}
	if strings.Contains(brief, "Workspace Knowledge") {
		t.Errorf("an empty Brain still added a knowledge section:\n%s", brief)
	}
	if fx.Count(t, `SELECT count(*) FROM workspace_note_usage WHERE task_id = $1`, taskID) != 0 {
		t.Errorf("an empty Brain recorded usage")
	}
}

func cutWorkspaceKnowledgeBlock(brief string) (string, bool) {
	_, after, ok := strings.Cut(brief, "Workspace Knowledge")
	if !ok {
		return "", false
	}
	return after, true
}
