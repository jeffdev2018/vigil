package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// DAEMON.md import / export (F24 / JEF-15).
//
// The parse/validate matrix is canonical in internal/skill/frontmatter_test.go
// (TestParseDaemonMarkdownRejections). What is proven HERE is what the parser
// cannot see: that a rejected document writes nothing, that the digest makes a
// re-import a true no-op down to updated_at, that the conflict strategies pick
// the right row, and that an export re-imports to the same configuration.

// daemonAgentName gives each test its own agent so parallel-safe reruns and
// the "agent not found" candidate list stay predictable.
func seedDaemonAgent(t *testing.T, name string) string {
	t.Helper()
	rt := dbfx.Runtime(t, "daemon-import-rt-"+name)
	return dbfx.Agent(t, name, rt)
}

func daemonDoc(name, agent string, extra ...string) string {
	return fmt.Sprintf(`---
name: %s
role: Sort the inbound queue and label it.
agent: %s
outputs: issue
triggers:
  - kind: schedule
    cron: "0 9 * * *"
    timezone: UTC
    label: Morning
%s---

# %s

Read the queue, then label.
`, name, agent, strings.Join(extra, ""), name)
}

func importDaemon(t *testing.T, markdown, strategy string) *testutil.Response {
	t.Helper()
	body := map[string]any{"markdown": markdown}
	if strategy != "" {
		body["strategy"] = strategy
	}
	req := newRequest("POST", "/api/autopilots/import", body)
	return testutil.Call(t, testHandler.ImportDaemon, req)
}

type daemonImportBody struct {
	Status    string `json:"status"`
	SkillID   string `json:"skill_id"`
	Digest    string `json:"digest"`
	Autopilot struct {
		ID            string  `json:"id"`
		Title         string  `json:"title"`
		Description   *string `json:"description"`
		AssigneeID    string  `json:"assignee_id"`
		ExecutionMode string  `json:"execution_mode"`
		UpdatedAt     string  `json:"updated_at"`
	} `json:"autopilot"`
	Triggers []struct {
		Kind           string `json:"kind"`
		CronExpression string `json:"cron_expression"`
		Label          string `json:"label"`
	} `json:"triggers"`
	Warnings []string `json:"warnings"`
}

func cleanupDaemon(t *testing.T, autopilotID, skillID string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		testPool.Exec(ctx, `DELETE FROM autopilot_memory WHERE autopilot_id = $1`, autopilotID)
		testPool.Exec(ctx, `DELETE FROM autopilot_trigger WHERE autopilot_id = $1`, autopilotID)
		testPool.Exec(ctx, `DELETE FROM autopilot_rule_version WHERE autopilot_id = $1`, autopilotID)
		testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)
		if skillID != "" {
			testPool.Exec(ctx, `DELETE FROM agent_skill WHERE skill_id = $1`, skillID)
			testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, skillID)
		}
	})
}

