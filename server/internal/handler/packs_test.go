package handler

// Packs (OS plan, vague B): every built-in pack validates and installs on a
// fresh workspace, a second install of the same version is refused, a forced
// re-apply creates nothing, an uninstall removes the configuration and keeps
// the content; the catalogue, preview, upload and export endpoints round-trip.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/packs"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
)

// packRowTables maps a ledger kind to the table its rows live in, for cleanup.
var packRowTables = map[string][]string{
	"permission_profiles": {"agent_permission_profile"},
	"skills":              {"skill_file:skill_id", "agent_skill:skill_id", "skill"},
	"agents":              {"agent_skill:agent_id", "agent"},
	"projects":            {"project_goal:project_id", "project_resource:project_id", "project"},
	"goals":               {"goal"},
	"autopilots":          {"autopilot_trigger:autopilot_id", "autopilot"},
	"triage_sources":      {"triage_source"},
	"org_structures":      {"org_revision:structure_id", "org_structure"},
	"notes":               {"workspace_note"},
	"issues":              {"issue_label_assignment:issue_id", "issue"},
	"issue_statuses":      {"issue_status"},
	"issue_types":         {"issue_type"},
	"labels":              {"issue_label"},
	"properties":          {"issue_property_type:property_id", "issue_property"},
	"views":               {"issue_view"},
	"transition_rules":    {"issue_transition_rule"},
	"business_rules":      {"business_rule"},
	"ownership_rules":     {"module_ownership"},
}

func cleanupPackRows(t *testing.T, items []transferItem) {
	t.Helper()
	ctx := context.Background()
	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		for _, spec := range packRowTables[it.Kind] {
			table, column := spec, "id"
			if j := indexByte(spec, ':'); j >= 0 {
				table, column = spec[:j], spec[j+1:]
			}
			_, _ = testPool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s = $1`, table, column), it.ID)
		}
	}
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace_pack_item WHERE workspace_id = $1`, testWorkspaceID)
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace_pack_install WHERE workspace_id = $1`, testWorkspaceID)
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace_transfer_run WHERE workspace_id = $1 AND source_name LIKE 'Multica pack%' OR (workspace_id = $1 AND source_name LIKE 'pack %')`, testWorkspaceID)
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

