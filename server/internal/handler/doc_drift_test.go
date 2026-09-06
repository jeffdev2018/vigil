package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Agent context drift detection (K56): a read-only scan reports the sections of
// the agent context document that stopped describing the repository, and the
// server turns each document into a proposal reviewed as a DRAFT pull request.
//
// The settings shape (defaults, path rules, due-ness) is covered canonically in
// service/doc_drift_test.go; this file covers the endpoints, the completion
// hooks and the one-open-proposal-per-document invariant.

type docDriftSettingsEnvelope struct {
	service.DocDriftSettings
	Repos []docDriftRepoStatus `json:"repos"`
}

type docDriftProposalListEnvelope struct {
	Proposals []DocDriftProposalResponse `json:"proposals"`
}

type docDriftProposalEnvelope struct {
	Proposal DocDriftProposalResponse `json:"proposal"`
}

const docDriftTestRepo = "https://example.test/acme/app.git"

func docDriftCleanup(t *testing.T) {
	t.Helper()
	// IssueService.Create numbers from workspace.issue_counter, which dbfx.Issue
	// deliberately does not move (see org_test.go).
	syncIssueCounter(t)
	t.Cleanup(func() {
		// NOT t.Context(): Go cancels it just before cleanups run.
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM doc_drift_proposal WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1 AND origin_type = 'doc_drift')`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM issue WHERE workspace_id = $1 AND origin_type = 'doc_drift'`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM inbox_item WHERE workspace_id = $1 AND type = 'doc_drift_report'`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM repo_index_chunk WHERE workspace_id = $1`, testWorkspaceID)
		testPool.Exec(ctx, `DELETE FROM audit_log_entry WHERE workspace_id = $1 AND action LIKE 'doc_drift.%'`, testWorkspaceID)
		testPool.Exec(ctx, `UPDATE workspace SET settings = settings - 'doc_drift' - 'repo_index' WHERE id = $1`, testWorkspaceID)
	})
}

// indexRepoAt fakes what the daemon's index pass leaves behind: the repository
// opted in, and one chunk carrying the commit of the default branch. That
// commit is the whole drift trigger, so moving it is how a test moves the
// branch.
func indexRepoAt(t *testing.T, repo, commit string) {
	t.Helper()
	dbfx.Exec(t, `UPDATE workspace SET settings = jsonb_set(COALESCE(settings, '{}'::jsonb), '{repo_index}', $2::jsonb, true) WHERE id = $1`,
		testWorkspaceID, `{"`+repo+`":{"enabled":true}}`)
	dbfx.Exec(t, `DELETE FROM repo_index_chunk WHERE workspace_id = $1 AND repo_identifier = $2`, testWorkspaceID, repo)
	dbfx.Exec(t, `INSERT INTO repo_index_chunk (workspace_id, repo_identifier, file_path, content, content_hash, indexed_commit)
		VALUES ($1, $2, 'CLAUDE.md', 'context document', 'hash', $3)`, testWorkspaceID, repo, commit)
}

func putDocDriftSettings(t *testing.T, body map[string]any) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.PutDocDriftSettings, newRequest(http.MethodPut, "/api/doc-drift/settings", body))
}

func getDocDriftSettings(t *testing.T) docDriftSettingsEnvelope {
	t.Helper()
	var out docDriftSettingsEnvelope
	testutil.Call(t, testHandler.GetDocDriftSettings, newRequest(http.MethodGet, "/api/doc-drift/settings", nil)).
		Want(http.StatusOK).JSON(&out)
	return out
}

func checkDocDrift(t *testing.T, repo string) *testutil.Response {
	t.Helper()
	return testutil.Call(t, testHandler.CheckDocDrift,
		newRequest(http.MethodPost, "/api/doc-drift/check", map[string]any{"repo_identifier": repo}))
}

func listDocDriftProposals(t *testing.T) []DocDriftProposalResponse {
	t.Helper()
	var out docDriftProposalListEnvelope
	testutil.Call(t, testHandler.ListDocDriftProposals, newRequest(http.MethodGet, "/api/doc-drift/proposals", nil)).
		Want(http.StatusOK).JSON(&out)
	return out.Proposals
}