// Acceptance 1: a valid declaration creates the autopilot, its triggers, a
// skill, and the agent_skill link — the whole projection, not part of it.
func TestImportDaemonCreatesAutopilotTriggersAndSkill(t *testing.T) {
	agentID := seedDaemonAgent(t, "Nova-create")
	name := "Nightly triage create"

	var got daemonImportBody
	importDaemon(t, daemonDoc(name, "Nova-create"), "").Want(http.StatusCreated).JSON(&got)
	cleanupDaemon(t, got.Autopilot.ID, got.SkillID)

	if got.Status != "created" {
		t.Errorf("status = %q, want created", got.Status)
	}
	if got.Autopilot.Title != name {
		t.Errorf("title = %q, want %q", got.Autopilot.Title, name)
	}
	if got.Autopilot.AssigneeID != agentID {
		t.Errorf("assignee = %q, want the agent named in the declaration (%q)", got.Autopilot.AssigneeID, agentID)
	}
	if got.Autopilot.ExecutionMode != "create_issue" {
		t.Errorf("execution_mode = %q; outputs: issue must map to create_issue", got.Autopilot.ExecutionMode)
	}
	if got.Autopilot.Description == nil || *got.Autopilot.Description != "Sort the inbound queue and label it." {
		t.Errorf("description = %v; role becomes the autopilot description", got.Autopilot.Description)
	}
	if len(got.Triggers) != 1 || got.Triggers[0].Kind != "schedule" || got.Triggers[0].CronExpression != "0 9 * * *" {
		t.Fatalf("triggers = %+v, want one schedule at 0 9 * * *", got.Triggers)
	}
	if got.SkillID == "" {
		t.Fatal("no skill was created; the declaration body is the agent's skill")
	}

	var skillName, skillContent string
	dbfx.QueryRow(t, `SELECT name, content FROM skill WHERE id = $1`, got.SkillID).Scan(&skillName, &skillContent)
	if skillName != name {
		t.Errorf("skill name = %q, want the daemon name %q", skillName, name)
	}
	if !strings.Contains(skillContent, "Read the queue, then label.") {
		t.Errorf("skill content is not the declaration body: %q", skillContent)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM agent_skill WHERE agent_id = $1 AND skill_id = $2`, agentID, got.SkillID); n != 1 {
		t.Errorf("agent_skill rows = %d, want 1; the skill must be bound to the daemon's agent", n)
	}
	// Creating a daemon publishes rule version v1, like any other autopilot
	// create: a dispatch with no accountable human is the thing that rule
	// versions exist to prevent.
	if n := dbfx.Count(t, `SELECT count(*) FROM autopilot_rule_version WHERE autopilot_id = $1`, got.Autopilot.ID); n != 1 {
		t.Errorf("rule versions = %d, want 1", n)
	}
}

// Acceptance 2: the same document twice is a no-op — no second autopilot, no
// duplicated trigger, and updated_at unmoved. A repository that re-applies its
// declarations on every deploy must not churn the row.
func TestImportDaemonIsIdempotentOnIdenticalDigest(t *testing.T) {
	seedDaemonAgent(t, "Nova-idem")
	doc := daemonDoc("Nightly triage idem", "Nova-idem")

	var first daemonImportBody
	importDaemon(t, doc, "").Want(http.StatusCreated).JSON(&first)
	cleanupDaemon(t, first.Autopilot.ID, first.SkillID)

	var before string
	dbfx.QueryRow(t, `SELECT updated_at::text FROM autopilot WHERE id = $1`, first.Autopilot.ID).Scan(&before)

	var second daemonImportBody
	// Strategy is deliberately the DEFAULT (fail): an identical re-import is a
	// no-op whatever the conflict strategy says, because nothing conflicts.
	importDaemon(t, doc, "").Want(http.StatusOK).JSON(&second)

	if second.Status != "unchanged" {
		t.Errorf("status = %q, want unchanged", second.Status)
	}
	if second.Autopilot.ID != first.Autopilot.ID {
		t.Errorf("a second autopilot was created: %q then %q", first.Autopilot.ID, second.Autopilot.ID)
	}
	var after string
	dbfx.QueryRow(t, `SELECT updated_at::text FROM autopilot WHERE id = $1`, first.Autopilot.ID).Scan(&after)
	if after != before {
		t.Errorf("updated_at moved on an identical re-import: %q -> %q", before, after)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM autopilot_trigger WHERE autopilot_id = $1`, first.Autopilot.ID); n != 1 {
		t.Errorf("triggers = %d after re-import, want 1", n)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM autopilot WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, "Nightly triage idem"); n != 1 {
		t.Errorf("autopilots with this name = %d, want 1", n)
	}
}

