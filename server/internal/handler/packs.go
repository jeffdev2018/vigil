package handler

// Packs (OS plan, vague B): a pack is a transfer bundle with a manifest,
// shipped in the built-in catalogue or uploaded as pack.yaml. Installing one
// runs the transfer pipeline (preview, strategy, one transaction, report) and
// writes an install ledger with every row it created, so the pack can be
// upgraded in place or uninstalled. A pack never carries a credential:
// autopilots arrive disabled, business rules as drafts, agents without a
// runtime bound.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/packs"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/dbid"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"gopkg.in/yaml.v3"
	"log/slog"
)

const (
	AuditPackInstalled   = "pack.installed"
	AuditPackUninstalled = "pack.uninstalled"

	packSourceBuiltin   = "builtin"
	packSourceUpload    = "upload"
	packSourceWorkspace = "workspace"
)

// PackSummary is one catalogue card.
type PackSummary struct {
	Manifest         packs.Manifest     `json:"manifest"`
	Counts           map[string]int     `json:"counts"`
	Builtin          bool               `json:"builtin"`
	InstalledVersion *string            `json:"installed_version"`
	InstallID        *string            `json:"install_id"`
	Upgrade          bool               `json:"upgrade_available"`
	Prerequisites    []PackPrerequisite `json:"prerequisites"`
}

// PackPrerequisite is a declared prerequisite with what the server could
// verify about it: met, missing, or unknown when it cannot tell.
type PackPrerequisite struct {
	packs.Prerequisite
	Status string `json:"status"`
}

// PackContents lists the names a pack would create, per kind.
type PackContents map[string][]string

type PackInstallResponse struct {
	ID           string          `json:"id"`
	PackID       string          `json:"pack_id"`
	PackVersion  string          `json:"pack_version"`
	Title        string          `json:"title"`
	Source       string          `json:"source"`
	Strategy     string          `json:"strategy"`
	Status       string          `json:"status"`
	RunID        *string         `json:"run_id"`
	Report       json.RawMessage `json:"report"`
	Manifest     json.RawMessage `json:"manifest"`
	InstalledBy  *string         `json:"installed_by"`
	InstalledAt  string          `json:"installed_at"`
	RemovedAt    *string         `json:"removed_at"`
	ItemCount    int             `json:"item_count"`
	Metric       packs.Metric    `json:"metric"`
	Domain       string          `json:"domain"`
	UpgradeTo    *string         `json:"upgrade_to"`
	BundleSha256 string          `json:"bundle_sha256"`
}

func packInstallToResponse(x db.WorkspacePackInstall, items int, upgradeTo *string) PackInstallResponse {
	var m packs.Manifest
	_ = json.Unmarshal(x.Manifest, &m)
	return PackInstallResponse{
		ID: uuidToString(x.ID), PackID: x.PackID, PackVersion: x.PackVersion, Title: x.Title, Source: x.Source, Strategy: x.Strategy, Status: x.Status,
		RunID: uuidToPtr(x.RunID), Report: json.RawMessage(nonEmptyJSON(x.Report)), Manifest: json.RawMessage(nonEmptyJSON(x.Manifest)), InstalledBy: uuidToPtr(x.InstalledBy),
		InstalledAt: timestampToString(x.InstalledAt), RemovedAt: timestampToPtr(x.RemovedAt), ItemCount: items, Metric: m.Metric, Domain: m.Domain, UpgradeTo: upgradeTo, BundleSha256: x.BundleSha256,
	}
}

// packBundle turns a parsed pack into a transfer bundle, with the manifest
// set and the guarantees a pack makes enforced whatever the file says.
func packBundle(p *packs.Pack) (*transferBundle, error) {
	b := newTransferBundle()
	if err := json.Unmarshal(p.Body, b); err != nil {
		return nil, fmt.Errorf("pack %s: body: %w", p.Manifest.ID, err)
	}
	for i := range b.Autopilots {
		for j := range b.Autopilots[i].Triggers {
			b.Autopilots[i].Triggers[j].Enabled = false
			b.Autopilots[i].Triggers[j].HadSecret = false
		}
	}
	for i := range b.Agents {
		b.Agents[i].EnvKeys = nil
		b.Agents[i].ScopedEnvKeys = nil
		if b.Agents[i].TrustMode == "" {
			b.Agents[i].TrustMode = "propose"
		}
	}
	for i := range b.Labels {
		if b.Labels[i].ResourceType == "" {
			b.Labels[i].ResourceType = "issue"
		}
	}
	m := p.Manifest
	b.Manifest = transferManifest{FormatVersion: transferFormatVersion, ExportedAt: time.Now().UTC().Format(time.RFC3339), Name: m.Title, Template: true, Counts: transferCounts(b), Secrets: []transferSecret{}, Pack: &m}
	if p.Builtin {
		b.Manifest.Source.Name = "Multica pack catalogue"
	} else {
		b.Manifest.Source.Name = "pack " + m.ID
	}
	b.Manifest.Source.Slug = m.ID
	return b, nil
}