func TestBuiltinPacksInstallIdempotentlyAndUninstall(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	doctrineCleanup(t)
	all, err := packs.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 10 {
		t.Fatalf("catalogue has %d packs, want the ten business packs", len(all))
	}
	ctx := context.Background()
	for _, p := range all {
		p := p
		t.Run(p.Manifest.ID, func(t *testing.T) {
			b, err := packBundle(p)
			if err != nil {
				t.Fatal(err)
			}
			if problems := validateTransferBundle(b); len(problems) > 0 {
				t.Fatalf("pack is not valid:\n%s", joinLines(problems))
			}
			if len(b.Agents) == 0 || len(b.IssueTypes) == 0 || strings.TrimSpace(b.Doctrine) == "" || len(b.Views) == 0 {
				t.Fatalf("pack is thin: agents=%d types=%d views=%d doctrine=%v", len(b.Agents), len(b.IssueTypes), len(b.Views), b.Doctrine != "")
			}
			for _, a := range b.Agents {
				if a.TrustMode == "autonomous" || len(a.EnvKeys) > 0 {
					t.Fatalf("agent %q: a pack agent is never autonomous and carries no env keys", a.Name)
				}
			}
			var items []transferItem
			t.Cleanup(func() { cleanupPackRows(t, items) })

			install, report, err := testHandler.installPack(ctx, parseUUID(testWorkspaceID), p, packSourceBuiltin, "", parseUUID(testUserID), false)
			if err != nil {
				t.Fatalf("install: %v", err)
			}
			items = append(items, report.Items...)
			if install.Status != "installed" || report.Created["agents"] != len(b.Agents) || report.Created["issue_types"] != len(b.IssueTypes) || report.Created["views"] != len(b.Views) || report.Created["doctrine"] != 1 {
				t.Fatalf("first install report = %+v", report.Created)
			}
			if len(report.Skipped) > 0 {
				t.Fatalf("first install skipped %+v", report.Skipped)
			}
			// Autopilots arrive paused with disabled triggers.
			var enabled int
			dbfx.QueryRow(t, `SELECT COUNT(*) FROM autopilot_trigger tr JOIN autopilot a ON a.id = tr.autopilot_id WHERE a.workspace_id = $1 AND tr.enabled AND a.title = ANY($2)`, testWorkspaceID, autopilotTitles(b)).Scan(&enabled)
			if enabled != 0 {
				t.Fatalf("%d triggers enabled after install", enabled)
			}
			// The same version again is refused; a forced re-apply creates nothing.
			if _, _, err := testHandler.installPack(ctx, parseUUID(testWorkspaceID), p, packSourceBuiltin, "", parseUUID(testUserID), false); err == nil {
				t.Fatal("second install of the same version must be refused")
			}
			again, report2, err := testHandler.installPack(ctx, parseUUID(testWorkspaceID), p, packSourceBuiltin, "", parseUUID(testUserID), true)
			if err != nil {
				t.Fatalf("forced re-apply: %v", err)
			}
			items = append(items, report2.Items...)
			if len(report2.Created) != 0 {
				t.Fatalf("forced re-apply created %+v", report2.Created)
			}
			// The forced install carries the earlier rows forward.
			carried, _ := testHandler.Queries.ListPackItems(ctx, db.ListPackItemsParams{InstallID: again.ID, WorkspaceID: parseUUID(testWorkspaceID)})
			if len(carried) < len(report.Items) {
				t.Fatalf("carried %d rows forward, want at least %d", len(carried), len(report.Items))
			}
			// Uninstall removes the configuration and keeps the content.
			req := withURLParam(newRequest(http.MethodPost, "/api/packs/installed/"+uuidToString(again.ID)+"/uninstall", nil), "id", uuidToString(again.ID))
			var out struct {
				Install PackInstallResponse `json:"install"`
				Report  packUninstallReport `json:"report"`
			}
			testutil.Call(t, testHandler.UninstallPack, req).Want(http.StatusOK).JSON(&out)
			if out.Install.Status != "removed" || out.Report.Removed["agents"] != len(b.Agents) || out.Report.Removed["views"] != len(b.Views) || out.Report.Removed["issue_types"] != len(b.IssueTypes) {
				t.Fatalf("uninstall = %+v / %+v", out.Install.Status, out.Report.Removed)
			}
			var kept map[string]bool
			for _, k := range out.Report.Kept {
				if kept == nil {
					kept = map[string]bool{}
				}
				kept[k.Kind] = true
			}
			if !kept["projects"] || !kept["issues"] {
				t.Fatalf("content must be kept: %+v", out.Report.Kept)
			}
			testutil.Call(t, testHandler.UninstallPack, req).Want(http.StatusConflict)
		})
	}
}

func autopilotTitles(b *transferBundle) []string {
	out := make([]string, 0, len(b.Autopilots))
	for _, a := range b.Autopilots {
		out = append(out, a.Title)
	}
	return out
}

func joinLines(lines []string) string {
	var buf bytes.Buffer
	for _, l := range lines {
		buf.WriteString("- " + l + "\n")
	}
	return buf.String()
}