// Acceptance 3: an invalid document is refused and leaves NOTHING behind. The
// per-field matrix lives in the parser test; what matters here is the absence
// of a partial write.
func TestImportDaemonRejectsInvalidDocumentWithoutWriting(t *testing.T) {
	seedDaemonAgent(t, "Nova-invalid")

	for _, tc := range []struct {
		name string
		doc  string
	}{
		{
			name: "unknown frontmatter key",
			doc: `---
name: Bad daemon unknown key
role: Something
agent: Nova-invalid
schedule: daily
---
body
`,
		},
		{
			name: "invalid trigger kind",
			doc: `---
name: Bad daemon bad kind
role: Something
agent: Nova-invalid
triggers:
  - kind: cron
---
body
`,
		},
		{
			name: "invalid cron",
			doc: `---
name: Bad daemon bad cron
role: Something
agent: Nova-invalid
triggers:
  - kind: schedule
    cron: "not a cron"
---
body
`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := importDaemon(t, tc.doc, "").Want(http.StatusBadRequest)
			body := resp.Map()
			errs, _ := body["errors"].([]any)
			if len(errs) == 0 {
				t.Fatalf("no line-level errors in the 400 body: %v", body)
			}
			first, _ := errs[0].(map[string]any)
			if line, _ := first["line"].(float64); line <= 0 {
				t.Errorf("error carries no line number: %v", first)
			}
			if n := dbfx.Count(t, `SELECT count(*) FROM autopilot WHERE workspace_id = $1 AND description = 'Something'`,
				testWorkspaceID); n != 0 {
				t.Errorf("a rejected document wrote %d autopilot(s); a 400 must leave nothing behind", n)
			}
		})
	}
}

// Acceptance 4: export then re-import yields the same configuration. This is
// the round-trip an operator relies on to move a daemon between workspaces.
func TestExportDaemonReImportsToTheSameConfiguration(t *testing.T) {
	seedDaemonAgent(t, "Nova-export")
	doc := daemonDoc("Nightly triage export", "Nova-export")

	var created daemonImportBody
	importDaemon(t, doc, "").Want(http.StatusCreated).JSON(&created)
	cleanupDaemon(t, created.Autopilot.ID, created.SkillID)

	req := withURLParam(newRequest("GET", "/api/autopilots/"+created.Autopilot.ID+"/export", nil), "id", created.Autopilot.ID)
	exported := testutil.Call(t, testHandler.ExportDaemon, req).Want(http.StatusOK).Text()

	if exported != doc {
		t.Errorf("an imported daemon must export its source verbatim, so a comment or a budget block is not silently dropped:\n got %q\nwant %q", exported, doc)
	}

	var reimported daemonImportBody
	importDaemon(t, exported, "").Want(http.StatusOK).JSON(&reimported)
	if reimported.Status != "unchanged" {
		t.Errorf("re-importing the export changed the daemon (status %q); the round-trip is not stable", reimported.Status)
	}
}

// An autopilot that never came from a declaration still exports one, rendered
// from its columns — that is how an existing autopilot gets pulled into a
// repository in the first place.
func TestExportDaemonRendersAnAutopilotThatHasNoSource(t *testing.T) {
	agentID := seedDaemonAgent(t, "Nova-render")
	apID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           "Hand made autopilot",
		"description":     "Do the thing.",
		"assignee_type":   "agent",
		"assignee_id":     agentID,
		"status":          "active",
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	dbfx.Insert(t, "autopilot_trigger", testutil.Cols{
		"autopilot_id":    apID,
		"kind":            "schedule",
		"enabled":         true,
		"cron_expression": "30 6 * * 1",
		"timezone":        "UTC",
	})

	req := withURLParam(newRequest("GET", "/api/autopilots/"+apID+"/export", nil), "id", apID)
	exported := testutil.Call(t, testHandler.ExportDaemon, req).Want(http.StatusOK).Text()

	for _, want := range []string{"Hand made autopilot", "Nova-render", "run_only", "30 6 * * 1"} {
		if !strings.Contains(exported, want) {
			t.Errorf("rendered declaration is missing %q:\n%s", want, exported)
		}
	}
	// It must be importable, not merely readable.
	var round daemonImportBody
	importDaemon(t, exported, "overwrite").Want(http.StatusOK).JSON(&round)
	cleanupDaemon(t, round.Autopilot.ID, round.SkillID)
	if round.Autopilot.ID != apID {
		t.Errorf("the rendered export created a new autopilot %q instead of matching %q", round.Autopilot.ID, apID)
	}
	if round.Autopilot.ExecutionMode != "run_only" {
		t.Errorf("execution_mode drifted to %q on the round-trip", round.Autopilot.ExecutionMode)
	}
}

