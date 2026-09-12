package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Packs (OS plan, vague B) from the terminal: the catalogue, what one pack
// contains, what installing it would collide with, the install itself, the
// ledger it writes, an uninstall, and the two file-based paths (upload a
// pack.yaml, export this workspace as one).
//
// Every write here is human-only on the server (RequireHumanActor), so an
// agent running these commands gets a 403 rather than a workspace it
// reconfigured by itself.

const packsPath = "/api/packs"

// packKindOrder is the order kinds are printed in, matching FORMAT.md's file
// layout so `pack show` reads like the pack file. Unknown kinds print after,
// alphabetically, instead of disappearing.
var packKindOrder = []string{
	"issue_statuses", "issue_types", "labels", "properties", "views", "transition_rules",
	"business_rules", "doctrine", "ownership_rules", "permission_profiles", "skills", "agents",
	"projects", "goals", "autopilots", "triage_sources", "org_structures", "notes", "issues",
}

type packMetric struct {
	Label       string `json:"label"`
	Description string `json:"description"`
	Hint        string `json:"hint"`
}

type packPrerequisite struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Optional bool   `json:"optional"`
	Note     string `json:"note"`
	// Status is what the server could verify: met, missing or unknown.
	Status string `json:"status"`
}

type packManifest struct {
	ID                 string     `json:"id"`
	Version            string     `json:"version"`
	Title              string     `json:"title"`
	Summary            string     `json:"summary"`
	Description        string     `json:"description"`
	Domain             string     `json:"domain"`
	Wave               int        `json:"wave"`
	Author             string     `json:"author"`
	License            string     `json:"license"`
	Tags               []string   `json:"tags"`
	WorksWithoutAgents bool       `json:"works_without_agents"`
	Metric             packMetric `json:"metric"`
}

type packSummary struct {
	Manifest         packManifest       `json:"manifest"`
	Counts           map[string]int     `json:"counts"`
	Builtin          bool               `json:"builtin"`
	InstalledVersion *string            `json:"installed_version"`
	InstallID        *string            `json:"install_id"`
	UpgradeAvailable bool               `json:"upgrade_available"`
	Prerequisites    []packPrerequisite `json:"prerequisites"`
}

type packCollision struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	ExistingID string `json:"existing_id"`
}

type packItem struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	ID     string `json:"id"`
	Action string `json:"action"`
}

type packInstall struct {
	ID          string     `json:"id"`
	PackID      string     `json:"pack_id"`
	PackVersion string     `json:"pack_version"`
	Title       string     `json:"title"`
	Source      string     `json:"source"`
	Strategy    string     `json:"strategy"`
	Status      string     `json:"status"`
	InstalledAt string     `json:"installed_at"`
	RemovedAt   *string    `json:"removed_at"`
	ItemCount   int        `json:"item_count"`
	Domain      string     `json:"domain"`
	Metric      packMetric `json:"metric"`
	UpgradeTo   *string    `json:"upgrade_to"`
}

type packReport struct {
	Created  map[string]int  `json:"created"`
	Merged   map[string]int  `json:"merged"`
	Skipped  []packCollision `json:"skipped"`
	Warnings []string        `json:"warnings"`
	Items    []packItem      `json:"items"`
}

var packCmd = &cobra.Command{
	Use:   "pack",
	Short: "Function packs: a ready-to-use workspace setup for helpdesk, sales, HR…",
	Long: "A pack is one file that turns a workspace into a working setup for a function: work item types, " +
		"statuses, properties, views, rules, a doctrine section, a project, notes, and agents as paused colleagues.\n\n" +
		"Every pack works with no agent at all. Installing one never overwrites what the workspace already has " +
		"(the default strategy is skip), records every row it created, and can be upgraded or uninstalled.\n\n" +
		"Installing, uninstalling, previewing and exporting are owner/admin actions the server accepts from a " +
		"person only: an agent gets 403.",
}

var packListCmd = &cobra.Command{
	Use:   "list",
	Short: "The pack catalogue, with what this workspace already has installed",
	Args:  exactArgs(0),
	RunE:  runPackList,
}

