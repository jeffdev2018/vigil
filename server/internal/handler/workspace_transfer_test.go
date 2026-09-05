package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/testutil"
)

// Workspace export / import (K76): the bundle carries the configuration
// and never a secret value (env values, gateway and MCP tokens, version tool
// configs, webhook tokens, triage token digests); import is admin-only,
// previews collisions, applies rename / merge / skip, asks the declared
// secrets back, never touches members; a template export seeds a new
// workspace.

func transferMultipart(t *testing.T, wsID, path string, data []byte, fields map[string]string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "bundle.zip")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write(data)
	for k, v := range fields {
		_ = mw.WriteField(k, v)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, path, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-User-ID", testUserID)
	req.Header.Set("X-Workspace-ID", wsID)
	return req
}

func TestWorkspaceTransfer(t *testing.T) {
	ctx := context.Background()
	const (
		envSecret     = "sk-livesecret1234567890abcdef"
		mcpSecret     = "mcp-token-XYZ-987654321"
		gatewaySecret = "gw-token-ABC-123456789"
		toolSecret    = "ghp_toolsecretQQQ1234567890"
		hookSecret    = "hook-token-777-abcdefghijk"
		signSecret    = "sign-999-abcdefghijklmnop"
		triageDigest  = "hash-555-abcdefghijklmnopqrstuvwxyz"
	)
	claude := providerRuntime(t, "claude")
	source := dbfx.Workspace(t, "xfer source", "xfer-source-"+uuid.NewString()[:8])
	dbfx.Member(t, source, testUserID, "owner")
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM workspace_transfer_run WHERE workspace_id = $1`, source) })
	src := func(req *http.Request) *http.Request { req.Header.Set("X-Workspace-ID", source); return req }
	agent := dbfx.Agent(t, "xfer agent "+uuid.NewString()[:8], claude, testutil.Cols{"workspace_id": source, "trust_mode": "autonomous",
		"custom_env": `{"OPENAI_API_KEY":"` + envSecret + `","REGION":"eu"}`, "scoped_env_keys": `["OPENAI_API_KEY"]`,
		"mcp_config":     `{"servers":{"x":{"url":"https://x.example","token":"` + mcpSecret + `"}}}`,
		"runtime_config": `{"gateway":{"url":"https://gw.example","token":"` + gatewaySecret + `"}}`, "instructions": "Be careful."})
	skill := dbfx.Insert(t, "skill", testutil.Cols{"workspace_id": source, "name": "xfer skill " + uuid.NewString()[:8], "description": "d", "content": "# skill", "config": "{}", "created_by": testUserID})
	dbfx.Insert(t, "skill_file", testutil.Cols{"skill_id": skill, "path": "references/a.md", "content": "ref"})
	dbfx.InsertNoID(t, "agent_skill", testutil.Cols{"agent_id": agent, "skill_id": skill}, "skill_id = $1", skill)
	dbfx.Insert(t, "agent_version", testutil.Cols{"workspace_id": source, "agent_id": agent, "version_number": 1, "instructions": "v1", "model": "m", "skill_ids": `["` + skill + `"]`, "tool_config": `{"mcp":{"api_key":"` + toolSecret + `"}}`, "note": "first", "created_by_type": "member", "created_by_id": testUserID})
	profile := dbfx.Insert(t, "agent_permission_profile", testutil.Cols{"id": uuid.NewString(), "workspace_id": source, "name": "xfer profile " + uuid.NewString()[:8], "description": "custom", "read_only": false, "denied_paths": `["secrets/*"]`, "allowed_commands": `["ls"]`, "hidden_secrets": `["*_PASSWORD"]`, "builtin": false})
	dbfx.Exec(t, `UPDATE agent SET permission_profile_id = $2 WHERE id = $1`, agent, profile)
	project := dbfx.Project(t, "xfer project "+uuid.NewString()[:8], testutil.Cols{"workspace_id": source, "description": "proj"})
	dbfx.Insert(t, "project_resource", testutil.Cols{"project_id": project, "workspace_id": source, "resource_type": "link", "resource_ref": `{"url":"https://docs.example"}`, "label": "Docs", "position": 1, "created_by": testUserID})
	mission := dbfx.Insert(t, "goal", testutil.Cols{"id": uuid.NewString(), "workspace_id": source, "title": "xfer mission " + uuid.NewString()[:8], "success_measure": "cash", "status": "active", "owner_id": testUserID})
	sub := dbfx.Insert(t, "goal", testutil.Cols{"id": uuid.NewString(), "workspace_id": source, "parent_goal_id": mission, "title": "xfer sub goal " + uuid.NewString()[:8], "status": "draft"})
	dbfx.InsertNoID(t, "project_goal", testutil.Cols{"workspace_id": source, "project_id": project, "goal_id": sub}, "project_id = $1 AND goal_id = $2", project, sub)
	autopilot := dbfx.Insert(t, "autopilot", testutil.Cols{"workspace_id": source, "title": "xfer autopilot " + uuid.NewString()[:8], "assignee_type": "agent", "assignee_id": agent, "status": "active", "execution_mode": "create_issue", "created_by_type": "member", "created_by_id": testUserID, "project_id": project})
	dbfx.Insert(t, "autopilot_trigger", testutil.Cols{"autopilot_id": autopilot, "kind": "webhook", "enabled": true, "webhook_token": hookSecret, "signing_secret": signSecret, "provider": "github", "event_filters": "[]"})
	dbfx.Insert(t, "triage_source", testutil.Cols{"workspace_id": source, "kind": "email", "ref_id": uuid.NewString(), "name": "xfer inbox " + uuid.NewString()[:8], "mode": "gate", "token_hash": triageDigest, "created_by_id": testUserID})
	dbfx.Insert(t, "org_structure", testutil.Cols{"id": uuid.NewString(), "workspace_id": source, "project_id": project, "model": "squads", "name": "xfer squads", "status": "active",
		"definition": `{"units":[{"id":"u","name":"Unit","owner_id":"` + testUserID + `","excludes":["external_effects"],"autonomy":"draft","allow":[],"deny":[],"escalation_quota_per_day":5,"members":[{"type":"member","id":"` + testUserID + `"},{"type":"agent","id":"` + agent + `"}],"roles":[]}],"edges":[],"rules":[],"committees":[]}`, "owner_id": testUserID})
	dbfx.Insert(t, "workspace_note", testutil.Cols{"id": uuid.NewString(), "workspace_id": source, "title": "xfer note", "content": "token " + envSecret + " must not leak", "tags": "{}", "created_by_id": testUserID})
	label := dbfx.Insert(t, "issue_label", testutil.Cols{"workspace_id": source, "resource_type": "issue", "name": "xfer-label-" + uuid.NewString()[:6], "description": "", "color": "#000000"})
	issue := dbfx.Issue(t, "xfer issue "+uuid.NewString()[:8], testutil.Cols{"workspace_id": source, "project_id": project, "goal_id": sub})
	dbfx.InsertNoID(t, "issue_to_label", testutil.Cols{"issue_id": issue, "label_id": label}, "issue_id = $1 AND label_id = $2", issue, label)

	// Export: the zip carries names and declarations, never a secret value.
	var run struct{ ID string }
	res := testutil.Call(t, testHandler.ExportWorkspace, src(newRequest(http.MethodPost, "/api/workspace-transfer/export", map[string]any{"include_issues": true, "include_notes": true, "template": true, "name": "xfer template"}))).Want(http.StatusOK)
	data := res.Body.Bytes()
	if !strings.Contains(res.Header().Get("Content-Disposition"), ".multica.zip") {
		t.Fatalf("download header: %q", res.Header().Get("Content-Disposition"))
	}
	run.ID = res.Header().Get("X-Transfer-Run-ID")
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, f := range zr.File {
		rc, _ := f.Open()
		raw, _ := io.ReadAll(rc)
		rc.Close()
		all.Write(raw)
	}
	text := all.String()
	for _, secret := range []string{envSecret, mcpSecret, gatewaySecret, toolSecret, hookSecret, signSecret, triageDigest} {
		if strings.Contains(text, secret) {
			t.Fatalf("secret value leaked into the bundle: %s", secret)
		}
	}
	for _, want := range []string{"OPENAI_API_KEY", "xfer skill", "references/a.md", "xfer profile", "xfer project", "xfer mission", "xfer autopilot", "xfer inbox", "xfer squads", "xfer note", "xfer issue", `"agent:xfer agent`} {
		if !strings.Contains(text, want) {
			t.Fatalf("bundle misses %q", want)
		}
	}
	var b transferBundle
	for _, f := range zr.File {
		if f.Name == "bundle.json" {
			rc, _ := f.Open()
			_ = json.NewDecoder(rc).Decode(&b)
			rc.Close()
		}
	}
	if b.Manifest.Counts["agents"] < 1 || len(b.Manifest.Secrets) < 4 || b.Agents[0].Versions[0].Instructions != "v1" || len(b.Issues) < 1 {
		t.Fatalf("manifest: %+v", b.Manifest)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM workspace_transfer_run WHERE id = $1 AND template AND bundle IS NOT NULL AND direction = 'export'`, run.ID) != 1 {
		t.Fatal("a template export keeps its bundle")
	}

	// Import into an empty workspace, as its owner: everything comes back, secrets are asked.
	target := dbfx.Workspace(t, "xfer target", "xfer-target-"+uuid.NewString()[:8])
	dbfx.Member(t, target, testUserID, "owner")
	if err := issuestatus.Ensure(ctx, testHandler.Queries, parseUUID(target)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, tbl := range []string{"issue_to_label", "issue", "issue_label", "workspace_note", "org_revision", "org_structure", "triage_source", "autopilot_trigger", "autopilot", "project_goal", "project_resource", "project", "goal", "agent_version", "agent_skill", "agent", "skill_file", "skill", "agent_permission_profile", "workspace_transfer_run", "member"} {
			switch tbl {
			case "issue_to_label":
				testPool.Exec(ctx, `DELETE FROM issue_to_label WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1)`, target)
			case "skill_file":
				testPool.Exec(ctx, `DELETE FROM skill_file WHERE skill_id IN (SELECT id FROM skill WHERE workspace_id = $1)`, target)
			case "agent_skill":
				testPool.Exec(ctx, `DELETE FROM agent_skill WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = $1)`, target)
			case "autopilot_trigger":
				testPool.Exec(ctx, `DELETE FROM autopilot_trigger WHERE autopilot_id IN (SELECT id FROM autopilot WHERE workspace_id = $1)`, target)
			default:
				testPool.Exec(ctx, `DELETE FROM `+tbl+` WHERE workspace_id = $1`, target)
			}
		}
		testPool.Exec(ctx, `DELETE FROM issue_status WHERE workspace_id = $1`, target)
	})
	var preview transferPreview
	testutil.Call(t, testHandler.PreviewWorkspaceImport, transferMultipart(t, target, "/api/workspace-transfer/preview", data, nil)).Want(http.StatusOK).JSON(&preview)
	if len(preview.Collisions) != 0 || len(preview.Secrets) < 4 || len(preview.Strategies) != 3 {
		t.Fatalf("preview: %+v", preview)
	}
	membersBefore := dbfx.Count(t, `SELECT COUNT(*) FROM member WHERE workspace_id = $1`, target)
	var out struct {
		RunID  string         `json:"run_id"`
		Report transferReport `json:"report"`
	}
	secrets := `{"` + b.Agents[0].Name + `":{"OPENAI_API_KEY":"sk-newvalue-0000000000"}}`
	testutil.Call(t, testHandler.ImportWorkspace, transferMultipart(t, target, "/api/workspace-transfer/import", data, map[string]string{"strategy": "rename", "secrets": secrets})).Want(http.StatusOK).JSON(&out)
	c := out.Report.Created
	if c["agents"] != 1 || c["skills"] != 1 || c["permission_profiles"] != 1 || c["projects"] != 1 || c["goals"] != 2 || c["autopilots"] != 1 || c["triage_sources"] != 1 || c["org_structures"] != 1 || c["notes"] != 1 || c["issues"] != 1 {
		t.Fatalf("created: %+v warnings=%v", c, out.Report.Warnings)
	}
	pending := 0
	for _, s := range out.Report.SecretsPending {
		if s.Key == "REGION" || s.Scope == "autopilot_trigger" {
			pending++
		}
		if s.Key == "OPENAI_API_KEY" {
			t.Fatal("a provided secret is not pending")
		}
	}
	if pending < 2 {
		t.Fatalf("secrets pending: %+v", out.Report.SecretsPending)
	}
	var env, scoped, owner, profileName string
	dbfx.QueryRow(t, `SELECT a.custom_env::text, a.scoped_env_keys::text, a.owner_id::text, COALESCE(p.name, '') FROM agent a LEFT JOIN agent_permission_profile p ON p.id = a.permission_profile_id WHERE a.workspace_id = $1`, target).Scan(&env, &scoped, &owner, &profileName)
	if !strings.Contains(env, "sk-newvalue") || strings.Contains(env, "REGION") || !strings.Contains(scoped, "OPENAI_API_KEY") || owner != testUserID || !strings.HasPrefix(profileName, "xfer profile") {
		t.Fatalf("imported agent env=%s scoped=%s owner=%s profile=%s", env, scoped, owner, profileName)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent_version WHERE workspace_id = $1 AND instructions = 'v1'`, target) != 1 || dbfx.Count(t, `SELECT COUNT(*) FROM skill_file sf JOIN skill s ON s.id = sf.skill_id WHERE s.workspace_id = $1`, target) != 1 {
		t.Fatal("versions and skill files come back")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM autopilot WHERE workspace_id = $1 AND status = 'paused' AND created_by_id = $2`, target, testUserID) != 1 || dbfx.Count(t, `SELECT COUNT(*) FROM autopilot_trigger t JOIN autopilot a ON a.id = t.autopilot_id WHERE a.workspace_id = $1 AND NOT t.enabled AND t.webhook_token IS NULL`, target) != 1 {
		t.Fatal("autopilots come back paused, triggers disabled without their secret")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM triage_source WHERE workspace_id = $1 AND token_hash = ''`, target) != 1 {
		t.Fatal("triage sources come back without their token")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM goal g JOIN goal p ON p.id = g.parent_goal_id WHERE g.workspace_id = $1`, target) != 1 || dbfx.Count(t, `SELECT COUNT(*) FROM project_goal WHERE workspace_id = $1`, target) != 1 {
		t.Fatal("goal ancestry and project links come back")
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM issue i JOIN issue_to_label il ON il.issue_id = i.id WHERE i.workspace_id = $1 AND i.goal_id IS NOT NULL`, target) != 1 {
		t.Fatal("issues come back with labels and goals")
	}
	var orgDef string
	dbfx.QueryRow(t, `SELECT definition::text FROM org_structure WHERE workspace_id = $1`, target).Scan(&orgDef)
	if strings.Contains(orgDef, "agent:") || !strings.Contains(orgDef, testUserID) {
		t.Fatalf("org structure remapped: %s", orgDef)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM member WHERE workspace_id = $1`, target) != membersBefore {
		t.Fatal("import never touches members")
	}
	// Second import: skip leaves everything, rename adds a copy, merge updates in place.
	testutil.Call(t, testHandler.PreviewWorkspaceImport, transferMultipart(t, target, "/api/workspace-transfer/preview", data, nil)).Want(http.StatusOK).JSON(&preview)
	if len(preview.Collisions) < 6 {
		t.Fatalf("collisions: %+v", preview.Collisions)
	}
	out.Report = transferReport{}
	testutil.Call(t, testHandler.ImportWorkspace, transferMultipart(t, target, "/api/workspace-transfer/import", data, map[string]string{"strategy": "skip"})).Want(http.StatusOK).JSON(&out)
	if len(out.Report.Created) != 0 || len(out.Report.Skipped) < 8 || dbfx.Count(t, `SELECT COUNT(*) FROM agent WHERE workspace_id = $1`, target) != 1 {
		t.Fatalf("skip: %+v", out.Report)
	}
	dbfx.Exec(t, `UPDATE agent SET instructions = 'edited locally' WHERE workspace_id = $1`, target)
	out.Report = transferReport{}
	testutil.Call(t, testHandler.ImportWorkspace, transferMultipart(t, target, "/api/workspace-transfer/import", data, map[string]string{"strategy": "merge"})).Want(http.StatusOK).JSON(&out)
	var instructions string
	dbfx.QueryRow(t, `SELECT instructions FROM agent WHERE workspace_id = $1 LIMIT 1`, target).Scan(&instructions)
	if out.Report.Merged["agents"] != 1 || instructions != "Be careful." || dbfx.Count(t, `SELECT COUNT(*) FROM agent WHERE workspace_id = $1`, target) != 1 {
		t.Fatalf("merge: %+v instructions=%q", out.Report.Merged, instructions)
	}
	out.Report = transferReport{}
	testutil.Call(t, testHandler.ImportWorkspace, transferMultipart(t, target, "/api/workspace-transfer/import", data, map[string]string{"strategy": "rename"})).Want(http.StatusOK).JSON(&out)
	if out.Report.Created["agents"] != 1 || out.Report.Created["issues"] != 1 || dbfx.Count(t, `SELECT COUNT(*) FROM workspace_note WHERE workspace_id = $1`, target) != 2 || dbfx.Count(t, `SELECT COUNT(*) FROM agent WHERE workspace_id = $1 AND name LIKE '%(imported)'`, target) != 1 {
		t.Fatalf("rename: %+v", out.Report.Created)
	}
	testutil.Call(t, testHandler.ImportWorkspace, transferMultipart(t, target, "/api/workspace-transfer/import", []byte("not a zip"), nil)).Want(http.StatusBadRequest)
	// A plain member cannot import.
	viewer := dbfx.User(t, "xfer viewer", "xfer-viewer-"+uuid.NewString()[:8]+"@example.test")
	dbfx.Member(t, target, viewer, "member")
	req := transferMultipart(t, target, "/api/workspace-transfer/import", data, nil)
	req.Header.Set("X-User-ID", viewer)
	testutil.Call(t, testHandler.ImportWorkspace, req).Want(http.StatusForbidden)
	var runs struct {
		Runs []struct {
			Direction string `json:"direction"`
			Status    string `json:"status"`
		} `json:"runs"`
	}
	r := newRequest(http.MethodGet, "/api/workspace-transfer/runs", nil)
	r.Header.Set("X-Workspace-ID", target)
	testutil.Call(t, testHandler.ListWorkspaceTransferRuns, r).Want(http.StatusOK).JSON(&runs)
	if len(runs.Runs) != 4 || runs.Runs[0].Direction != "import" || runs.Runs[0].Status != "completed" {
		t.Fatalf("runs: %+v", runs.Runs)
	}

	// Templates: the export is listed, and a workspace created from it is configured at once.
	var templates struct {
		Templates []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"templates"`
	}
	testutil.Call(t, testHandler.ListWorkspaceTemplates, newRequest(http.MethodGet, "/api/workspace-templates", nil)).Want(http.StatusOK).JSON(&templates)
	found := false
	for _, tpl := range templates.Templates {
		if tpl.ID == run.ID && tpl.Name == "xfer template" {
			found = true
		}
	}
	if !found {
		t.Fatalf("templates: %+v", templates.Templates)
	}
	var created WorkspaceResponse
	slug := "xfer-tpl-" + uuid.NewString()[:8]
	testutil.Call(t, testHandler.CreateWorkspace, newRequest(http.MethodPost, "/api/workspaces", map[string]any{"name": "From template", "slug": slug, "template_run_id": run.ID})).Want(http.StatusCreated).JSON(&created)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_skill WHERE agent_id IN (SELECT id FROM agent WHERE workspace_id = $1)`, created.ID)
		testPool.Exec(ctx, `DELETE FROM skill_file WHERE skill_id IN (SELECT id FROM skill WHERE workspace_id = $1)`, created.ID)
		testPool.Exec(ctx, `DELETE FROM autopilot_trigger WHERE autopilot_id IN (SELECT id FROM autopilot WHERE workspace_id = $1)`, created.ID)
		for _, tbl := range []string{"issue_to_label", "issue", "issue_label", "workspace_note", "org_revision", "org_structure", "triage_source", "autopilot", "project_goal", "project_resource", "project", "goal", "agent_version", "agent", "skill", "agent_permission_profile", "workspace_transfer_run", "issue_status", "member", "workspace"} {
			if tbl == "issue_to_label" {
				testPool.Exec(ctx, `DELETE FROM issue_to_label WHERE issue_id IN (SELECT id FROM issue WHERE workspace_id = $1)`, created.ID)
				continue
			}
			col := "workspace_id"
			if tbl == "workspace" {
				col = "id"
			}
			testPool.Exec(ctx, `DELETE FROM `+tbl+` WHERE `+col+` = $1`, created.ID)
		}
	})
	if created.TemplateError != "" || created.Template == nil {
		t.Fatalf("template seed: %+v", created)
	}
	if dbfx.Count(t, `SELECT COUNT(*) FROM agent WHERE workspace_id = $1`, created.ID) != 1 || dbfx.Count(t, `SELECT COUNT(*) FROM org_structure WHERE workspace_id = $1`, created.ID) != 2 {
		t.Fatal("a workspace from a template carries the template's agents and structures beside its default")
	}
}

func TestTransferScrub(t *testing.T) {
	t.Parallel()
	out := string(scrubJSON([]byte(`{"gateway":{"url":"https://x","token":"abc"},"servers":[{"apiKey":"k","plain":"ok","note":"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0"}]}`)))
	for _, want := range []string{`"token":"***"`, `"apiKey":"***"`, `"note":"***"`, `"plain":"ok"`, `"url":"https://x"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("scrub: %s", out)
		}
	}
	if string(scrubJSON([]byte("garbage"))) != "{}" || scrubText("key sk-abcdefghij1234 end") != "key *** end" {
		t.Fatal("garbage becomes an empty object; token shapes are masked in text")
	}
}