func packContents(b *transferBundle) PackContents {
	out := PackContents{}
	addAll := func(kind string, names ...string) {
		if len(names) > 0 {
			out[kind] = append(out[kind], names...)
		}
	}
	for _, x := range b.IssueStatuses {
		addAll("issue_statuses", x.Name)
	}
	for _, x := range b.IssueTypes {
		addAll("issue_types", x.Name)
	}
	for _, x := range b.Labels {
		addAll("labels", x.Name)
	}
	for _, x := range b.Properties {
		addAll("properties", x.Name)
	}
	for _, x := range b.Views {
		addAll("views", x.Name)
	}
	for _, x := range b.TransitionRules {
		addAll("transition_rules", transitionRuleName(x))
	}
	for _, x := range b.BusinessRules {
		addAll("business_rules", x.Title)
	}
	for _, x := range b.OwnershipRules {
		addAll("ownership_rules", ownershipRuleName(x))
	}
	for _, x := range b.Profiles {
		addAll("permission_profiles", x.Name)
	}
	for _, x := range b.Skills {
		addAll("skills", x.Name)
	}
	for _, x := range b.Agents {
		addAll("agents", x.Name)
	}
	for _, x := range b.Projects {
		addAll("projects", x.Title)
	}
	for _, x := range b.Goals {
		addAll("goals", x.Title)
	}
	for _, x := range b.Autopilots {
		addAll("autopilots", x.Title)
	}
	for _, x := range b.TriageSources {
		addAll("triage_sources", x.Name)
	}
	for _, x := range b.Org {
		addAll("org_structures", x.Name)
	}
	for _, x := range b.Notes {
		addAll("notes", x.Title)
	}
	for _, x := range b.Issues {
		addAll("issues", x.Title)
	}
	if strings.TrimSpace(b.Doctrine) != "" {
		addAll("doctrine", "Doctrine section")
	}
	return out
}

// packPrerequisites reports what the server can verify: the native runtime
// and connected runtimes are known; integrations and channels are declared
// and left to the person, marked unknown.
func (h *Handler) packPrerequisites(ctx context.Context, wsUUID pgtype.UUID, m packs.Manifest) []PackPrerequisite {
	out := make([]PackPrerequisite, 0, len(m.Prerequisites))
	nativeOK := h.NativeAgents != nil && h.NativeAgents.Available()
	runtimes := -1
	for _, p := range m.Prerequisites {
		status := "unknown"
		switch p.Kind {
		case "native_runtime":
			status = "missing"
			if nativeOK {
				status = "met"
			}
		case "runtime":
			if runtimes < 0 {
				runtimes = 0
				if rows, err := h.Queries.ListAgentRuntimes(ctx, wsUUID); err == nil {
					runtimes = len(rows)
				}
			}
			status = "missing"
			if runtimes > 0 || nativeOK {
				status = "met"
			}
		}
		out = append(out, PackPrerequisite{Prerequisite: p, Status: status})
	}
	return out
}

func (h *Handler) packSummary(ctx context.Context, wsUUID pgtype.UUID, p *packs.Pack, b *transferBundle) PackSummary {
	s := PackSummary{Manifest: p.Manifest, Counts: transferCounts(b), Builtin: p.Builtin, Prerequisites: h.packPrerequisites(ctx, wsUUID, p.Manifest)}
	if installed, err := h.Queries.GetInstalledPack(ctx, db.GetInstalledPackParams{WorkspaceID: wsUUID, PackID: p.Manifest.ID}); err == nil {
		v, id := installed.PackVersion, uuidToString(installed.ID)
		s.InstalledVersion, s.InstallID = &v, &id
		s.Upgrade = packs.CompareVersions(p.Manifest.Version, installed.PackVersion) > 0
	}
	return s
}

func (h *Handler) requirePackManager(w http.ResponseWriter, r *http.Request) (pgtype.UUID, db.Member, bool) {
	wsRaw := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsRaw, "workspace id")
	if !ok {
		return pgtype.UUID{}, db.Member{}, false
	}
	member, ok := h.requireWorkspaceRole(w, r, wsRaw, "workspace not found", "owner", "admin")
	if !ok {
		return pgtype.UUID{}, db.Member{}, false
	}
	return wsUUID, member, true
}

// GET /api/packs — the catalogue, with this workspace's install state.
func (h *Handler) ListPacks(w http.ResponseWriter, r *http.Request) {
	wsRaw := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsRaw, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, wsRaw, "workspace not found"); !ok {
		return
	}
	all, err := packs.Builtin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "catalogue unavailable: "+err.Error())
		return
	}
	out := make([]PackSummary, 0, len(all))
	for _, p := range all {
		b, err := packBundle(p)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, h.packSummary(r.Context(), wsUUID, p, b))
	}
	writeJSON(w, http.StatusOK, struct {
		Packs   []PackSummary `json:"packs"`
		Domains []string      `json:"domains"`
	}{out, packs.Domains})
}