// completeDocDriftTask drives the daemon completion callback, which is where
// both drift hooks hang. prURL is what the pull-request run reports back.
func completeDocDriftTask(t *testing.T, taskID, output, prURL string) *httptest.ResponseRecorder {
	t.Helper()
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, taskID)
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": output, "pr_url": prURL}, testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("complete task %s: %d %s", taskID, w.Code, w.Body.String())
	}
	return w
}

// startedScanTask returns the run the last check enqueued for a repository.
func startedScanTask(t *testing.T, repo string) string {
	t.Helper()
	return getDocDriftSettings(t).ScanTasks[repo]
}

func docDriftAgent(t *testing.T) string {
	t.Helper()
	return dbfx.Agent(t, "context agent "+uuid.NewString()[:6], handlerTestRuntimeID(t))
}

// docDriftFenceFor builds the report contract for the given documents.
func docDriftFenceFor(t *testing.T, proposals []DocDriftProposalReport) string {
	t.Helper()
	raw, err := json.Marshal(DocDriftReport{Proposals: proposals})
	if err != nil {
		t.Fatal(err)
	}
	return "Checked the repository.\n\n```doc_drift\n" + string(raw) + "\n```\n"
}

func TestDocDriftSettingsAreAdminOnlyAndValidated(t *testing.T) {
	docDriftCleanup(t)
	agentID := docDriftAgent(t)

	// Disabled by default, with the documented defaults.
	out := getDocDriftSettings(t)
	if out.Enabled || len(out.Docs) != len(service.DocDriftDefaultDocs) || !out.OpenPR {
		t.Fatalf("defaults: %+v", out.DocDriftSettings)
	}

	// A plain member reads the settings but cannot write them, check, dismiss
	// or open a pull request.
	member := dbfx.User(t, "doc drift member", "doc-drift-"+uuid.NewString()[:6]+"@example.test")
	dbfx.Member(t, testWorkspaceID, member, "member")
	testutil.Call(t, testHandler.GetDocDriftSettings, newRequestAs(member, http.MethodGet, "/api/doc-drift/settings", nil)).Want(http.StatusOK)
	testutil.Call(t, testHandler.ListDocDriftProposals, newRequestAs(member, http.MethodGet, "/api/doc-drift/proposals", nil)).Want(http.StatusOK)
	testutil.Call(t, testHandler.PutDocDriftSettings, newRequestAs(member, http.MethodPut, "/api/doc-drift/settings",
		map[string]any{"enabled": true, "agent_id": agentID})).Want(http.StatusForbidden)
	testutil.Call(t, testHandler.CheckDocDrift, newRequestAs(member, http.MethodPost, "/api/doc-drift/check",
		map[string]any{"repo_identifier": docDriftTestRepo})).Want(http.StatusForbidden)

	// Validation: the agent must belong to this workspace, and a document path
	// must stay inside the checkout the run is given.
	putDocDriftSettings(t, map[string]any{"enabled": true}).Want(http.StatusBadRequest)
	putDocDriftSettings(t, map[string]any{"enabled": true, "agent_id": uuid.NewString()}).Want(http.StatusBadRequest)
	putDocDriftSettings(t, map[string]any{"enabled": true, "agent_id": agentID, "docs": []string{"../../../etc/passwd"}}).Want(http.StatusBadRequest)
	putDocDriftSettings(t, map[string]any{"enabled": true, "agent_id": agentID, "docs": []string{"/etc/passwd"}}).Want(http.StatusBadRequest)

	var saved docDriftSettingsEnvelope
	putDocDriftSettings(t, map[string]any{
		"enabled": true, "agent_id": agentID, "docs": []string{"AGENTS.md"}, "open_pr": false,
	}).Want(http.StatusOK).JSON(&saved)
	if !saved.Enabled || saved.AgentID != agentID || len(saved.Docs) != 1 || saved.Docs[0] != "AGENTS.md" || saved.OpenPR {
		t.Fatalf("saved settings: %+v", saved.DocDriftSettings)
	}

	// last_checked is server-owned: a client cannot rewrite it to force a check
	// on every tick.
	indexRepoAt(t, docDriftTestRepo, "commit-a")
	checkDocDrift(t, docDriftTestRepo).Want(http.StatusCreated)
	putDocDriftSettings(t, map[string]any{
		"enabled": true, "agent_id": agentID, "docs": []string{"AGENTS.md"},
		"last_checked": map[string]string{docDriftTestRepo: "rewritten"},
	}).Want(http.StatusOK).JSON(&saved)
	if saved.LastChecked[docDriftTestRepo] != "commit-a" {
		t.Fatalf("last_checked is server-owned, got %q", saved.LastChecked[docDriftTestRepo])
	}
}