var packShowCmd = &cobra.Command{
	Use:   "show <pack-id>",
	Short: "One pack: its manifest, its metric, its prerequisites and what it would create",
	Args:  exactArgs(1),
	RunE:  runPackShow,
}

var packPreviewCmd = &cobra.Command{
	Use:   "preview <pack-id>",
	Short: "What installing this pack would collide with, and under which strategy",
	Args:  exactArgs(1),
	RunE:  runPackPreview,
}

var packInstallCmd = &cobra.Command{
	Use:   "install <pack-id>",
	Short: "Install a catalogue pack (owner/admin, human only)",
	Long: "Applies the pack in one transaction and records every row it created.\n\n" +
		"The default strategy is `skip` on a first install and `merge` on an upgrade. " +
		"`rename` keeps both the workspace's row and the pack's. Re-installing the version already " +
		"installed is refused; --force re-applies it.",
	Args: exactArgs(1),
	RunE: runPackInstall,
}

var packInstalledCmd = &cobra.Command{
	Use:   "installed",
	Short: "The install ledger: which packs this workspace has, and what is upgradable",
	Args:  exactArgs(0),
	RunE:  runPackInstalled,
}

var packItemsCmd = &cobra.Command{
	Use:   "items <install-id>",
	Short: "Every row one install created or merged",
	Args:  exactArgs(1),
	RunE:  runPackItems,
}

var packUninstallCmd = &cobra.Command{
	Use:   "uninstall <install-id>",
	Short: "Remove the configuration a pack installed; its content stays",
	Long: "Agents and autopilots are archived; rules, views, labels, properties, statuses and types are " +
		"removed when nothing uses them. Projects, goals, notes, issues and anything the pack merged into " +
		"an existing row stay in the workspace, and the report says why each kept row was kept.",
	Args: exactArgs(1),
	RunE: runPackUninstall,
}

var packDownloadCmd = &cobra.Command{
	Use:   "download <pack-id>",
	Short: "Download a catalogue pack's pack.yaml",
	Args:  exactArgs(1),
	RunE:  runPackDownload,
}

var packPreviewFileCmd = &cobra.Command{
	Use:   "preview-file <pack.yaml>",
	Short: "Preview a pack file of your own without installing it",
	Args:  exactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runPackFile(cmd, args, false) },
}

var packInstallFileCmd = &cobra.Command{
	Use:   "install-file <pack.yaml>",
	Short: "Install a pack file of your own (owner/admin, human only)",
	Args:  exactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return runPackFile(cmd, args, true) },
}

var packExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export this workspace's configuration as a pack.yaml",
	Long: "Writes the workspace's configuration as a pack file you can edit, share and install elsewhere. " +
		"Nothing exported carries a credential: agents lose their env keys, autopilot triggers come back " +
		"disabled and triage sources come back without their token.",
	Args: exactArgs(0),
	RunE: runPackExport,
}

func init() {
	for _, c := range []*cobra.Command{packListCmd, packShowCmd, packPreviewCmd, packInstallCmd,
		packInstalledCmd, packItemsCmd, packUninstallCmd, packPreviewFileCmd, packInstallFileCmd} {
		c.Flags().String("output", "table", "Output format: table or json")
	}
	packListCmd.Flags().String("domain", "", "only packs of this domain (helpdesk, ops, support, sales, …)")
	for _, c := range []*cobra.Command{packPreviewCmd, packInstallCmd, packPreviewFileCmd, packInstallFileCmd} {
		c.Flags().String("strategy", "", "collision strategy: rename, merge or skip (default: skip, merge on an upgrade)")
	}
	for _, c := range []*cobra.Command{packInstallCmd, packInstallFileCmd} {
		c.Flags().Bool("force", false, "re-apply a version that is already installed")
	}
	for _, c := range []*cobra.Command{packInstalledCmd, packItemsCmd} {
		c.Flags().Bool("full-id", false, "print full ids")
	}
	packDownloadCmd.Flags().String("out", "", "write to this file (default: the name the server sends)")
	packExportCmd.Flags().String("id", "", "pack id, kebab-case (required)")
	packExportCmd.Flags().String("version", "1.0.0", "semver version")
	packExportCmd.Flags().String("title", "", "title (required)")
	packExportCmd.Flags().String("summary", "", "one sentence for the catalogue card (required)")
	packExportCmd.Flags().String("description", "", "markdown: what the pack sets up and who it is for")
	packExportCmd.Flags().String("domain", "other", "helpdesk, ops, support, sales, marketing, leadership, research, hr, finance, legal, engineering or other")
	packExportCmd.Flags().Int("wave", 0, "rollout wave 1, 2 or 3 (0 = unset)")
	packExportCmd.Flags().String("metric-label", "", "the one number this pack is judged on (required)")
	packExportCmd.Flags().String("metric-description", "", "how that number is measured (required)")
	packExportCmd.Flags().String("metric-hint", "", "where to read it in the workspace")
	packExportCmd.Flags().Bool("include-issues", false, "include issues as sample content")
	packExportCmd.Flags().Bool("include-notes", false, "include Brain notes")
	packExportCmd.Flags().Bool("include-skills", true, "include the workspace's skills (skills discovered on a connected computer are never exported)")
	packExportCmd.Flags().String("out", "", "write to this file (default: the name the server sends)")

	packCmd.AddCommand(packListCmd, packShowCmd, packPreviewCmd, packInstallCmd, packInstalledCmd,
		packItemsCmd, packUninstallCmd, packDownloadCmd, packPreviewFileCmd, packInstallFileCmd, packExportCmd)
}