// The conflict strategies: same name, different content.
func TestImportDaemonConflictStrategies(t *testing.T) {
	seedDaemonAgent(t, "Nova-conflict")
	name := "Nightly triage conflict"
	original := daemonDoc(name, "Nova-conflict")
	changed := strings.Replace(original, "0 9 * * *", "0 18 * * *", 1)

	var created daemonImportBody
	importDaemon(t, original, "").Want(http.StatusCreated).JSON(&created)
	cleanupDaemon(t, created.Autopilot.ID, created.SkillID)

	t.Run("fail is the default", func(t *testing.T) {
		body := importDaemon(t, changed, "").Want(http.StatusConflict).Map()
		if body["code"] != "daemon_name_conflict" {
			t.Errorf("code = %v, want daemon_name_conflict", body["code"])
		}
	})

	t.Run("overwrite replaces the declared configuration", func(t *testing.T) {
		var got daemonImportBody
		importDaemon(t, changed, "overwrite").Want(http.StatusOK).JSON(&got)
		if got.Status != "updated" || got.Autopilot.ID != created.Autopilot.ID {
			t.Fatalf("overwrite created %q (status %q) instead of updating %q", got.Autopilot.ID, got.Status, created.Autopilot.ID)
		}
		// Triggers are REPLACED, not merged: a schedule the document dropped
		// must stop firing, and merging would leave the old 09:00 alive.
		if len(got.Triggers) != 1 || got.Triggers[0].CronExpression != "0 18 * * *" {
			t.Errorf("triggers = %+v, want exactly the declared 0 18 * * *", got.Triggers)
		}
		if n := dbfx.Count(t, `SELECT count(*) FROM autopilot_trigger WHERE autopilot_id = $1`, created.Autopilot.ID); n != 1 {
			t.Errorf("trigger rows = %d after overwrite, want 1", n)
		}
	})

	t.Run("rename keeps both", func(t *testing.T) {
		var got daemonImportBody
		importDaemon(t, strings.Replace(changed, "0 18 * * *", "0 20 * * *", 1), "rename").
			Want(http.StatusCreated).JSON(&got)
		cleanupDaemon(t, got.Autopilot.ID, got.SkillID)
		if got.Autopilot.ID == created.Autopilot.ID {
			t.Fatal("rename overwrote the existing daemon")
		}
		if got.Autopilot.Title != name+"-2" {
			t.Errorf("renamed title = %q, want %q", got.Autopilot.Title, name+"-2")
		}
	})

	t.Run("unknown strategy is refused", func(t *testing.T) {
		importDaemon(t, changed, "merge").Want(http.StatusBadRequest)
	})
}

// A declaration naming an agent the workspace does not have is a 404 that
// lists what it does have — the operator mistyped a name, and the fix is in
// the message.
func TestImportDaemonUnknownAgentListsCandidates(t *testing.T) {
	seedDaemonAgent(t, "Nova-candidates")

	body := importDaemon(t, daemonDoc("Nightly triage unknown agent", "Nebula-does-not-exist"), "").
		Want(http.StatusNotFound).Map()

	if body["code"] != "daemon_agent_not_found" {
		t.Errorf("code = %v, want daemon_agent_not_found", body["code"])
	}
	msg, _ := body["error"].(string)
	if !strings.Contains(msg, "Nova-candidates") {
		t.Errorf("the message does not name the agents that DO exist, so the operator cannot fix the typo: %q", msg)
	}
}