// GET /api/pack-catalogue — the catalogue without any workspace context, for
// the workspace creation flow: manifests, counts and contents, no install
// state and no prerequisite check (there is no workspace to check against).
func (h *Handler) ListPackCatalogue(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	all, err := packs.Builtin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "catalogue unavailable: "+err.Error())
		return
	}
	type entry struct {
		Manifest packs.Manifest `json:"manifest"`
		Counts   map[string]int `json:"counts"`
		Contents PackContents   `json:"contents"`
	}
	out := make([]entry, 0, len(all))
	for _, p := range all {
		b, err := packBundle(p)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, entry{Manifest: p.Manifest, Counts: transferCounts(b), Contents: packContents(b)})
	}
	writeJSON(w, http.StatusOK, struct {
		Packs   []entry  `json:"packs"`
		Domains []string `json:"domains"`
	}{out, packs.Domains})
}

// GET /api/packs/{id} — one catalogue pack with its contents and description.
func (h *Handler) GetPack(w http.ResponseWriter, r *http.Request) {
	wsRaw := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsRaw, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, wsRaw, "workspace not found"); !ok {
		return
	}
	p, err := packs.FindBuiltin(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "pack not found")
		return
	}
	b, err := packBundle(p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Pack     PackSummary  `json:"pack"`
		Contents PackContents `json:"contents"`
		Source   string       `json:"source"`
	}{h.packSummary(r.Context(), wsUUID, p, b), packContents(b), string(p.Source)})
}

// GET /api/packs/{id}/download — the catalogue file itself.
func (h *Handler) DownloadPack(w http.ResponseWriter, r *http.Request) {
	wsRaw := h.resolveWorkspaceID(r)
	if _, ok := h.requireWorkspaceMember(w, r, wsRaw, "workspace not found"); !ok {
		return
	}
	p, err := packs.FindBuiltin(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "pack not found")
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.pack.yaml"`, p.Manifest.ID, p.Manifest.Version))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(p.Source)
}

type packPreview struct {
	Pack       PackSummary          `json:"pack"`
	Contents   PackContents         `json:"contents"`
	Collisions []transferCollision  `json:"collisions"`
	Problems   []string             `json:"problems"`
	Strategies []string             `json:"strategies"`
	Strategy   string               `json:"strategy"`
	Installed  *PackInstallResponse `json:"installed"`
	Blocked    string               `json:"blocked"`
}