// --- file transport ------------------------------------------------------
//
// The pack endpoints speak two shapes the shared APIClient does not: a
// multipart upload to an arbitrary path, and a POST that answers with a file
// rather than JSON. Both are built here on the client's own base URL and
// credentials so a failure still surfaces as *cli.HTTPError, which is what
// FormatError and the exit codes read.

// packHTTPError mirrors what the shared client returns for a >= 400, so
// FormatError and ExitCodeFor classify these failures like any other.
func packHTTPError(path string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return &cli.HTTPError{
		Method:     http.MethodPost,
		Path:       path,
		StatusCode: resp.StatusCode,
		Body:       strings.TrimSpace(string(body)),
		TaskScoped: strings.HasPrefix(resp.Request.Header.Get("Authorization"), "Bearer "+cli.TaskTokenPrefix),
	}
}

func packRequestHeaders(client *cli.APIClient, req *http.Request) {
	if client.Token != "" {
		req.Header.Set("Authorization", "Bearer "+client.Token)
	}
	if client.WorkspaceID != "" {
		req.Header.Set("X-Workspace-ID", client.WorkspaceID)
	}
}

// postMultipart uploads one file plus form fields and decodes the JSON answer.
func postMultipart(ctx context.Context, client *cli.APIClient, path, filename string, file []byte, fields map[string]string, out any) error {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	if _, err := part.Write(file); err != nil {
		return err
	}
	for k, v := range fields {
		if v == "" {
			continue
		}
		if err := w.WriteField(k, v); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.BaseURL+path, bytes.NewReader(body.Bytes()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	packRequestHeaders(client, req)
	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return packHTTPError(path, resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode the response of POST %s: %w", path, err)
	}
	return nil
}

// postFile posts a JSON body to an endpoint that answers with a file, and
// returns the bytes plus the filename the server suggested.
func postFile(ctx context.Context, client *cli.APIClient, path string, body any) ([]byte, string, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.BaseURL+path, bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	packRequestHeaders(client, req)
	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", packHTTPError(path, resp)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read the response of POST %s: %w", path, err)
	}
	return raw, attachmentFilename(resp.Header.Get("Content-Disposition")), nil
}

