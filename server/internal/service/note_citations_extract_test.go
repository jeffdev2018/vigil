package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// citationExtractionFixture reuses the agent-memory-extraction workspace/
// user/agent/issue and adds the notes a citation test needs: one live note in
// the run's own workspace, one archived there, and one that lives in a
// different workspace entirely.
type citationExtractionFixture struct {
	agentMemoryExtractionFixture
	validNoteID    string
	archivedNoteID string
	foreignNoteID  string
	nonexistentID  string
}

func seedCitationExtractionFixture(t *testing.T) (citationExtractionFixture, *pgxpool.Pool) {
	t.Helper()
	fx, pool := seedAgentMemoryExtractionFixture(t)
	wsFx := testutil.New(pool, fx.workspaceID, fx.userID)

	suffix := time.Now().UnixNano()
	bootstrap := testutil.New(pool, "", "")
	otherUser := bootstrap.User(t, fmt.Sprintf("citation-other-%d", suffix), fmt.Sprintf("citation-other-%d@example.com", suffix))
	otherWs := bootstrap.Workspace(t, fmt.Sprintf("citation-other-%d", suffix), fmt.Sprintf("citation-other-%d", suffix))
	otherFx := testutil.New(pool, otherWs, otherUser)

	valid := wsFx.Insert(t, "workspace_note", testutil.Cols{
		"id": testutil.Raw("gen_random_uuid()"), "workspace_id": fx.workspaceID,
		"title": "Deploy procedure", "content": "Push a signed tag.",
	})
	archived := wsFx.Insert(t, "workspace_note", testutil.Cols{
		"id": testutil.Raw("gen_random_uuid()"), "workspace_id": fx.workspaceID,
		"title": "Old procedure", "content": "stale.", "archived_at": testutil.Raw("now()"),
	})
	foreign := otherFx.Insert(t, "workspace_note", testutil.Cols{
		"id": testutil.Raw("gen_random_uuid()"), "workspace_id": otherWs,
		"title": "Someone else's note", "content": "elsewhere.",
	})

	return citationExtractionFixture{
		agentMemoryExtractionFixture: fx,
		validNoteID:                  valid,
		archivedNoteID:               archived,
		foreignNoteID:                foreign,
		nonexistentID:                "99999999-9999-4999-8999-999999999999",
	}, pool
}

// citationText builds the run's text: cites the valid note plus three that
// must be rejected (archived, another workspace's, and one that does not
// exist at all).
func (f citationExtractionFixture) citationText() string {
	return fmt.Sprintf(
		"Per [Deploy procedure](mention://note/%s), tag before you push.\n"+
			"Also see [Old procedure](mention://note/%s), [elsewhere](mention://note/%s) and [nowhere](mention://note/%s).",
		f.validNoteID, f.archivedNoteID, f.foreignNoteID, f.nonexistentID)
}

func (f citationExtractionFixture) citedRows(t *testing.T, pool *pgxpool.Pool, taskID string) []struct {
	noteID  string
	channel string
} {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT note_id::text, channel FROM workspace_note_usage WHERE task_id = $1 AND kind = 'cited' ORDER BY note_id`, taskID)
	if err != nil {
		t.Fatalf("list cited rows: %v", err)
	}
	defer rows.Close()
	var out []struct {
		noteID  string
		channel string
	}
	for rows.Next() {
		var r struct {
			noteID  string
			channel string
		}
		if err := rows.Scan(&r.noteID, &r.channel); err != nil {
			t.Fatalf("scan cited row: %v", err)
		}
		out = append(out, r)
	}
	return out
}

// The output alone carries the valid citation; the other three ids in it are
// rejected (this test puts them in a comment instead, see below), so this
// covers the "cite from the final output" half of the contract.
func TestExtractNoteCitationsFromOutput(t *testing.T) {
	fx, pool := seedCitationExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed",
		fmt.Sprintf("Per [Deploy procedure](mention://note/%s), tag before you push.", fx.validNoteID))
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM workspace_note_usage WHERE task_id = $1`, taskID) })

	svc := &TaskService{Queries: db.New(pool), TxStarter: pool}
	if err := svc.ExtractNoteCitationsForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("extract: %v", err)
	}

	rows := fx.citedRows(t, pool, taskID)
	if len(rows) != 1 || rows[0].noteID != fx.validNoteID || rows[0].channel != "citation" {
		t.Fatalf("cited rows = %+v, want exactly the valid note with channel=citation", rows)
	}
}

// The run's comments are scanned too, and only the valid citation among the
// four (archived, foreign-workspace, nonexistent, valid) is recorded. A
// second pass over the same task (event redelivery) inserts nothing new.
func TestExtractNoteCitationsFromCommentsRejectsInvalidOnesAndIsIdempotent(t *testing.T) {
	fx, pool := seedCitationExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed", "Done. No notes cited in the output itself.")
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM workspace_note_usage WHERE task_id = $1`, taskID) })

	wsFx := testutil.New(pool, fx.workspaceID, fx.userID)
	wsFx.Comment(t, fx.issueID, fx.citationText(), testutil.Cols{
		"author_type": "agent", "author_id": fx.agentID, "source_task_id": taskID,
	})

	svc := &TaskService{Queries: db.New(pool), TxStarter: pool}
	if err := svc.ExtractNoteCitationsForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("extract: %v", err)
	}
	rows := fx.citedRows(t, pool, taskID)
	if len(rows) != 1 || rows[0].noteID != fx.validNoteID {
		t.Fatalf("cited rows = %+v, want exactly the valid note", rows)
	}

	// Redelivery: the same completion event fires the pass again.
	if err := svc.ExtractNoteCitationsForTask(context.Background(), util.MustParseUUID(taskID)); err != nil {
		t.Fatalf("extract again: %v", err)
	}
	if rows := fx.citedRows(t, pool, taskID); len(rows) != 1 {
		t.Fatalf("redelivery duplicated citations: %+v", rows)
	}
}

// TestNoteCitationExtractionEventWiring drives the async path end to end: a
// task:completed event on the bus must land a 'cited' row without the
// publisher waiting for the pass.
func TestNoteCitationExtractionEventWiring(t *testing.T) {
	fx, pool := seedCitationExtractionFixture(t)
	taskID := fx.seedTerminalTask(t, pool, "completed",
		fmt.Sprintf("Per [Deploy procedure](mention://note/%s), tag before you push.", fx.validNoteID))
	t.Cleanup(func() { pool.Exec(context.Background(), `DELETE FROM workspace_note_usage WHERE task_id = $1`, taskID) })

	bus := events.New()
	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: bus}
	svc.SubscribeNoteCitationExtraction(bus)

	bus.Publish(events.Event{
		Type:        protocol.EventTaskCompleted,
		WorkspaceID: fx.workspaceID,
		ActorType:   "system",
		Payload: map[string]any{
			"task_id":  taskID,
			"agent_id": fx.agentID,
			"status":   "completed",
		},
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		if rows := fx.citedRows(t, pool, taskID); len(rows) == 1 {
			if rows[0].noteID != fx.validNoteID {
				t.Fatalf("cited note = %s, want %s", rows[0].noteID, fx.validNoteID)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for the citation extraction pass to write its row")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
