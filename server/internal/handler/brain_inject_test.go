package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/brainknowledge"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

type claimBrainResponse struct {
	Task *struct {
		ID             string `json:"id"`
		WorkspaceNotes []struct {
			ID     string  `json:"id"`
			Title  string  `json:"title"`
			Reason string  `json:"reason"`
			Score  float64 `json:"score"`
		} `json:"workspace_notes"`
		WorkspaceNotesQuery string                     `json:"workspace_notes_query"`
		MemoryContext       *service.TaskMemoryContext `json:"memory_context"`
	} `json:"task"`
}

func claimForBrain(t *testing.T, runtimeID string) claimBrainResponse {
	t.Helper()
	req := withURLParam(newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "brain-inject-claim"), "runtimeId", runtimeID)
	var resp claimBrainResponse
	testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK).JSON(&resp)
	if resp.Task == nil {
		t.Fatal("claim returned no task")
	}
	return resp
}

// An issue-bound claim selects the Brain by relevance to its own ticket
// (JEF-414): the old note that answers it arrives, the fresher off-topic ones
// mostly do not, and everything the response says was injected is recorded as
// injected.
func TestClaimSelectsWorkspaceNotesByRelevance(t *testing.T) {
	agentID := agentMemoryFixture(t, "brain-inject-claim-agent")
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)

	answering := usageNote(t, testWorkspaceID, "Procédure de déploiement en production",
		"Le déploiement passe par un tag Git signé, jamais depuis une branche.",
		testutil.Cols{"updated_at": testutil.Raw("now() - interval '400 days'")})
	pinned := usageNote(t, testWorkspaceID, "Contacts d'astreinte", "Appeler l'astreinte ops.",
		testutil.Cols{"pinned": true, "updated_at": testutil.Raw("now() - interval '500 days'")})
	var offTopic []string
	for i := 0; i < 9; i++ {
		offTopic = append(offTopic, usageNote(t, testWorkspaceID,
			"Compte rendu de réunion "+string(rune('A'+i)), "Réunion hebdomadaire, rien à retenir.", nil))
	}

	issueID := dbfx.Issue(t, "Le déploiement échoue depuis une branche", testutil.Cols{
		"description": "Un agent a poussé depuis une branche et le pipeline a refusé le build.",
	})
	taskID := dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "issue_id": issueID})
	dbfx.Cleanup(t, `DELETE FROM workspace_note_usage WHERE task_id = $1`, taskID)

	resp := claimForBrain(t, runtimeID)
	if resp.Task.ID != taskID {
		t.Fatalf("claimed %s, want %s", resp.Task.ID, taskID)
	}
	byID := map[string]string{}
	var sent []string
	for _, n := range resp.Task.WorkspaceNotes {
		sent = append(sent, n.ID)
		byID[n.ID] = n.Reason
	}
	if byID[answering] != brainknowledge.ReasonRelevant {
		t.Fatalf("the answering note is %q in %v; relevance did not reach the claim", byID[answering], sent)
	}
	if byID[pinned] != brainknowledge.ReasonPinned || sent[0] != pinned {
		t.Errorf("workspace_notes = %v, want the pinned note first and labelled pinned", sent)
	}
	for _, n := range resp.Task.WorkspaceNotes {
		if n.Reason == brainknowledge.ReasonRelevant && n.Score <= 0 {
			t.Errorf("note %q is relevant with score %v; the index needs the real score", n.Title, n.Score)
		}
	}
	riding := 0
	for _, id := range offTopic {
		if byID[id] != "" {
			riding++
		}
	}
	if riding > 4 {
		t.Errorf("%d off-topic notes rode along, want at most the bounded recent tail", riding)
	}

	// The query the notes were found with rides with them, so the run's index
	// can name it.
	if !strings.Contains(resp.Task.WorkspaceNotesQuery, "déploiement") {
		t.Errorf("workspace_notes_query = %q, want the ticket's own subject", resp.Task.WorkspaceNotesQuery)
	}
	if strings.ContainsAny(resp.Task.WorkspaceNotesQuery, "\n\r") {
		t.Errorf("workspace_notes_query is multi-line (%q); the index prints it inside a list item", resp.Task.WorkspaceNotesQuery)
	}
	if n := len([]rune(resp.Task.WorkspaceNotesQuery)); n > brainClaimQueryWireLimit {
		t.Errorf("workspace_notes_query is %d runes, over the %d wire limit", n, brainClaimQueryWireLimit)
	}

	// memory_context and the usage rows are exactly the notes on the wire.
	var stored service.TaskMemoryContext
	var raw []byte
	dbfx.QueryRow(t, `SELECT memory_context FROM agent_task_queue WHERE id = $1`, taskID).Scan(&raw)
	if err := json.Unmarshal(raw, &stored); err != nil {
		t.Fatal(err)
	}
	if stored.WorkspaceNotesStatus != "loaded" || len(stored.WorkspaceNotes) != len(sent) {
		t.Fatalf("memory_context = %+v, want %d loaded notes", stored, len(sent))
	}
	for i, v := range stored.WorkspaceNotes {
		if v.ID != sent[i] {
			t.Fatalf("memory_context.workspace_notes[%d] = %s, want %s", i, v.ID, sent[i])
		}
	}
	if got := usageRows(t, `task_id = $1 AND kind = 'injected' AND channel = 'daemon_brief'`, taskID); got != len(sent) {
		t.Fatalf("injected rows = %d, want %d", got, len(sent))
	}
	for _, id := range offTopic {
		if byID[id] == "" && usageRows(t, `task_id = $1 AND note_id = $2`, taskID, id) != 0 {
			t.Errorf("note %s was not sent but has usage rows", id)
		}
	}
}

// A claim with a Brain but no searchable subject keeps the selection it always
// had, so a chat or autopilot run is not punished for having no ticket.
func TestClaimWithoutSubjectKeepsPinnedAndRecent(t *testing.T) {
	agentID := agentMemoryFixture(t, "brain-inject-nosubject-agent")
	var runtimeID string
	dbfx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
	pinned := usageNote(t, testWorkspaceID, "Contacts d'astreinte", "Appeler l'astreinte ops.", testutil.Cols{"pinned": true})
	recent := usageNote(t, testWorkspaceID, "Note fraîche", "Quelque chose de récent.", nil)

	dbfx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID})
	resp := claimForBrain(t, runtimeID)
	var sent []string
	for _, n := range resp.Task.WorkspaceNotes {
		sent = append(sent, n.ID)
		if n.Reason == brainknowledge.ReasonRelevant {
			t.Errorf("note %q is labelled relevant although the run had no subject", n.Title)
		}
	}
	if len(sent) == 0 || sent[0] != pinned {
		t.Fatalf("workspace_notes = %v, want the pinned note first", sent)
	}
	found := false
	for _, id := range sent {
		if id == recent {
			found = true
		}
	}
	if !found {
		t.Errorf("recent note %s missing from %v", recent, sent)
	}
	if resp.Task.WorkspaceNotesQuery != "" {
		t.Errorf("workspace_notes_query = %q, want empty with no subject", resp.Task.WorkspaceNotesQuery)
	}
}