// attachmentFilename reads the name out of a Content-Disposition header.
// filepath.Base is not cosmetic: the name decides where a --out-less download
// is written, and a server (or a proxy) is not allowed to pick a path.
func attachmentFilename(header string) string {
	if header == "" {
		return ""
	}
	_, params, err := mime.ParseMediaType(header)
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(params["filename"])
	if name == "" || name == "." || name == ".." {
		return ""
	}
	base := filepath.Base(name)
	if base == "." || base == ".." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

// writeDownload writes a downloaded file to --out, or to the server's name in
// the working directory, and says where it landed.
func writeDownload(cmd *cobra.Command, data []byte, serverName, fallback string) error {
	out, _ := cmd.Flags().GetString("out")
	if out == "" {
		out = serverName
	}
	if out == "" {
		out = fallback
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	fmt.Fprintf(os.Stdout, "%s\t%d bytes\n", out, len(data))
	return nil
}

// --- catalogue -----------------------------------------------------------

func runPackList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var resp struct {
		Packs   []packSummary `json:"packs"`
		Domains []string      `json:"domains"`
	}
	if err := client.GetJSON(cmd.Context(), packsPath, &resp); err != nil {
		return fmt.Errorf("read the pack catalogue: %w", err)
	}
	if domain, _ := cmd.Flags().GetString("domain"); domain != "" {
		kept := make([]packSummary, 0, len(resp.Packs))
		for _, p := range resp.Packs {
			if p.Manifest.Domain == domain {
				kept = append(kept, p)
			}
		}
		if len(kept) == 0 {
			fmt.Fprintf(os.Stderr, "no pack in domain %q; the catalogue has: %s\n", domain, strings.Join(resp.Domains, ", "))
		}
		resp.Packs = kept
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	rows := make([][]string, 0, len(resp.Packs))
	for _, p := range resp.Packs {
		installed := "-"
		if p.InstalledVersion != nil {
			installed = *p.InstalledVersion
			if p.UpgradeAvailable {
				installed += " → " + p.Manifest.Version
			}
		}
		rows = append(rows, []string{
			p.Manifest.ID, p.Manifest.Domain, strconv.Itoa(p.Manifest.Wave), p.Manifest.Version,
			installed, p.Manifest.Title, p.Manifest.Metric.Label,
		})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "DOMAIN", "WAVE", "VERSION", "INSTALLED", "TITLE", "METRIC"}, rows)
	return nil
}

func printPackContents(contents map[string][]string) {
	seen := map[string]bool{}
	print := func(kind string) {
		names := contents[kind]
		if len(names) == 0 {
			return
		}
		seen[kind] = true
		fmt.Fprintf(os.Stdout, "  %-20s %s\n", kind, strings.Join(names, ", "))
	}
	for _, kind := range packKindOrder {
		print(kind)
	}
	rest := make([]string, 0, len(contents))
	for kind := range contents {
		if !seen[kind] {
			rest = append(rest, kind)
		}
	}
	sort.Strings(rest)
	for _, kind := range rest {
		fmt.Fprintf(os.Stdout, "  %-20s %s\n", kind, strings.Join(contents[kind], ", "))
	}
}

func printPackPrerequisites(list []packPrerequisite) {
	if len(list) == 0 {
		return
	}
	fmt.Fprintln(os.Stdout, "\nPrerequisites")
	rows := make([][]string, 0, len(list))
	for _, p := range list {
		required := "required"
		if p.Optional {
			required = "optional"
		}
		rows = append(rows, []string{p.Kind, p.Name, required, p.Status, p.Note})
	}
	cli.PrintTable(os.Stdout, []string{"KIND", "NAME", "NEED", "STATUS", "NOTE"}, rows)
}

func runPackShow(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var resp struct {
		Pack     packSummary         `json:"pack"`
		Contents map[string][]string `json:"contents"`
	}
	if err := client.GetJSON(cmd.Context(), packsPath+"/"+args[0], &resp); err != nil {
		return fmt.Errorf("read pack: %w", err)
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	m := resp.Pack.Manifest
	fmt.Fprintf(os.Stdout, "%s %s — %s\n%s\n", m.ID, m.Version, m.Title, m.Summary)
	fmt.Fprintf(os.Stdout, "domain %s, wave %d, by %s (%s)\n", m.Domain, m.Wave, m.Author, m.License)
	if m.WorksWithoutAgents {
		fmt.Fprintln(os.Stdout, "works with no agent at all")
	}
	fmt.Fprintf(os.Stdout, "\nMetric: %s\n  %s\n", m.Metric.Label, m.Metric.Description)
	if m.Metric.Hint != "" {
		fmt.Fprintf(os.Stdout, "  where: %s\n", m.Metric.Hint)
	}
	if resp.Pack.InstalledVersion != nil {
		fmt.Fprintf(os.Stdout, "\ninstalled: version %s (install %s)\n", *resp.Pack.InstalledVersion, strPtr(resp.Pack.InstallID))
		if resp.Pack.UpgradeAvailable {
			fmt.Fprintf(os.Stdout, "upgrade available: %s\n", m.Version)
		}
	}
	printPackPrerequisites(resp.Pack.Prerequisites)
	fmt.Fprintln(os.Stdout, "\nContents")
	printPackContents(resp.Contents)
	if strings.TrimSpace(m.Description) != "" {
		fmt.Fprintf(os.Stdout, "\n%s\n", strings.TrimSpace(m.Description))
	}
	return nil
}

func runPackDownload(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	raw, err := client.GetText(cmd.Context(), packsPath+"/"+args[0]+"/download")
	if err != nil {
		return fmt.Errorf("download pack: %w", err)
	}
	return writeDownload(cmd, []byte(raw), "", args[0]+".pack.yaml")
}

// --- preview and install -------------------------------------------------

type packPreviewResponse struct {
	Pack       packSummary         `json:"pack"`
	Contents   map[string][]string `json:"contents"`
	Collisions []packCollision     `json:"collisions"`
	Problems   []string            `json:"problems"`
	Strategies []string            `json:"strategies"`
	Strategy   string              `json:"strategy"`
	Installed  *packInstall        `json:"installed"`
	Blocked    string              `json:"blocked"`
}

func printPackPreview(cmd *cobra.Command, preview packPreviewResponse) error {
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, preview)
	}
	m := preview.Pack.Manifest
	fmt.Fprintf(os.Stdout, "%s %s — %s\nstrategy %s (of %s)\n", m.ID, m.Version, m.Title, preview.Strategy, strings.Join(preview.Strategies, ", "))
	if preview.Installed != nil {
		fmt.Fprintf(os.Stdout, "installed: version %s, install %s\n", preview.Installed.PackVersion, preview.Installed.ID)
	}
	fmt.Fprintln(os.Stdout, "\nContents")
	printPackContents(preview.Contents)
	if len(preview.Collisions) > 0 {
		fmt.Fprintf(os.Stdout, "\nCollisions (%d) — what `%s` does to each\n", len(preview.Collisions), preview.Strategy)
		rows := make([][]string, 0, len(preview.Collisions))
		for _, c := range preview.Collisions {
			rows = append(rows, []string{c.Kind, c.Name, c.ExistingID})
		}
		cli.PrintTable(os.Stdout, []string{"KIND", "NAME", "EXISTING ID"}, rows)
	} else {
		fmt.Fprintln(os.Stdout, "\nno collision: nothing in this workspace shares a name with the pack")
	}
	for _, p := range preview.Problems {
		fmt.Fprintf(os.Stderr, "problem: %s\n", p)
	}
	if preview.Blocked != "" {
		fmt.Fprintf(os.Stderr, "blocked: %s (--force re-applies it)\n", preview.Blocked)
	}
	return nil
}