// defaultPackStrategy is skip on a first install (a pack never overwrites
// what a workspace already has) and merge on an upgrade (the pack's own
// rows follow the new version).
func (h *Handler) packPreviewFor(ctx context.Context, wsUUID pgtype.UUID, p *packs.Pack, b *transferBundle, requested string) (packPreview, error) {
	preview := packPreview{Pack: h.packSummary(ctx, wsUUID, p, b), Contents: packContents(b), Collisions: h.transferCollisions(ctx, wsUUID, b), Problems: validateTransferBundle(b), Strategies: transferStrategies}
	if preview.Problems == nil {
		preview.Problems = []string{}
	}
	strategy := transferStrategySkip
	if installed, err := h.Queries.GetInstalledPack(ctx, db.GetInstalledPackParams{WorkspaceID: wsUUID, PackID: p.Manifest.ID}); err == nil {
		items, _ := h.Queries.ListPackItems(ctx, db.ListPackItemsParams{InstallID: installed.ID, WorkspaceID: wsUUID})
		resp := packInstallToResponse(installed, len(items), nil)
		preview.Installed = &resp
		switch packs.CompareVersions(p.Manifest.Version, installed.PackVersion) {
		case 0:
			preview.Blocked = fmt.Sprintf("version %s is already installed", installed.PackVersion)
		case -1:
			preview.Blocked = fmt.Sprintf("version %s is installed; a pack does not downgrade", installed.PackVersion)
		default:
			strategy = transferStrategyMerge
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return preview, err
	}
	if requested != "" {
		if !containsStr(transferStrategies, requested) {
			return preview, errors.New("strategy must be one of: rename, merge, skip")
		}
		strategy = requested
	}
	preview.Strategy = strategy
	return preview, nil
}

// POST /api/packs/{id}/preview {strategy?}
func (h *Handler) PreviewPack(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.requirePackManager(w, r)
	if !ok {
		return
	}
	p, err := packs.FindBuiltin(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "pack not found")
		return
	}
	var req struct {
		Strategy string `json:"strategy"`
	}
	if r.ContentLength != 0 {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req)
	}
	b, err := packBundle(p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	preview, err := h.packPreviewFor(r.Context(), wsUUID, p, b, req.Strategy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

// installPack applies a pack and writes the ledger. `force` re-applies an
// already installed version (the transfer stays idempotent under skip).
func (h *Handler) installPack(ctx context.Context, wsUUID pgtype.UUID, p *packs.Pack, source, strategy string, importer pgtype.UUID, force bool) (db.WorkspacePackInstall, transferReport, error) {
	b, err := packBundle(p)
	if err != nil {
		return db.WorkspacePackInstall{}, transferReport{}, packFail(http.StatusBadRequest, err.Error())
	}
	if problems := validateTransferBundle(b); len(problems) > 0 {
		return db.WorkspacePackInstall{}, transferReport{}, packFail(http.StatusBadRequest, "the pack is not valid: "+strings.Join(problems, "; "))
	}
	preview, err := h.packPreviewFor(ctx, wsUUID, p, b, strategy)
	if err != nil {
		return db.WorkspacePackInstall{}, transferReport{}, packFail(http.StatusBadRequest, err.Error())
	}
	if preview.Blocked != "" && !force {
		return db.WorkspacePackInstall{}, transferReport{}, packFail(http.StatusConflict, preview.Blocked)
	}
	strategy = preview.Strategy
	data, _ := json.Marshal(b)
	report, runID, err := h.importTransferBundle(ctx, wsUUID, b, strategy, map[string]map[string]string{}, importer, data)
	if err != nil {
		return db.WorkspacePackInstall{}, report, err
	}
	sum := sha256.Sum256(p.Source)
	manifestJSON, _ := json.Marshal(p.Manifest)
	reportJSON, _ := json.Marshal(report)
	if err := h.Queries.MarkPackInstallSuperseded(ctx, db.MarkPackInstallSupersededParams{WorkspaceID: wsUUID, PackID: p.Manifest.ID, RemovedBy: importer}); err != nil {
		return db.WorkspacePackInstall{}, report, err
	}
	install, err := h.Queries.CreatePackInstall(ctx, db.CreatePackInstallParams{ID: dbid.NewV7(), WorkspaceID: wsUUID, PackID: p.Manifest.ID, PackVersion: p.Manifest.Version, Title: p.Manifest.Title, Source: source, Strategy: strategy, BundleSha256: hex.EncodeToString(sum[:]), RunID: parseUUID(runID), Manifest: manifestJSON, InstalledBy: importer})
	if err != nil {
		return db.WorkspacePackInstall{}, report, err
	}
	// Carry the previous install's rows forward on an upgrade: what an older
	// version created and this one skipped still belongs to the pack.
	seen := map[string]bool{}
	for _, it := range report.Items {
		seen[it.Kind+":"+it.ID] = true
		// The ledger is what uninstall walks: an item that is not recorded
		// can never be removed again.
		if err := h.Queries.CreatePackItem(ctx, db.CreatePackItemParams{ID: dbid.NewV7(), InstallID: install.ID, WorkspaceID: wsUUID, Kind: it.Kind, RowID: parseUUID(it.ID), Name: it.Name, Action: it.Action}); err != nil {
			slog.Error("pack install: ledger write failed", "install_id", uuidToString(install.ID), "kind", it.Kind, "row_id", it.ID, "error", err)
		}
	}
	if preview.Installed != nil {
		if prev, err := h.Queries.ListPackItems(ctx, db.ListPackItemsParams{InstallID: parseUUID(preview.Installed.ID), WorkspaceID: wsUUID}); err == nil {
			for _, it := range prev {
				key := it.Kind + ":" + uuidToString(it.RowID)
				if seen[key] || it.Action == "skipped" {
					continue
				}
				if err := h.Queries.CreatePackItem(ctx, db.CreatePackItemParams{ID: dbid.NewV7(), InstallID: install.ID, WorkspaceID: wsUUID, Kind: it.Kind, RowID: it.RowID, Name: it.Name, Action: it.Action}); err != nil {
					slog.Error("pack install: ledger carry-over failed", "install_id", uuidToString(install.ID), "kind", it.Kind, "error", err)
				}
			}
		}
	}
	install, err = h.Queries.FinishPackInstall(ctx, db.FinishPackInstallParams{ID: install.ID, WorkspaceID: wsUUID, Status: "installed", Report: reportJSON})
	if err != nil {
		return db.WorkspacePackInstall{}, report, err
	}
	h.audit(ctx, wsUUID, "member", uuidToString(importer), AuditPackInstalled, "workspace_pack_install", install.ID, map[string]any{"pack_id": p.Manifest.ID, "version": p.Manifest.Version, "source": source, "strategy": strategy, "created": report.Created, "merged": report.Merged, "skipped": len(report.Skipped)}, nil)
	h.publish(protocol.EventPackChanged, uuidToString(wsUUID), "member", uuidToString(importer), map[string]any{"pack_id": p.Manifest.ID, "version": p.Manifest.Version, "change": "installed"})
	return install, report, nil
}

type packError struct {
	status int
	msg    string
}

func (e *packError) Error() string { return e.msg }

func packFail(status int, msg string) error { return &packError{status: status, msg: msg} }

func writePackError(w http.ResponseWriter, err error) {
	var pe *packError
	if errors.As(err, &pe) {
		writeError(w, pe.status, pe.msg)
		return
	}
	writeError(w, http.StatusInternalServerError, "pack install failed: "+err.Error())
}

func (h *Handler) writePackInstallResult(w http.ResponseWriter, ctx context.Context, wsUUID pgtype.UUID, install db.WorkspacePackInstall, report transferReport) {
	items, _ := h.Queries.ListPackItems(ctx, db.ListPackItemsParams{InstallID: install.ID, WorkspaceID: wsUUID})
	writeJSON(w, http.StatusOK, struct {
		Install PackInstallResponse `json:"install"`
		Report  transferReport      `json:"report"`
	}{packInstallToResponse(install, len(items), nil), report})
}

// POST /api/packs/{id}/install {strategy?, force?}
func (h *Handler) InstallPack(w http.ResponseWriter, r *http.Request) {
	wsUUID, member, ok := h.requirePackManager(w, r)
	if !ok {
		return
	}
	p, err := packs.FindBuiltin(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusNotFound, "pack not found")
		return
	}
	var req struct {
		Strategy string `json:"strategy"`
		Force    bool   `json:"force"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	install, report, err := h.installPack(r.Context(), wsUUID, p, packSourceBuiltin, req.Strategy, member.UserID, req.Force)
	if err != nil {
		writePackError(w, err)
		return
	}
	h.writePackInstallResult(w, r.Context(), wsUUID, install, report)
}

func (h *Handler) readPackUpload(w http.ResponseWriter, r *http.Request) (*packs.Pack, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, packs.MaxFileBytes+64<<10)
	if err := r.ParseMultipartForm(packs.MaxFileBytes); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart upload or file exceeds the size limit")
		return nil, false
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, `a pack.yaml file is required (form field "file")`)
		return nil, false
	}
	defer file.Close()
	raw := make([]byte, 0, 64<<10)
	buf := make([]byte, 32<<10)
	for {
		n, err := file.Read(buf)
		raw = append(raw, buf[:n]...)
		if err != nil {
			break
		}
		if len(raw) > packs.MaxFileBytes {
			writeError(w, http.StatusBadRequest, "pack file exceeds the size limit")
			return nil, false
		}
	}
	p, err := packs.Parse(raw)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	return p, true
}

// POST /api/packs/preview (multipart: file, strategy?)
func (h *Handler) PreviewPackUpload(w http.ResponseWriter, r *http.Request) {
	wsUUID, _, ok := h.requirePackManager(w, r)
	if !ok {
		return
	}
	p, ok := h.readPackUpload(w, r)
	if !ok {
		return
	}
	b, err := packBundle(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	preview, err := h.packPreviewFor(r.Context(), wsUUID, p, b, r.FormValue("strategy"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, preview)
}

// POST /api/packs/install (multipart: file, strategy?, force?)
func (h *Handler) InstallPackUpload(w http.ResponseWriter, r *http.Request) {
	wsUUID, member, ok := h.requirePackManager(w, r)
	if !ok {
		return
	}
	p, ok := h.readPackUpload(w, r)
	if !ok {
		return
	}
	install, report, err := h.installPack(r.Context(), wsUUID, p, packSourceUpload, r.FormValue("strategy"), member.UserID, r.FormValue("force") == "true")
	if err != nil {
		writePackError(w, err)
		return
	}
	h.writePackInstallResult(w, r.Context(), wsUUID, install, report)
}

// GET /api/packs/installed
func (h *Handler) ListPackInstalls(w http.ResponseWriter, r *http.Request) {
	wsRaw := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsRaw, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, wsRaw, "workspace not found"); !ok {
		return
	}
	rows, err := h.Queries.ListPackInstalls(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list installed packs")
		return
	}
	catalogue := map[string]string{}
	if all, err := packs.Builtin(); err == nil {
		for _, p := range all {
			catalogue[p.Manifest.ID] = p.Manifest.Version
		}
	}
	out := make([]PackInstallResponse, 0, len(rows))
	for _, x := range rows {
		items, _ := h.Queries.ListPackItems(r.Context(), db.ListPackItemsParams{InstallID: x.ID, WorkspaceID: wsUUID})
		var upgrade *string
		if v, ok := catalogue[x.PackID]; ok && x.Status == "installed" && x.Source == packSourceBuiltin && packs.CompareVersions(v, x.PackVersion) > 0 {
			upgrade = &v
		}
		out = append(out, packInstallToResponse(x, len(items), upgrade))
	}
	writeJSON(w, http.StatusOK, struct {
		Installs []PackInstallResponse `json:"installs"`
	}{out})
}

// GET /api/packs/installed/{id}
func (h *Handler) GetPackInstall(w http.ResponseWriter, r *http.Request) {
	wsRaw := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsRaw, "workspace id")
	if !ok {
		return
	}
	if _, ok := h.requireWorkspaceMember(w, r, wsRaw, "workspace not found"); !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "install id")
	if !ok {
		return
	}
	install, err := h.Queries.GetPackInstall(r.Context(), db.GetPackInstallParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "install not found")
		return
	}
	items, _ := h.Queries.ListPackItems(r.Context(), db.ListPackItemsParams{InstallID: install.ID, WorkspaceID: wsUUID})
	list := make([]transferItem, 0, len(items))
	for _, it := range items {
		list = append(list, transferItem{Kind: it.Kind, Name: it.Name, ID: uuidToString(it.RowID), Action: it.Action})
	}
	writeJSON(w, http.StatusOK, struct {
		Install PackInstallResponse `json:"install"`
		Items   []transferItem      `json:"items"`
	}{packInstallToResponse(install, len(items), nil), list})
}

type packUninstallReport struct {
	Removed map[string]int `json:"removed"`
	Kept    []transferItem `json:"kept"`
	Reasons []string       `json:"reasons"`
}

// POST /api/packs/installed/{id}/uninstall — removes the configuration the
// pack created (agents and autopilots archived; rules, views, labels,
// properties, statuses and types removed or archived when nothing uses
// them). Content stays: projects, goals, notes and issues are the
// workspace's now, as is anything the pack merged into an existing row.
func (h *Handler) UninstallPack(w http.ResponseWriter, r *http.Request) {
	wsUUID, member, ok := h.requirePackManager(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "install id")
	if !ok {
		return
	}
	install, err := h.Queries.GetPackInstall(r.Context(), db.GetPackInstallParams{ID: id, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusNotFound, "install not found")
		return
	}
	if install.Status != "installed" {
		writeError(w, http.StatusConflict, "this pack is not installed")
		return
	}
	items, err := h.Queries.ListPackItems(r.Context(), db.ListPackItemsParams{InstallID: install.ID, WorkspaceID: wsUUID})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read the pack's rows")
		return
	}
	report := h.uninstallPackItems(r.Context(), wsUUID, member.UserID, items)
	raw, _ := json.Marshal(report)
	updated, err := h.Queries.MarkPackInstallRemoved(r.Context(), db.MarkPackInstallRemovedParams{ID: install.ID, WorkspaceID: wsUUID, RemovedBy: member.UserID, Report: raw})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record the uninstall")
		return
	}
	h.audit(r.Context(), wsUUID, "member", uuidToString(member.UserID), AuditPackUninstalled, "workspace_pack_install", install.ID, map[string]any{"pack_id": install.PackID, "version": install.PackVersion, "removed": report.Removed, "kept": len(report.Kept)}, nil)
	h.publish(protocol.EventPackChanged, uuidToString(wsUUID), "member", uuidToString(member.UserID), map[string]any{"pack_id": install.PackID, "version": install.PackVersion, "change": "uninstalled"})
	writeJSON(w, http.StatusOK, struct {
		Install PackInstallResponse `json:"install"`
		Report  packUninstallReport `json:"report"`
	}{packInstallToResponse(updated, len(items), nil), report})
}

// uninstallOrder is the reverse of the apply order: dependants first.
var uninstallOrder = []string{"ownership_rules", "views", "transition_rules", "business_rules", "autopilots", "org_structures", "agents", "skills", "permission_profiles", "properties", "labels", "issue_types", "issue_statuses", "triage_sources", "doctrine", "projects", "goals", "notes", "issues"}

func (h *Handler) uninstallPackItems(ctx context.Context, wsUUID, actor pgtype.UUID, items []db.WorkspacePackItem) packUninstallReport {
	report := packUninstallReport{Removed: map[string]int{}, Kept: []transferItem{}, Reasons: []string{}}
	byKind := map[string][]db.WorkspacePackItem{}
	for _, it := range items {
		byKind[it.Kind] = append(byKind[it.Kind], it)
	}
	keep := func(it db.WorkspacePackItem, reason string) {
		report.Kept = append(report.Kept, transferItem{Kind: it.Kind, Name: it.Name, ID: uuidToString(it.RowID), Action: it.Action})
		if reason != "" {
			report.Reasons = append(report.Reasons, it.Kind+" "+it.Name+": "+reason)
		}
	}
	for _, kind := range uninstallOrder {
		for _, it := range byKind[kind] {
			if it.Action != "created" {
				keep(it, "merged into an existing row")
				continue
			}
			var err error
			removed := true
			switch kind {
			case "ownership_rules":
				_, err = h.Queries.DeleteModuleOwnership(ctx, db.DeleteModuleOwnershipParams{ID: it.RowID, WorkspaceID: wsUUID})
			case "views":
				_, err = h.Queries.DeleteIssueView(ctx, db.DeleteIssueViewParams{ID: it.RowID, WorkspaceID: wsUUID})
			case "transition_rules":
				if err = h.Queries.DeleteIssueTransitionRuleActors(ctx, it.RowID); err == nil {
					_, err = h.Queries.DeleteIssueTransitionRule(ctx, db.DeleteIssueTransitionRuleParams{ID: it.RowID, WorkspaceID: wsUUID})
				}
			case "business_rules":
				err = h.Queries.DeleteBusinessRule(ctx, db.DeleteBusinessRuleParams{ID: it.RowID, WorkspaceID: wsUUID})
			case "autopilots":
				// These three queries are not workspace-scoped: check the row first.
				if ap, gerr := h.Queries.GetAutopilot(ctx, it.RowID); gerr != nil || ap.WorkspaceID != wsUUID {
					err = fmt.Errorf("autopilot %s is not in this workspace", uuidToString(it.RowID))
				} else {
					err = h.Queries.ArchiveAutopilot(ctx, it.RowID)
				}
			case "agents":
				if ag, gerr := h.Queries.GetAgent(ctx, it.RowID); gerr != nil || ag.WorkspaceID != wsUUID {
					err = fmt.Errorf("agent %s is not in this workspace", uuidToString(it.RowID))
				} else {
					_, err = h.Queries.ArchiveAgent(ctx, db.ArchiveAgentParams{ID: it.RowID, ArchivedBy: actor})
				}
			case "skills":
				if err = h.Queries.DeleteSkillFilesBySkill(ctx, it.RowID); err == nil {
					err = h.Queries.DeleteSkill(ctx, db.DeleteSkillParams{ID: it.RowID, WorkspaceID: wsUUID})
				}
			case "permission_profiles":
				if pp, gerr := h.Queries.GetPermissionProfile(ctx, it.RowID); gerr != nil || pp.WorkspaceID != wsUUID {
					err = fmt.Errorf("permission profile %s is not in this workspace", uuidToString(it.RowID))
				} else {
					_, err = h.Queries.DeletePermissionProfile(ctx, it.RowID)
				}
			case "properties":
				_, err = h.Queries.UpdateIssueProperty(ctx, db.UpdateIssuePropertyParams{ID: it.RowID, WorkspaceID: wsUUID, ArchivedSet: true, ArchivedAt: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true}})
			case "labels":
				if err = h.Queries.DeleteIssueLabelAssignmentsByLabel(ctx, it.RowID); err == nil {
					_, err = h.Queries.DeleteLabel(ctx, db.DeleteLabelParams{ID: it.RowID, WorkspaceID: wsUUID})
				}
			case "issue_types":
				row, lookupErr := h.Queries.GetIssueTypeEntryByID(ctx, db.GetIssueTypeEntryByIDParams{ID: it.RowID, WorkspaceID: wsUUID})
				if lookupErr != nil {
					removed = false
					keep(it, "already gone")
					continue
				}
				if n, _ := h.Queries.CountIssuesUsingIssueType(ctx, db.CountIssuesUsingIssueTypeParams{WorkspaceID: wsUUID, Key: row.Key}); n > 0 {
					removed = false
					keep(it, fmt.Sprintf("%d issues use it; archive it from Settings when they are done", n))
					continue
				}
				_, err = h.Queries.ArchiveIssueTypeEntry(ctx, db.ArchiveIssueTypeEntryParams{ID: it.RowID, WorkspaceID: wsUUID})
			case "issue_statuses":
				row, lookupErr := h.Queries.GetIssueStatusEntryByID(ctx, db.GetIssueStatusEntryByIDParams{ID: it.RowID, WorkspaceID: wsUUID})
				if lookupErr != nil {
					removed = false
					keep(it, "already gone")
					continue
				}
				if n, _ := h.Queries.CountIssuesUsingStatusKey(ctx, db.CountIssuesUsingStatusKeyParams{WorkspaceID: wsUUID, Key: row.Key}); n > 0 {
					removed = false
					keep(it, fmt.Sprintf("%d issues are on it; archive it from Settings when they have moved", n))
					continue
				}
				_, err = h.Queries.ArchiveIssueStatusEntry(ctx, db.ArchiveIssueStatusEntryParams{ID: it.RowID, WorkspaceID: wsUUID})
			default:
				removed = false
				keep(it, "content stays in the workspace")
				continue
			}
			if err != nil {
				keep(it, err.Error())
				continue
			}
			if removed {
				report.Removed[kind]++
			}
		}
	}
	return report
}

// packExportRequest is the manifest a workspace export becomes a pack with.
type packExportRequest struct {
	Manifest      packs.Manifest `json:"manifest"`
	IncludeIssues bool           `json:"include_issues"`
	IncludeNotes  bool           `json:"include_notes"`
	// IncludeSkills defaults to true; nil means unset.
	IncludeSkills *bool `json:"include_skills"`
}

// POST /api/packs/export {manifest, include_issues, include_notes} — the
// workspace's configuration as a pack.yaml a person can edit and share.
func (h *Handler) ExportPack(w http.ResponseWriter, r *http.Request) {
	wsUUID, member, ok := h.requirePackManager(w, r)
	if !ok {
		return
	}
	var req packExportRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := req.Manifest.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ws, err := h.Queries.GetWorkspace(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusNotFound, "workspace not found")
		return
	}
	// Skills discovered on a connected computer never travel in a pack: they
	// are that machine's, and the file would be tens of megabytes of them.
	b, err := h.buildTransferBundle(r.Context(), ws, transferExportOptions{IncludeIssues: req.IncludeIssues, IncludeNotes: req.IncludeNotes, Template: true, Name: req.Manifest.Title, SkipSkills: req.IncludeSkills != nil && !*req.IncludeSkills, SkipMachineSkills: true})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export failed: "+err.Error())
		return
	}
	raw, err := packYAML(req.Manifest, b)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "export failed: "+err.Error())
		return
	}
	h.audit(r.Context(), wsUUID, "member", uuidToString(member.UserID), AuditWorkspaceExported, "workspace", wsUUID, map[string]any{"pack_id": req.Manifest.ID, "version": req.Manifest.Version, "counts": b.Manifest.Counts, "format": "pack"}, nil)
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.pack.yaml"`, req.Manifest.ID, req.Manifest.Version))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

// packYAML renders a bundle as a pack file: the manifest first, then the
// body kinds in FORMAT.md order, empty kinds omitted.
func packYAML(m packs.Manifest, b *transferBundle) ([]byte, error) {
	body, err := json.Marshal(b)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, err
	}
	delete(doc, "manifest")
	for k, v := range doc {
		switch x := v.(type) {
		case []any:
			if len(x) == 0 {
				delete(doc, k)
			}
		case string:
			if strings.TrimSpace(x) == "" {
				delete(doc, k)
			}
		case nil:
			delete(doc, k)
		}
	}
	manifestJSON, _ := json.Marshal(m)
	var manifestDoc map[string]any
	_ = json.Unmarshal(manifestJSON, &manifestDoc)
	order := []string{"issue_statuses", "issue_types", "labels", "properties", "views", "transition_rules", "business_rules", "doctrine", "ownership_rules", "permission_profiles", "skills", "agents", "projects", "goals", "autopilots", "triage_sources", "org_structures", "notes", "issues"}
	node := &yaml.Node{Kind: yaml.MappingNode}
	appendKV := func(key string, value any) error {
		node.Content = append(node.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, toYAMLNode(value))
		return nil
	}
	if err := appendKV("pack", manifestDoc); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	for _, k := range order {
		if v, ok := doc[k]; ok {
			if err := appendKV(k, v); err != nil {
				return nil, err
			}
			seen[k] = true
		}
	}
	rest := make([]string, 0)
	for k := range doc {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	for _, k := range rest {
		if err := appendKV(k, doc[k]); err != nil {
			return nil, err
		}
	}
	var out strings.Builder
	out.WriteString("# Multica pack — see FORMAT.md. Nothing here carries a credential.\n")
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if err := enc.Encode(node); err != nil {
		return nil, err
	}
	_ = enc.Close()
	// The file has to read back: a pack nobody can import is not an export.
	var probe map[string]any
	if err := yaml.Unmarshal([]byte(out.String()), &probe); err != nil {
		return nil, fmt.Errorf("the export does not read back as YAML: %w", err)
	}
	return []byte(out.String()), nil
}

// toYAMLNode builds the node tree by hand, in key order, choosing the scalar
// style itself. yaml.v3's own encoder emits a multi-line string whose first
// line starts with a space as a literal block without its indentation
// indicator, which no parser (its own included) reads back; those strings
// are double-quoted instead.
func toYAMLNode(v any) *yaml.Node {
	switch x := v.(type) {
	case nil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	case map[string]any:
		n := &yaml.Node{Kind: yaml.MappingNode}
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, toYAMLNode(x[k]))
		}
		return n
	case []any:
		n := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range x {
			n.Content = append(n.Content, toYAMLNode(item))
		}
		return n
	case string:
		n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: x}
		if strings.ContainsAny(x, "\r\t\x00") || strings.ContainsAny(x, "\x01\x02\x03\x04\x05\x06\x07\x08\x0b\x0c\x0e\x0f") {
			n.Style = yaml.DoubleQuotedStyle
		} else if strings.Contains(x, "\n") {
			first := strings.SplitN(x, "\n", 2)[0]
			if strings.HasPrefix(first, " ") || strings.HasSuffix(x, " ") || strings.Contains(x, " \n") {
				n.Style = yaml.DoubleQuotedStyle
			} else {
				n.Style = yaml.LiteralStyle
			}
		} else if x == "" || x != strings.TrimSpace(x) {
			n.Style = yaml.DoubleQuotedStyle
		}
		return n
	case bool:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprint(x)}
	case float64:
		if x == float64(int64(x)) {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprintf("%d", int64(x))}
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: fmt.Sprint(x)}
	case json.Number:
		return &yaml.Node{Kind: yaml.ScalarNode, Value: x.String()}
	default:
		var n yaml.Node
		if err := n.Encode(v); err == nil {
			return &n
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: fmt.Sprint(v), Style: yaml.DoubleQuotedStyle}
	}
}

// applyWorkspacePack seeds a freshly created workspace from a catalogue
// pack. Like templates, a failure leaves the workspace and says so.
func (h *Handler) applyWorkspacePack(ctx context.Context, wsUUID pgtype.UUID, packID string, userID string) (map[string]any, error) {
	p, err := packs.FindBuiltin(packID)
	if err != nil {
		return nil, err
	}
	install, report, err := h.installPack(ctx, wsUUID, p, packSourceBuiltin, "", parseUUID(userID), false)
	if err != nil {
		return nil, err
	}
	return map[string]any{"pack_id": p.Manifest.ID, "version": p.Manifest.Version, "install_id": uuidToString(install.ID), "report": report}, nil
}