func TestDocDriftCheckEnqueuesReadOnlyScanAndStampsTheCommit(t *testing.T) {
	docDriftCleanup(t)
	agentID := docDriftAgent(t)
	putDocDriftSettings(t, map[string]any{"enabled": false, "agent_id": agentID}).Want(http.StatusOK)
	indexRepoAt(t, docDriftTestRepo, "commit-a")

	// A manual check is allowed with the schedule off.
	var started struct {
		RepoIdentifier string `json:"repo_identifier"`
		TaskID         string `json:"task_id"`
	}
	checkDocDrift(t, docDriftTestRepo).Want(http.StatusCreated).JSON(&started)
	if started.TaskID == "" || started.RepoIdentifier != docDriftTestRepo {
		t.Fatalf("check started: %+v", started)
	}

	// The run hangs off the housekeeping issue and carries the read-only
	// contract, the documents and the repository.
	var brief, issueOrigin string
	dbfx.QueryRow(t, `SELECT COALESCE(a.handoff_note, ''), COALESCE(i.origin_type, '') FROM agent_task_queue a JOIN issue i ON i.id = a.issue_id WHERE a.id = $1`,
		started.TaskID).Scan(&brief, &issueOrigin)
	if issueOrigin != "doc_drift" {
		t.Fatalf("the scan run hangs off the housekeeping issue, got origin %q", issueOrigin)
	}
	for _, want := range []string{"doc_drift", "FORBIDDEN", "CLAUDE.md", docDriftTestRepo, "commit-a"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("the brief carries %q: %q", want, brief)
		}
	}

	// The commit is stamped at enqueue, so the same commit is never checked
	// twice by the scheduler even if this run crashes.
	settings := getDocDriftSettings(t)
	if settings.LastChecked[docDriftTestRepo] != "commit-a" {
		t.Fatalf("last_checked stamped at enqueue: %+v", settings.LastChecked)
	}
	if len(settings.Repos) != 1 || settings.Repos[0].Due || !settings.Repos[0].Scanning {
		t.Fatalf("repo status after the check: %+v", settings.Repos)
	}

	// One scan at a time per repository.
	checkDocDrift(t, docDriftTestRepo).Want(http.StatusConflict)

	// A settled scan releases the repository, even one that reported nothing.
	completeDocDriftTask(t, started.TaskID, "```doc_drift\n{\"proposals\":[],\"no_drift\":\"still accurate\"}\n```", "")
	if p := listDocDriftProposals(t); len(p) != 0 {
		t.Fatalf("a clean scan stores nothing: %+v", p)
	}
	checkDocDrift(t, docDriftTestRepo).Want(http.StatusCreated)
}