func runPackPreview(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	strategy, err := packStrategy(cmd)
	if err != nil {
		return err
	}
	var preview packPreviewResponse
	if err := client.PostJSON(cmd.Context(), packsPath+"/"+args[0]+"/preview", map[string]any{"strategy": strategy}, &preview); err != nil {
		return fmt.Errorf("preview pack: %w", err)
	}
	return printPackPreview(cmd, preview)
}

// packStrategy validates --strategy before the request goes out: an unknown
// value would otherwise read as a server-side rejection of the pack itself.
func packStrategy(cmd *cobra.Command) (string, error) {
	strategy, _ := cmd.Flags().GetString("strategy")
	switch strategy {
	case "", "rename", "merge", "skip":
		return strategy, nil
	default:
		return "", fmt.Errorf("--strategy must be rename, merge or skip")
	}
}

type packInstallResponse struct {
	Install packInstall `json:"install"`
	Report  packReport  `json:"report"`
}

func printPackInstall(cmd *cobra.Command, resp packInstallResponse) error {
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fmt.Fprintf(os.Stdout, "%s\t%s %s\t%s\tstrategy %s\n", resp.Install.ID, resp.Install.PackID, resp.Install.PackVersion, resp.Install.Status, resp.Install.Strategy)
	rows := make([][]string, 0, len(resp.Report.Created)+len(resp.Report.Merged))
	for _, kind := range packCountKinds(resp.Report.Created, resp.Report.Merged) {
		rows = append(rows, []string{kind, strconv.Itoa(resp.Report.Created[kind]), strconv.Itoa(resp.Report.Merged[kind])})
	}
	cli.PrintTable(os.Stdout, []string{"KIND", "CREATED", "MERGED"}, rows)
	if n := len(resp.Report.Skipped); n > 0 {
		fmt.Fprintf(os.Stdout, "skipped %d row(s) the workspace already had:\n", n)
		for _, s := range resp.Report.Skipped {
			fmt.Fprintf(os.Stdout, "  %s %s\n", s.Kind, s.Name)
		}
	}
	for _, warning := range resp.Report.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	fmt.Fprintf(os.Stderr, "metric to watch: %s\n", resp.Install.Metric.Label)
	fmt.Fprintln(os.Stderr, "agents are paused colleagues without a runtime, autopilots are disabled and business rules are drafts: a person turns each on")
	return nil
}