func TestPackEndpointsCatalogueUploadAndExport(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	doctrineCleanup(t)
	var items []transferItem
	t.Cleanup(func() { cleanupPackRows(t, items) })

	var list struct {
		Packs   []PackSummary `json:"packs"`
		Domains []string      `json:"domains"`
	}
	testutil.Call(t, testHandler.ListPacks, newRequest(http.MethodGet, "/api/packs", nil)).Want(http.StatusOK).JSON(&list)
	var helpdesk *PackSummary
	for i := range list.Packs {
		if list.Packs[i].Manifest.ID == "helpdesk-it" {
			helpdesk = &list.Packs[i]
		}
	}
	if helpdesk == nil || helpdesk.InstalledVersion != nil || helpdesk.Counts["agents"] == 0 || len(helpdesk.Prerequisites) == 0 || len(list.Domains) == 0 {
		t.Fatalf("catalogue = %+v", list)
	}
	var detail struct {
		Pack     PackSummary  `json:"pack"`
		Contents PackContents `json:"contents"`
	}
	testutil.Call(t, testHandler.GetPack, withURLParam(newRequest(http.MethodGet, "/api/packs/helpdesk-it", nil), "id", "helpdesk-it")).Want(http.StatusOK).JSON(&detail)
	if len(detail.Contents["agents"]) == 0 || len(detail.Contents["issue_statuses"]) == 0 {
		t.Fatalf("contents = %+v", detail.Contents)
	}
	var preview packPreview
	testutil.Call(t, testHandler.PreviewPack, withURLParam(newRequest(http.MethodPost, "/api/packs/helpdesk-it/preview", map[string]any{}), "id", "helpdesk-it")).Want(http.StatusOK).JSON(&preview)
	if preview.Strategy != transferStrategySkip || preview.Blocked != "" || len(preview.Problems) != 0 || len(preview.Collisions) != 0 {
		t.Fatalf("preview = %+v", preview)
	}
	var installed struct {
		Install PackInstallResponse `json:"install"`
		Report  transferReport      `json:"report"`
	}
	testutil.Call(t, testHandler.InstallPack, withURLParam(newRequest(http.MethodPost, "/api/packs/helpdesk-it/install", map[string]any{}), "id", "helpdesk-it")).Want(http.StatusOK).JSON(&installed)
	items = append(items, installed.Report.Items...)
	if installed.Install.PackID != "helpdesk-it" || installed.Install.ItemCount == 0 || installed.Install.Metric.Label == "" {
		t.Fatalf("install = %+v", installed.Install)
	}
	// The catalogue now shows it installed, the preview is blocked, the install list has it.
	testutil.Call(t, testHandler.PreviewPack, withURLParam(newRequest(http.MethodPost, "/api/packs/helpdesk-it/preview", map[string]any{}), "id", "helpdesk-it")).Want(http.StatusOK).JSON(&preview)
	if preview.Blocked == "" || preview.Installed == nil {
		t.Fatalf("preview after install = %+v", preview)
	}
	testutil.Call(t, testHandler.InstallPack, withURLParam(newRequest(http.MethodPost, "/api/packs/helpdesk-it/install", map[string]any{}), "id", "helpdesk-it")).Want(http.StatusConflict)
	var installs struct {
		Installs []PackInstallResponse `json:"installs"`
	}
	testutil.Call(t, testHandler.ListPackInstalls, newRequest(http.MethodGet, "/api/packs/installed", nil)).Want(http.StatusOK).JSON(&installs)
	if len(installs.Installs) != 1 || installs.Installs[0].Status != "installed" {
		t.Fatalf("installs = %+v", installs.Installs)
	}
	var one struct {
		Items []transferItem `json:"items"`
	}
	testutil.Call(t, testHandler.GetPackInstall, withURLParam(newRequest(http.MethodGet, "/api/packs/installed/"+installed.Install.ID, nil), "id", installed.Install.ID)).Want(http.StatusOK).JSON(&one)
	if len(one.Items) != installed.Install.ItemCount {
		t.Fatalf("items = %d, want %d", len(one.Items), installed.Install.ItemCount)
	}
	// A member cannot install; a run token cannot either (route guard, tested elsewhere).
	member := calendarGuest(t)
	testutil.Call(t, testHandler.InstallPack, withURLParam(newRequestAs(member, http.MethodPost, "/api/packs/helpdesk-it/install", map[string]any{}), "id", "helpdesk-it")).Want(http.StatusForbidden)

	// Export the workspace as a pack, parse it back, preview the upload.
	exportReq := newRequest(http.MethodPost, "/api/packs/export", map[string]any{"manifest": map[string]any{"id": "my-desk", "version": "0.1.0", "title": "My desk", "summary": "Exported from the tests.", "domain": "helpdesk", "metric": map[string]any{"label": "x", "description": "y"}}, "include_issues": true, "include_notes": true})
	resp := testutil.Call(t, testHandler.ExportPack, exportReq).Want(http.StatusOK)
	exported, err := packs.Parse(resp.Body.Bytes())
	if err != nil {
		t.Fatalf("exported pack does not parse: %v\n%s", err, resp.Body.String())
	}
	if exported.Manifest.ID != "my-desk" {
		t.Fatalf("exported manifest = %+v", exported.Manifest)
	}
	eb, err := packBundle(exported)
	if err != nil {
		t.Fatal(err)
	}
	if len(eb.IssueStatuses) < 2 || len(eb.Agents) < 1 || len(eb.Views) < 2 || eb.Doctrine == "" || len(eb.Properties) < 2 {
		t.Fatalf("exported bundle is thin: statuses=%d agents=%d views=%d properties=%d doctrine=%v", len(eb.IssueStatuses), len(eb.Agents), len(eb.Views), len(eb.Properties), eb.Doctrine != "")
	}
	if problems := validateTransferBundle(eb); len(problems) > 0 {
		t.Fatalf("exported pack is not valid:\n%s", joinLines(problems))
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "my-desk.pack.yaml")
	_, _ = fw.Write(resp.Body.Bytes())
	_ = mw.WriteField("strategy", "skip")
	_ = mw.Close()
	up := newRequest(http.MethodPost, "/api/packs/preview", nil)
	up.Body = io.NopCloser(bytes.NewReader(body.Bytes()))
	up.Header.Set("Content-Type", mw.FormDataContentType())
	up.ContentLength = int64(body.Len())
	var uploadPreview packPreview
	testutil.Call(t, testHandler.PreviewPackUpload, up).Want(http.StatusOK).JSON(&uploadPreview)
	if uploadPreview.Pack.Manifest.ID != "my-desk" || len(uploadPreview.Collisions) == 0 {
		t.Fatalf("upload preview = %+v", uploadPreview)
	}
	// Uninstall through the endpoint.
	unreq := withURLParam(newRequest(http.MethodPost, "/api/packs/installed/"+installed.Install.ID+"/uninstall", nil), "id", installed.Install.ID)
	testutil.Call(t, testHandler.UninstallPack, unreq).Want(http.StatusOK)
	testutil.Call(t, testHandler.ListPacks, newRequest(http.MethodGet, "/api/packs", nil)).Want(http.StatusOK).JSON(&list)
	for _, p := range list.Packs {
		if p.Manifest.ID == "helpdesk-it" && p.InstalledVersion != nil {
			t.Fatal("still installed after uninstall")
		}
	}
}

func TestPackCatalogueNeedsNoWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	req := newRequest(http.MethodGet, "/api/pack-catalogue", nil)
	req.Header.Del("X-Workspace-ID")
	var out struct {
		Packs []struct {
			Manifest packs.Manifest `json:"manifest"`
			Counts   map[string]int `json:"counts"`
			Contents PackContents   `json:"contents"`
		} `json:"packs"`
		Domains []string `json:"domains"`
	}
	testutil.Call(t, testHandler.ListPackCatalogue, req).Want(http.StatusOK).JSON(&out)
	if len(out.Packs) < 10 || out.Packs[0].Counts["agents"] == 0 || len(out.Packs[0].Contents["agents"]) == 0 || len(out.Domains) == 0 {
		t.Fatalf("catalogue = %d packs, %+v", len(out.Packs), out.Domains)
	}
	anon := newRequest(http.MethodGet, "/api/pack-catalogue", nil)
	anon.Header.Del("X-User-ID")
	anon.Header.Del("X-Workspace-ID")
	testutil.Call(t, testHandler.ListPackCatalogue, anon).Want(http.StatusUnauthorized)
}

func TestCreateWorkspaceSeedsFromAPack(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	const slug = "handler-tests-pack-seed"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	t.Cleanup(func() {
		var id string
		if err := testPool.QueryRow(context.Background(), `SELECT id::text FROM workspace WHERE slug = $1`, slug).Scan(&id); err == nil {
			for _, table := range []string{"workspace_pack_item", "workspace_pack_install", "workspace_transfer_run", "agent", "skill", "issue_status", "issue_type", "issue_label", "issue_property", "issue_view", "issue_transition_rule", "business_rule", "module_ownership", "project", "goal", "autopilot", "workspace_note", "issue", "workspace_doctrine_version", "member", "workspace"} {
				_, _ = testPool.Exec(context.Background(), fmt.Sprintf(`DELETE FROM %s WHERE workspace_id = $1`, table), id)
			}
			_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, id)
		}
	})
	var resp struct {
		ID            string         `json:"id"`
		Template      map[string]any `json:"template"`
		TemplateError string         `json:"template_error"`
	}
	testutil.Call(t, testHandler.CreateWorkspace, newRequest(http.MethodPost, "/api/workspaces", map[string]any{"name": "Pack seed probe", "slug": slug, "pack_id": "helpdesk-it"})).Want(http.StatusCreated).JSON(&resp)
	if resp.TemplateError != "" || resp.Template == nil || resp.Template["pack_id"] != "helpdesk-it" {
		t.Fatalf("workspace pack seed = %+v / %q", resp.Template, resp.TemplateError)
	}
	var n int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM workspace_pack_install WHERE workspace_id = $1 AND status = 'installed'`, resp.ID).Scan(&n)
	if n != 1 {
		t.Fatalf("installs on the new workspace = %d", n)
	}
	var agents int
	dbfx.QueryRow(t, `SELECT COUNT(*) FROM agent WHERE workspace_id = $1 AND name LIKE 'Helpdesk%'`, resp.ID).Scan(&agents)
	if agents == 0 {
		t.Fatal("the pack's agent was not created in the new workspace")
	}
	// An unknown pack leaves the workspace and says so.
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug+"-2")
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, slug+"-2") })
	testutil.Call(t, testHandler.CreateWorkspace, newRequest(http.MethodPost, "/api/workspaces", map[string]any{"name": "Pack seed probe 2", "slug": slug + "-2", "pack_id": "nope"})).Want(http.StatusCreated).JSON(&resp)
	if resp.TemplateError == "" {
		t.Fatal("an unknown pack must be reported")
	}
}

func TestRewriteViewQueryResolvesPropertyNames(t *testing.T) {
	refs := &transferRefs{properties: map[string]transferPropertyRef{"impact": {id: parseUUID("11111111-1111-4111-8111-111111111111"), options: map[string]string{"everyone": "opt-1"}}}}
	out := rewriteViewQuery(json.RawMessage(`{"typeFilters":["ticket"],"propertyFilters":{"Impact":["Everyone","opt-9"],"22222222-2222-4222-8222-222222222222":["x"]}}`), refs)
	var q map[string]any
	if err := json.Unmarshal(out, &q); err != nil {
		t.Fatal(err)
	}
	filters := q["propertyFilters"].(map[string]any)
	if got := filters["11111111-1111-4111-8111-111111111111"]; got == nil || got.([]any)[0] != "opt-1" || got.([]any)[1] != "opt-9" {
		t.Fatalf("rewritten = %v", filters)
	}
	if filters["22222222-2222-4222-8222-222222222222"] == nil || filters["Impact"] != nil {
		t.Fatalf("untouched keys = %v", filters)
	}
}

// yaml.v3 emits a multi-line string whose first line starts with a space as
// a literal block no parser reads back; the export chooses the style itself.
func TestPackYAMLReadsBackWithAwkwardStrings(t *testing.T) {
	b := newTransferBundle()
	b.Skills = []transferSkill{{Name: "s", Description: "d", Content: "  indented first line\nsecond line\n", Status: "published", Config: json.RawMessage(`{}`), Files: []transferFile{{Path: "a.md", Content: "trailing space \nline\t tab\r\n"}}}}
	b.Agents = []transferAgent{{Name: "A", Instructions: "```\n  code\n```\n", Skills: []string{"s"}, ConversationStarters: json.RawMessage(`["hi"]`), CustomArgs: json.RawMessage(`[]`)}}
	b.Doctrine = " starts with a space\n- rule"
	raw, err := packYAML(packs.Manifest{ID: "awkward", Version: "1.0.0", Title: "Awkward", Summary: "s", Domain: "ops", Metric: packs.Metric{Label: "l", Description: "d"}}, b)
	if err != nil {
		t.Fatal(err)
	}
	p, err := packs.Parse(raw)
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, raw)
	}
	back, err := packBundle(p)
	if err != nil {
		t.Fatal(err)
	}
	if back.Skills[0].Content != b.Skills[0].Content || back.Skills[0].Files[0].Content != b.Skills[0].Files[0].Content || back.Agents[0].Instructions != b.Agents[0].Instructions || back.Doctrine != b.Doctrine {
		t.Fatalf("round trip changed the text:\n%q\n%q\n%q", back.Skills[0].Content, back.Skills[0].Files[0].Content, back.Doctrine)
	}
}

// A pack export leaves machine-local skills out and, on request, every skill;
// the agents' skill lists follow, so the file validates.
func TestPackExportLeavesMachineSkillsOut(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	local := dbfx.Insert(t, "skill", testutil.Cols{"workspace_id": testWorkspaceID, "name": "machine-only-" + uuidShort(), "description": "d", "content": "c", "config": `{"origin":{"type":"runtime_local","runtime_id":"r","provider":"codex","source_path":"~/.agents/skills/x"}}`, "created_by": testUserID})
	shared := dbfx.Insert(t, "skill", testutil.Cols{"workspace_id": testWorkspaceID, "name": "shared-" + uuidShort(), "description": "d", "content": "c", "config": `{}`, "created_by": testUserID})
	agent := dbfx.Agent(t, "export agent "+uuidShort(), handlerTestRuntimeID(t))
	for _, sk := range []string{local, shared} {
		dbfx.InsertNoID(t, "agent_skill", testutil.Cols{"agent_id": agent, "skill_id": sk}, "agent_id = $1 AND skill_id = $2", agent, sk)
	}
	manifest := map[string]any{"id": "skills-probe", "version": "0.1.0", "title": "Skills probe", "summary": "s", "domain": "ops", "metric": map[string]any{"label": "l", "description": "d"}}
	resp := testutil.Call(t, testHandler.ExportPack, newRequest(http.MethodPost, "/api/packs/export", map[string]any{"manifest": manifest})).Want(http.StatusOK)
	p, err := packs.Parse(resp.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	b, err := packBundle(p)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, s := range b.Skills {
		names[s.Name] = true
	}
	var localName, sharedName string
	dbfx.QueryRow(t, `SELECT name FROM skill WHERE id = $1`, local).Scan(&localName)
	dbfx.QueryRow(t, `SELECT name FROM skill WHERE id = $1`, shared).Scan(&sharedName)
	if names[localName] || !names[sharedName] {
		t.Fatalf("skills exported = %v (machine-local %q must be out, shared %q in)", names, localName, sharedName)
	}
	if problems := validateTransferBundle(b); len(problems) > 0 {
		t.Fatalf("export with a dropped skill does not validate:\n%s", joinLines(problems))
	}
	// include_skills=false: no skill at all, agents reference none, still valid.
	resp = testutil.Call(t, testHandler.ExportPack, newRequest(http.MethodPost, "/api/packs/export", map[string]any{"manifest": manifest, "include_skills": false})).Want(http.StatusOK)
	p, err = packs.Parse(resp.Body.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	b, err = packBundle(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Skills) != 0 {
		t.Fatalf("include_skills=false exported %d skills", len(b.Skills))
	}
	for _, a := range b.Agents {
		if len(a.Skills) != 0 {
			t.Fatalf("agent %q still lists skills %v", a.Name, a.Skills)
		}
	}
	if problems := validateTransferBundle(b); len(problems) > 0 {
		t.Fatalf("export without skills does not validate:\n%s", joinLines(problems))
	}
}

func uuidShort() string { return uuidToString(dbid.NewV7())[:8] }
