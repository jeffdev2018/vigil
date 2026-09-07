package handler

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// F26 acceptance 1: a merge on a linked repository starts exactly one wiki run,
// even when ten merges land in the same second.

// wikiDaemon installs the opt-in for a project: an active autopilot on it
// carrying an enabled webhook trigger labelled `code-wiki`, which is what the
// wiki DAEMON.md declares.
func wikiDaemon(t *testing.T, projectID string) string {
	t.Helper()
	agentID := dbfx.Agent(t, "wiki agent "+uuid.NewString()[:6], handlerTestRuntimeID(t))
	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           "Code wiki " + uuid.NewString()[:6],
		"description":     "Regenerate the code wiki",
		"status":          "active",
		"execution_mode":  "run_only",
		"assignee_type":   "agent",
		"assignee_id":     agentID,
		"project_id":      projectID,
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	dbfx.Insert(t, "autopilot_trigger", testutil.Cols{
		"autopilot_id":  autopilotID,
		"kind":          "webhook",
		"enabled":       true,
		"label":         "code-wiki",
		"webhook_token": "wt-" + uuid.NewString(),
	})
	dbfx.Cleanup(t, `DELETE FROM autopilot_run WHERE autopilot_id = $1`, autopilotID)
	return autopilotID
}

func TestMergeBurstStartsOneWikiRun(t *testing.T) {
	projectID, resourceID := wikiFixture(t, "https://github.com/acme/burst")
	autopilotID := wikiDaemon(t, projectID)
	wsUUID := parseUUID(testWorkspaceID)
	ctx := context.Background()

	// Ten merges land back to back, as they do when a queue drains.
	for i := 0; i < 10; i++ {
		testHandler.triggerCodeWikiForMergedPR(ctx, wsUUID, "acme", "burst", "sha-merge")
	}

	var builds int
	dbfx.QueryRow(t,
		`SELECT count(*) FROM code_wiki_snapshot WHERE project_resource_id = $1 AND state = 'building'`,
		resourceID).Scan(&builds)
	if builds != 1 {
		t.Fatalf("a burst of merges must hold exactly one build slot, got %d", builds)
	}

	var runs int
	dbfx.QueryRow(t, `SELECT count(*) FROM autopilot_run WHERE autopilot_id = $1`, autopilotID).Scan(&runs)
	if runs != 1 {
		t.Fatalf("a burst of merges must start exactly one wiki run, got %d", runs)
	}

	// Once the build finishes, the next merge starts the next run: the rule
	// collapses a burst, not the feature.
	dbfx.Exec(t, `UPDATE code_wiki_snapshot SET state = 'failed' WHERE project_resource_id = $1`, resourceID)
	testHandler.triggerCodeWikiForMergedPR(ctx, wsUUID, "acme", "burst", "sha-later")
	dbfx.QueryRow(t, `SELECT count(*) FROM autopilot_run WHERE autopilot_id = $1`, autopilotID).Scan(&runs)
	if runs != 2 {
		t.Fatalf("a later merge must be able to start a run, got %d total", runs)
	}
}

func TestMergeOnAnUnrelatedRepoStartsNothing(t *testing.T) {
	projectID, resourceID := wikiFixture(t, "https://github.com/acme/watched")
	autopilotID := wikiDaemon(t, projectID)
	ctx := context.Background()

	testHandler.triggerCodeWikiForMergedPR(ctx, parseUUID(testWorkspaceID), "acme", "some-other-repo", "sha")

	var builds, runs int
	dbfx.QueryRow(t, `SELECT count(*) FROM code_wiki_snapshot WHERE project_resource_id = $1`, resourceID).Scan(&builds)
	dbfx.QueryRow(t, `SELECT count(*) FROM autopilot_run WHERE autopilot_id = $1`, autopilotID).Scan(&runs)
	if builds != 0 || runs != 0 {
		t.Fatalf("a merge on another repository must start nothing: %d builds, %d runs", builds, runs)
	}
}

func TestMergeWithoutTheWikiDaemonStartsNothing(t *testing.T) {
	// The feature is opt-in: a project that never installed the wiki daemon is
	// never charged for a run it did not ask for.
	projectID, resourceID := wikiFixture(t, "https://github.com/acme/no-daemon")
	_ = projectID
	testHandler.triggerCodeWikiForMergedPR(context.Background(), parseUUID(testWorkspaceID), "acme", "no-daemon", "sha")

	var builds int
	dbfx.QueryRow(t, `SELECT count(*) FROM code_wiki_snapshot WHERE project_resource_id = $1`, resourceID).Scan(&builds)
	if builds != 0 {
		t.Fatalf("no wiki daemon means no build, got %d", builds)
	}
}

func TestWikiDaemonMustCarryTheTriggerLabel(t *testing.T) {
	projectID, resourceID := wikiFixture(t, "https://github.com/acme/mislabelled")
	agentID := dbfx.Agent(t, "wiki agent "+uuid.NewString()[:6], handlerTestRuntimeID(t))
	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           "Something else " + uuid.NewString()[:6],
		"description":     "Not the wiki",
		"status":          "active",
		"execution_mode":  "run_only",
		"assignee_type":   "agent",
		"assignee_id":     agentID,
		"project_id":      projectID,
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	dbfx.Insert(t, "autopilot_trigger", testutil.Cols{
		"autopilot_id":  autopilotID,
		"kind":          "webhook",
		"enabled":       true,
		"label":         "ci",
		"webhook_token": "wt-" + uuid.NewString(),
	})
	dbfx.Cleanup(t, `DELETE FROM autopilot_run WHERE autopilot_id = $1`, autopilotID)

	testHandler.triggerCodeWikiForMergedPR(context.Background(), parseUUID(testWorkspaceID), "acme", "mislabelled", "sha")

	var runs int
	dbfx.QueryRow(t, `SELECT count(*) FROM autopilot_run WHERE autopilot_id = $1`, autopilotID).Scan(&runs)
	if runs != 0 {
		t.Fatalf("a merge must not dispatch an autopilot that is not the wiki daemon, got %d runs", runs)
	}
	var builds int
	dbfx.QueryRow(t, `SELECT count(*) FROM code_wiki_snapshot WHERE project_resource_id = $1`, resourceID).Scan(&builds)
	if builds != 0 {
		t.Fatalf("no build should have been claimed, got %d", builds)
	}
}