// packCountKinds is the union of two count maps, in a stable order.
func packCountKinds(maps ...map[string]int) []string {
	seen := map[string]bool{}
	for _, m := range maps {
		for kind := range m {
			seen[kind] = true
		}
	}
	out := make([]string, 0, len(seen))
	for _, kind := range packKindOrder {
		if seen[kind] {
			out = append(out, kind)
			delete(seen, kind)
		}
	}
	rest := make([]string, 0, len(seen))
	for kind := range seen {
		rest = append(rest, kind)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

func runPackInstall(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	strategy, err := packStrategy(cmd)
	if err != nil {
		return err
	}
	force, _ := cmd.Flags().GetBool("force")
	var resp packInstallResponse
	if err := client.PostJSON(cmd.Context(), packsPath+"/"+args[0]+"/install", map[string]any{"strategy": strategy, "force": force}, &resp); err != nil {
		return fmt.Errorf("install pack: %w", err)
	}
	return printPackInstall(cmd, resp)
}

// runPackFile drives preview-file and install-file: the same upload, one
// endpoint apart.
func runPackFile(cmd *cobra.Command, args []string, install bool) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	strategy, err := packStrategy(cmd)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read %s: %w", args[0], err)
	}
	fields := map[string]string{"strategy": strategy}
	if !install {
		var preview packPreviewResponse
		if err := postMultipart(cmd.Context(), client, packsPath+"/preview", filepath.Base(args[0]), raw, fields, &preview); err != nil {
			return fmt.Errorf("preview pack file: %w", err)
		}
		return printPackPreview(cmd, preview)
	}
	if force, _ := cmd.Flags().GetBool("force"); force {
		fields["force"] = "true"
	}
	var resp packInstallResponse
	if err := postMultipart(cmd.Context(), client, packsPath+"/install", filepath.Base(args[0]), raw, fields, &resp); err != nil {
		return fmt.Errorf("install pack file: %w", err)
	}
	return printPackInstall(cmd, resp)
}

// --- the ledger ----------------------------------------------------------