// budget parses, round-trips, and is reported as inert. Nothing enforces it:
// run quota is workspace-scoped (autopilot_quota_period), and there is no
// per-autopilot limit column to write it into. Saying so beats dropping it.
func TestImportDaemonReportsBudgetAsUnenforced(t *testing.T) {
	seedDaemonAgent(t, "Nova-budget")
	doc := daemonDoc("Nightly triage budget", "Nova-budget", "budget:\n  runs_per_day: 3\n")

	var got daemonImportBody
	importDaemon(t, doc, "").Want(http.StatusCreated).JSON(&got)
	cleanupDaemon(t, got.Autopilot.ID, got.SkillID)

	if len(got.Warnings) == 0 || !strings.Contains(strings.Join(got.Warnings, " "), "budget") {
		t.Errorf("budget was accepted silently; warnings = %v", got.Warnings)
	}
	var stored string
	dbfx.QueryRow(t, `SELECT source_markdown FROM autopilot WHERE id = $1`, got.Autopilot.ID).Scan(&stored)
	if !strings.Contains(stored, "runs_per_day: 3") {
		t.Errorf("the budget block did not survive into source_markdown, so the export would drop it: %q", stored)
	}
}

// Preview writes nothing and answers the two questions the dialog asks before
// it dares to: is this valid, and would it change anything.
func TestPreviewDaemonImportDoesNotWrite(t *testing.T) {
	seedDaemonAgent(t, "Nova-preview")
	doc := daemonDoc("Nightly triage preview", "Nova-preview")

	req := newRequest("POST", "/api/autopilots/import/preview", map[string]any{"markdown": doc})
	body := testutil.Call(t, testHandler.PreviewDaemonImport, req).Want(http.StatusOK).Map()

	if body["valid"] != true {
		t.Fatalf("a valid declaration previewed as invalid: %v", body)
	}
	fm, _ := body["frontmatter"].(map[string]any)
	if fm == nil || fm["name"] != "Nightly triage preview" {
		t.Errorf("frontmatter = %v", fm)
	}
	if bodyText, _ := body["body"].(string); !strings.Contains(bodyText, "Read the queue, then label.") {
		t.Errorf("the rendered body is missing: %q", bodyText)
	}
	if body["agent_id"] == "" || body["agent_id"] == nil {
		t.Errorf("preview did not resolve the agent: %v", body)
	}
	if n := dbfx.Count(t, `SELECT count(*) FROM autopilot WHERE workspace_id = $1 AND title = $2`,
		testWorkspaceID, "Nightly triage preview"); n != 0 {
		t.Errorf("preview created %d autopilot(s); it must not write", n)
	}
}

// A preview of a broken document reports its lines and refuses to show
// half-parsed frontmatter as if it had been accepted.
func TestPreviewDaemonImportReportsLineErrors(t *testing.T) {
	req := newRequest("POST", "/api/autopilots/import/preview", map[string]any{
		"markdown": "---\nname: a\nrole: b\nagent: c\nbogus: 1\n---\nbody\n",
	})
	body := testutil.Call(t, testHandler.PreviewDaemonImport, req).Want(http.StatusOK).Map()

	if body["valid"] != false {
		t.Fatalf("an invalid declaration previewed as valid: %v", body)
	}
	if body["frontmatter"] != nil {
		t.Errorf("half-parsed frontmatter was returned for an invalid document: %v", body["frontmatter"])
	}
	errs, _ := body["errors"].([]any)
	if len(errs) != 1 {
		t.Fatalf("errors = %v, want exactly the unknown key", errs)
	}
	first, _ := errs[0].(map[string]any)
	if line, _ := first["line"].(float64); line != 5 {
		t.Errorf("unknown key reported on line %v, want 5", first["line"])
	}
}