func TestDocDriftScanOpensOneDraftPerDocumentAndItsPullRequest(t *testing.T) {
	docDriftCleanup(t)
	agentID := docDriftAgent(t)
	putDocDriftSettings(t, map[string]any{"enabled": true, "agent_id": agentID, "open_pr": true}).Want(http.StatusOK)
	indexRepoAt(t, docDriftTestRepo, "commit-a")
	checkDocDrift(t, docDriftTestRepo).Want(http.StatusCreated)
	scanTask := startedScanTask(t, docDriftTestRepo)

	output := docDriftFenceFor(t, []DocDriftProposalReport{
		{DocPath: "CLAUDE.md", Summary: "Two commands moved.", Sections: []DocDriftSection{
			{Heading: "Commands", Reason: "`make serve` was renamed to `make server`", Patch: "-make serve\n+make server"},
		}},
		{DocPath: "AGENTS.md", Summary: "The package list is stale.", Sections: []DocDriftSection{
			{Heading: "Project Shape", Reason: "packages/legacy no longer exists", Patch: "-packages/legacy\n"},
		}},
		// Refused: a path that would leave the checkout is not trusted just
		// because a model produced it.
		{DocPath: "../../etc/passwd", Summary: "nope", Sections: []DocDriftSection{{Heading: "x", Reason: "y", Patch: "z"}}},
		// Refused: a proposal with no applicable section is not a proposal.
		{DocPath: "README.md", Summary: "vague feeling", Sections: []DocDriftSection{{Heading: "Intro", Reason: "reads oddly"}}},
	})
	completeDocDriftTask(t, scanTask, output, "")

	proposals := listDocDriftProposals(t)
	if len(proposals) != 2 {
		t.Fatalf("one proposal per drifted document, got %d: %+v", len(proposals), proposals)
	}
	byDoc := map[string]DocDriftProposalResponse{}
	for _, p := range proposals {
		byDoc[p.DocPath] = p
	}
	claude, ok := byDoc["CLAUDE.md"]
	if !ok {
		t.Fatalf("documents: %+v", byDoc)
	}
	if claude.Status != "draft" || claude.RepoIdentifier != docDriftTestRepo || claude.DetectedAtCommit != "commit-a" {
		t.Fatalf("proposal: %+v", claude)
	}
	if !strings.Contains(claude.DetectedDrift, "make serve") || !strings.Contains(claude.ProposedPatch, "+make server") {
		t.Fatalf("the proposal carries the summary and the patch: %+v", claude)
	}
	if claude.PRTaskID == nil || *claude.PRTaskID == "" {
		t.Fatalf("open_pr queues the pull request run: %+v", claude)
	}

	// The pull-request run is narrow by construction: one file, one branch, a
	// draft, and no commit to the default branch.
	var prBrief string
	dbfx.QueryRow(t, `SELECT COALESCE(handoff_note, '') FROM agent_task_queue WHERE id = $1`, *claude.PRTaskID).Scan(&prBrief)
	for _, want := range []string{"CLAUDE.md", docDriftBranchPrefix, docDriftPRTitle, "DRAFT", "DO NOT touch any other file"} {
		if !strings.Contains(prBrief, want) {
			t.Fatalf("the pull request brief carries %q: %q", want, prBrief)
		}
	}

	// Its completion with a URL moves the proposal in front of a human.
	completeDocDriftTask(t, *claude.PRTaskID, "Opened the draft.", "https://example.test/acme/app/pull/42")
	after := listDocDriftProposals(t)
	var settled DocDriftProposalResponse
	for _, p := range after {
		if p.ID == claude.ID {
			settled = p
		}
	}
	if settled.Status != "opened_pr" || settled.PullRequestURL != "https://example.test/acme/app/pull/42" {
		t.Fatalf("the reported URL settles the proposal: %+v", settled)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM inbox_item WHERE workspace_id = $1 AND type = 'doc_drift_report'`, testWorkspaceID); n == 0 {
		t.Fatalf("the admins hear about the draft pull request")
	}
}

// A document already under review accumulates: a second scan appends to the
// open proposal instead of racing it with a near-duplicate. The partial unique
// index is what makes that an invariant rather than a convention.
func TestDocDriftSecondScanMergesIntoTheOpenProposal(t *testing.T) {
	docDriftCleanup(t)
	agentID := docDriftAgent(t)
	putDocDriftSettings(t, map[string]any{"enabled": true, "agent_id": agentID, "open_pr": false}).Want(http.StatusOK)

	indexRepoAt(t, docDriftTestRepo, "commit-a")
	checkDocDrift(t, docDriftTestRepo).Want(http.StatusCreated)
	completeDocDriftTask(t, startedScanTask(t, docDriftTestRepo), docDriftFenceFor(t, []DocDriftProposalReport{
		{DocPath: "CLAUDE.md", Summary: "First round.", Sections: []DocDriftSection{
			{Heading: "Commands", Reason: "make serve is gone", Patch: "-make serve"},
		}},
	}), "")

	first := listDocDriftProposals(t)
	if len(first) != 1 {
		t.Fatalf("one proposal after the first scan: %+v", first)
	}

	indexRepoAt(t, docDriftTestRepo, "commit-b")
	checkDocDrift(t, docDriftTestRepo).Want(http.StatusCreated)
	completeDocDriftTask(t, startedScanTask(t, docDriftTestRepo), docDriftFenceFor(t, []DocDriftProposalReport{
		{DocPath: "CLAUDE.md", Summary: "Second round.", Sections: []DocDriftSection{
			{Heading: "Project Shape", Reason: "packages/legacy is gone", Patch: "-packages/legacy"},
		}},
	}), "")

	merged := listDocDriftProposals(t)
	if len(merged) != 1 || merged[0].ID != first[0].ID {
		t.Fatalf("the second scan merges into the open proposal: %+v", merged)
	}
	if !strings.Contains(merged[0].DetectedDrift, "make serve") || !strings.Contains(merged[0].DetectedDrift, "packages/legacy") {
		t.Fatalf("both rounds are on the merged proposal: %q", merged[0].DetectedDrift)
	}
	if !strings.Contains(merged[0].ProposedPatch, "-make serve") || !strings.Contains(merged[0].ProposedPatch, "-packages/legacy") {
		t.Fatalf("both patches are on the merged proposal: %q", merged[0].ProposedPatch)
	}
	if merged[0].DetectedAtCommit != "commit-b" {
		t.Fatalf("the merged proposal carries the newest commit, got %q", merged[0].DetectedAtCommit)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM doc_drift_proposal WHERE workspace_id = $1 AND doc_path = 'CLAUDE.md' AND status IN ('draft','opened_pr')`, testWorkspaceID); n != 1 {
		t.Fatalf("exactly one open proposal per document, got %d", n)
	}

	// With open_pr off the proposal waits for someone to press the button; the
	// button queues the same narrow run.
	if merged[0].PRTaskID != nil && *merged[0].PRTaskID != "" {
		t.Fatalf("open_pr is off: no pull request run was queued: %+v", merged[0])
	}
	openPR := func(user string) *testutil.Response {
		req := newRequest(http.MethodPost, "/api/doc-drift/proposals/"+merged[0].ID+"/open-pr", nil)
		if user != "" {
			req = newRequestAs(user, http.MethodPost, "/api/doc-drift/proposals/"+merged[0].ID+"/open-pr", nil)
		}
		return testutil.Call(t, testHandler.OpenDocDriftProposalPR, testutil.WithURLParams(req, "id", merged[0].ID))
	}
	reader := dbfx.User(t, "doc drift viewer", "doc-drift-v-"+uuid.NewString()[:6]+"@example.test")
	dbfx.Member(t, testWorkspaceID, reader, "member")
	openPR(reader).Want(http.StatusForbidden)

	var queued docDriftProposalEnvelope
	openPR("").Want(http.StatusCreated).JSON(&queued)
	if queued.Proposal.PRTaskID == nil || *queued.Proposal.PRTaskID == "" {
		t.Fatalf("open-pr queues the pull request run: %+v", queued.Proposal)
	}
	// And not a second one while it is in flight.
	openPR("").Want(http.StatusConflict)
}