func runPackInstalled(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var resp struct {
		Installs []packInstall `json:"installs"`
	}
	if err := client.GetJSON(cmd.Context(), packsPath+"/installed", &resp); err != nil {
		return fmt.Errorf("read installed packs: %w", err)
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	rows := make([][]string, 0, len(resp.Installs))
	for _, x := range resp.Installs {
		upgrade := "-"
		if x.UpgradeTo != nil {
			upgrade = *x.UpgradeTo
		}
		rows = append(rows, []string{
			displayID(x.ID, fullID), x.PackID, x.PackVersion, x.Status, x.Strategy,
			strconv.Itoa(x.ItemCount), upgrade, shortTime(x.InstalledAt),
		})
	}
	cli.PrintTable(os.Stdout, []string{"INSTALL", "PACK", "VERSION", "STATUS", "STRATEGY", "ROWS", "UPGRADE", "INSTALLED"}, rows)
	return nil
}

func runPackItems(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var resp struct {
		Install packInstall `json:"install"`
		Items   []packItem  `json:"items"`
	}
	if err := client.GetJSON(cmd.Context(), packsPath+"/installed/"+args[0], &resp); err != nil {
		return fmt.Errorf("read pack install: %w", err)
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	fmt.Fprintf(os.Stdout, "%s %s\t%s\t%d row(s)\n", resp.Install.PackID, resp.Install.PackVersion, resp.Install.Status, resp.Install.ItemCount)
	rows := make([][]string, 0, len(resp.Items))
	for _, it := range resp.Items {
		rows = append(rows, []string{it.Kind, it.Action, displayID(it.ID, fullID), it.Name})
	}
	cli.PrintTable(os.Stdout, []string{"KIND", "ACTION", "ID", "NAME"}, rows)
	return nil
}

func runPackUninstall(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var resp struct {
		Install packInstall `json:"install"`
		Report  struct {
			Removed map[string]int `json:"removed"`
			Kept    []packItem     `json:"kept"`
			Reasons []string       `json:"reasons"`
		} `json:"report"`
	}
	if err := client.PostJSON(cmd.Context(), packsPath+"/installed/"+args[0]+"/uninstall", map[string]any{}, &resp); err != nil {
		return fmt.Errorf("uninstall pack: %w", err)
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fmt.Fprintf(os.Stdout, "%s\t%s %s\t%s\n", resp.Install.ID, resp.Install.PackID, resp.Install.PackVersion, resp.Install.Status)
	rows := make([][]string, 0, len(resp.Report.Removed))
	for _, kind := range packCountKinds(resp.Report.Removed) {
		rows = append(rows, []string{kind, strconv.Itoa(resp.Report.Removed[kind])})
	}
	cli.PrintTable(os.Stdout, []string{"KIND", "REMOVED"}, rows)
	if len(resp.Report.Kept) > 0 {
		fmt.Fprintf(os.Stdout, "kept %d row(s):\n", len(resp.Report.Kept))
		for _, it := range resp.Report.Kept {
			fmt.Fprintf(os.Stdout, "  %s %s\n", it.Kind, it.Name)
		}
	}
	for _, reason := range resp.Report.Reasons {
		fmt.Fprintf(os.Stderr, "kept: %s\n", reason)
	}
	return nil
}

// --- export --------------------------------------------------------------

// packExportManifest builds the manifest the export endpoint validates. The
// required fields are checked here so a typo costs no request.
func packExportManifest(cmd *cobra.Command) (map[string]any, error) {
	str := func(name string) string {
		v, _ := cmd.Flags().GetString(name)
		return strings.TrimSpace(v)
	}
	missing := []string{}
	for _, name := range []string{"id", "title", "summary", "metric-label", "metric-description"} {
		if str(name) == "" {
			missing = append(missing, "--"+name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%s %s required", strings.Join(missing, ", "), map[bool]string{true: "is", false: "are"}[len(missing) == 1])
	}
	wave, _ := cmd.Flags().GetInt("wave")
	return map[string]any{
		"id":          str("id"),
		"version":     str("version"),
		"title":       str("title"),
		"summary":     str("summary"),
		"description": str("description"),
		"domain":      str("domain"),
		"wave":        wave,
		"author":      "",
		"license":     "",
		"metric": map[string]any{
			"label":       str("metric-label"),
			"description": str("metric-description"),
			"hint":        str("metric-hint"),
		},
	}, nil
}

func runPackExport(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	manifest, err := packExportManifest(cmd)
	if err != nil {
		return err
	}
	includeIssues, _ := cmd.Flags().GetBool("include-issues")
	includeNotes, _ := cmd.Flags().GetBool("include-notes")
	includeSkills, _ := cmd.Flags().GetBool("include-skills")
	body := map[string]any{"manifest": manifest, "include_issues": includeIssues, "include_notes": includeNotes, "include_skills": includeSkills}
	data, name, err := postFile(cmd.Context(), client, packsPath+"/export", body)
	if err != nil {
		return fmt.Errorf("export pack: %w", err)
	}
	return writeDownload(cmd, data, name, fmt.Sprintf("%s-%s.pack.yaml", manifest["id"], manifest["version"]))
}

func strPtr(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
