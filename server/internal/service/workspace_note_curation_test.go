package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type curatedNote struct {
	title      string
	tags       []string
	archived   bool
	mergedInto string
}

// seedBrainNote inserts one live note in the given workspace and returns its id.
func seedBrainNote(t *testing.T, pool *pgxpool.Pool, workspaceID, title, content string, tags []string, pinned bool) string {
	t.Helper()
	id := uuid.NewString()
	if tags == nil {
		tags = []string{}
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO workspace_note (id, workspace_id, title, content, tags, source, pinned, created_by_type)
		VALUES ($1, $2, $3, $4, $5, 'manual', $6, 'member')`,
		id, workspaceID, title, content, tags, pinned); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM workspace_note WHERE id = $1`, id)
	})
	return id
}

func readBrainNote(t *testing.T, pool *pgxpool.Pool, id string) curatedNote {
	t.Helper()
	var got curatedNote
	var archivedAt *time.Time
	var mergedInto *string
	if err := pool.QueryRow(context.Background(), `
		SELECT title, tags, archived_at, merged_into::text FROM workspace_note WHERE id = $1`, id).
		Scan(&got.title, &got.tags, &archivedAt, &mergedInto); err != nil {
		t.Fatalf("read note: %v", err)
	}
	got.archived = archivedAt != nil
	if mergedInto != nil {
		got.mergedInto = *mergedInto
	}
	return got
}

func brainCurationService(pool *pgxpool.Pool, client WorkspaceNoteCurationLLM) *TaskService {
	return &TaskService{Queries: db.New(pool), TxStarter: pool, BrainCuration: client}
}

func TestBrainCurationAppliesThePlan(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	workspaceID := uuid.NewString()

	survivor := seedBrainNote(t, pool, workspaceID, "Deploys go through the release tag",
		"Push v0.x.x on main; release.yml publishes the binaries and the Homebrew tap.", []string{"deploy"}, false)
	duplicate := seedBrainNote(t, pool, workspaceID, "Releases", "Push a v0.x.x tag.", []string{"Release"}, false)
	vague := seedBrainNote(t, pool, workspaceID, "Pooling", "pgbouncer sits in front of Postgres on 6432.", []string{"db"}, false)
	stale := seedBrainNote(t, pool, workspaceID, "The build is red", "CI is failing on main today.", nil, false)

	plan := `{
      "merge":[{"into":"` + survivor + `","from":["` + duplicate + `"]}],
      "retitle":[{"id":"` + vague + `","title":"Postgres runs behind pgbouncer on 6432"}],
      "tag":[{"id":"` + vague + `","tags":["Db","infra","db"]}],
      "archive":[{"id":"` + stale + `","reason":"a status report, not durable knowledge"}]
    }`
	svc := brainCurationService(pool, stubMemoryLLM(t, plan))

	applied, err := svc.curateWorkspaceBrain(context.Background(), util.MustParseUUID(workspaceID), time.Now().UTC())
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	if applied != 4 {
		t.Fatalf("applied = %d, want 4 (one merge, one retitle, one tag, one archive)", applied)
	}

	// A merged source is archived, never deleted, and points at the note that
	// now carries the fact — that is what makes a wrong merge recoverable.
	dup := readBrainNote(t, pool, duplicate)
	if !dup.archived || dup.mergedInto != survivor {
		t.Errorf("merged source: archived=%v merged_into=%q, want archived pointing at %s", dup.archived, dup.mergedInto, survivor)
	}
	if kept := readBrainNote(t, pool, survivor); kept.archived {
		t.Error("the merge target was archived")
	}

	updated := readBrainNote(t, pool, vague)
	if updated.title != "Postgres runs behind pgbouncer on 6432" {
		t.Errorf("title = %q, want the retitled one", updated.title)
	}
	// Tags land normalized exactly as a human-typed set would: lowercased,
	// de-duplicated, sorted.
	if len(updated.tags) != 2 || updated.tags[0] != "db" || updated.tags[1] != "infra" {
		t.Errorf("tags = %v, want [db infra]", updated.tags)
	}

	if got := readBrainNote(t, pool, stale); !got.archived {
		t.Error("the stale note was not archived")
	}
}

// Ids the model invented, or ids belonging to another workspace, must be
// dropped before they reach a write.
func TestBrainCurationIgnoresUnknownIDs(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	workspaceID := uuid.NewString()
	otherWorkspaceID := uuid.NewString()

	mine := seedBrainNote(t, pool, workspaceID, "Mine one", "body", nil, false)
	seedBrainNote(t, pool, workspaceID, "Mine two", "body", nil, false)
	foreign := seedBrainNote(t, pool, otherWorkspaceID, "Not mine", "body", nil, false)

	plan := `{"archive":[{"id":"` + foreign + `","reason":"x"},{"id":"` + uuid.NewString() + `","reason":"x"}],
	          "retitle":[{"id":"` + mine + `","title":"Mine one"}]}`
	svc := brainCurationService(pool, stubMemoryLLM(t, plan))

	applied, err := svc.curateWorkspaceBrain(context.Background(), util.MustParseUUID(workspaceID), time.Now().UTC())
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	// The retitle is a no-op (same title), so nothing at all should apply.
	if applied != 0 {
		t.Fatalf("applied = %d, want 0", applied)
	}
	if got := readBrainNote(t, pool, foreign); got.archived {
		t.Error("a note from another workspace was archived")
	}
}

// One edit is somebody fixing a typo. The pass costs tokens, so it only runs
// on a Brain that actually moved.
func TestBrainCurationSkipsAWorkspaceThatBarelyChanged(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	workspaceID := uuid.NewString()
	seedBrainNote(t, pool, workspaceID, "Only note", "body", nil, false)

	svc := brainCurationService(pool, stubMemoryLLM(t, `{"archive":[{"id":"whatever","reason":"x"}]}`))
	applied, err := svc.curateWorkspaceBrain(context.Background(), util.MustParseUUID(workspaceID), time.Now().UTC())
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	if applied != 0 {
		t.Fatalf("applied = %d, want 0 for a workspace with one changed note", applied)
	}
}

// A plan the model got wildly wrong is refused wholesale rather than applied
// halfway: 200 edits is a misread corpus, not a workspace that needed them.
func TestBrainCurationRefusesAnOversizedPlan(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	workspaceID := uuid.NewString()
	var ids []string
	for i := 0; i < 3; i++ {
		ids = append(ids, seedBrainNote(t, pool, workspaceID, "Note", "body", nil, false))
	}

	plan := `{"archive":[`
	for i := 0; i < brainCurationMaxPlanOps+1; i++ {
		if i > 0 {
			plan += ","
		}
		plan += `{"id":"` + ids[i%len(ids)] + `","reason":"x"}`
	}
	plan += `]}`

	svc := brainCurationService(pool, stubMemoryLLM(t, plan))
	if _, err := svc.curateWorkspaceBrain(context.Background(), util.MustParseUUID(workspaceID), time.Now().UTC()); err == nil {
		t.Fatal("an oversized plan was accepted")
	}
	if got := readBrainNote(t, pool, ids[0]); got.archived {
		t.Error("the refused plan still archived a note")
	}
}

// A deployment with no MULTICA_LLM_* configuration must pay nothing.
func TestBrainCurationIsANoOpWithoutAnLLM(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	svc := brainCurationService(pool, nil)
	applied, err := svc.CurateWorkspaceBrains(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatalf("curate: %v", err)
	}
	if applied != 0 {
		t.Fatalf("applied = %d, want 0 without an LLM", applied)
	}
}