// Dismissing is an answer, not a mute: the next scan that still sees the drift
// proposes it again, because the unique index only covers the open states.
func TestDocDriftDismissedProposalDoesNotBlockTheNextOne(t *testing.T) {
	docDriftCleanup(t)
	agentID := docDriftAgent(t)
	putDocDriftSettings(t, map[string]any{"enabled": true, "agent_id": agentID, "open_pr": false}).Want(http.StatusOK)

	report := docDriftFenceFor(t, []DocDriftProposalReport{
		{DocPath: "CLAUDE.md", Summary: "Commands moved.", Sections: []DocDriftSection{
			{Heading: "Commands", Reason: "make serve is gone", Patch: "-make serve"},
		}},
	})
	indexRepoAt(t, docDriftTestRepo, "commit-a")
	checkDocDrift(t, docDriftTestRepo).Want(http.StatusCreated)
	completeDocDriftTask(t, startedScanTask(t, docDriftTestRepo), report, "")

	first := listDocDriftProposals(t)
	if len(first) != 1 {
		t.Fatalf("one proposal: %+v", first)
	}

	// A plain member cannot dismiss.
	member := dbfx.User(t, "doc drift reader", "doc-drift-r-"+uuid.NewString()[:6]+"@example.test")
	dbfx.Member(t, testWorkspaceID, member, "member")
	testutil.Call(t, testHandler.DismissDocDriftProposal,
		testutil.WithURLParams(newRequestAs(member, http.MethodPost, "/api/doc-drift/proposals/"+first[0].ID+"/dismiss", nil), "id", first[0].ID)).
		Want(http.StatusForbidden)

	var dismissed docDriftProposalEnvelope
	testutil.Call(t, testHandler.DismissDocDriftProposal,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/doc-drift/proposals/"+first[0].ID+"/dismiss", nil), "id", first[0].ID)).
		Want(http.StatusOK).JSON(&dismissed)
	if dismissed.Proposal.Status != "dismissed" {
		t.Fatalf("dismissed: %+v", dismissed.Proposal)
	}
	// Settled once: a second dismiss has nothing to do.
	testutil.Call(t, testHandler.DismissDocDriftProposal,
		testutil.WithURLParams(newRequest(http.MethodPost, "/api/doc-drift/proposals/"+first[0].ID+"/dismiss", nil), "id", first[0].ID)).
		Want(http.StatusConflict)

	indexRepoAt(t, docDriftTestRepo, "commit-b")
	checkDocDrift(t, docDriftTestRepo).Want(http.StatusCreated)
	completeDocDriftTask(t, startedScanTask(t, docDriftTestRepo), report, "")

	after := listDocDriftProposals(t)
	if len(after) != 2 {
		t.Fatalf("a dismissed proposal never blocks a later detection: %+v", after)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM doc_drift_proposal WHERE workspace_id = $1 AND status = 'draft'`, testWorkspaceID); n != 1 {
		t.Fatalf("one fresh draft, got %d", n)
	}
}

func TestDocDriftMalformedFenceAuditsAndStoresNothing(t *testing.T) {
	docDriftCleanup(t)
	agentID := docDriftAgent(t)
	putDocDriftSettings(t, map[string]any{"enabled": true, "agent_id": agentID}).Want(http.StatusOK)
	indexRepoAt(t, docDriftTestRepo, "commit-a")
	checkDocDrift(t, docDriftTestRepo).Want(http.StatusCreated)
	scanTask := startedScanTask(t, docDriftTestRepo)

	completeDocDriftTask(t, scanTask, "I had a look.\n\n```doc_drift\nnot json at all\n```\n", "")

	if p := listDocDriftProposals(t); len(p) != 0 {
		t.Fatalf("an unreadable report proposes nothing: %+v", p)
	}
	if n := dbfx.Count(t, `SELECT COUNT(*) FROM audit_log_entry WHERE workspace_id = $1 AND action = $2`, testWorkspaceID, AuditDocDriftScanFailed); n != 1 {
		t.Fatalf("the failure is on the record, got %d audit rows", n)
	}
	// And the repository is released, so the next moved commit is checked.
	checkDocDrift(t, docDriftTestRepo).Want(http.StatusCreated)
}

// The scheduled sweep only touches workspaces that asked for it, and only
// repositories whose default branch actually moved. The due-ness rule itself is
// unit-tested in service/doc_drift_test.go; this checks the sweep honours it.
func TestDocDriftScheduledSweepSkipsDisabledAndUnchangedCommits(t *testing.T) {
	docDriftCleanup(t)
	agentID := docDriftAgent(t)
	indexRepoAt(t, docDriftTestRepo, "commit-a")

	// Disabled: never checked, however far the branch moves.
	putDocDriftSettings(t, map[string]any{"enabled": false, "agent_id": agentID}).Want(http.StatusOK)
	if n, err := testHandler.ScanDocDrift(t.Context()); err != nil || n != 0 {
		t.Fatalf("a disabled workspace is never checked: started=%d err=%v", n, err)
	}

	// Enabled and never checked at this commit: exactly one check.
	putDocDriftSettings(t, map[string]any{"enabled": true, "agent_id": agentID}).Want(http.StatusOK)
	if n, err := testHandler.ScanDocDrift(t.Context()); err != nil || n != 1 {
		t.Fatalf("a moved branch is checked: started=%d err=%v", n, err)
	}
	// Not again at the same commit, and not while the scan is in flight.
	if n, err := testHandler.ScanDocDrift(t.Context()); err != nil || n != 0 {
		t.Fatalf("the same commit is not checked twice: started=%d err=%v", n, err)
	}

	// Settle the scan, then move the branch: one more check.
	completeDocDriftTask(t, startedScanTask(t, docDriftTestRepo), "```doc_drift\n{\"proposals\":[],\"no_drift\":\"accurate\"}\n```", "")
	if n, err := testHandler.ScanDocDrift(t.Context()); err != nil || n != 0 {
		t.Fatalf("a settled scan at the same commit is still not due: started=%d err=%v", n, err)
	}
	indexRepoAt(t, docDriftTestRepo, "commit-b")
	if n, err := testHandler.ScanDocDrift(t.Context()); err != nil || n != 1 {
		t.Fatalf("the moved branch is checked again: started=%d err=%v", n, err)
	}
}
