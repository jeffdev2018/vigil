package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

// F26 (JEF-22): the generating run's half of the code wiki.
//
// A generation is three steps and they are separate commands on purpose: the
// snapshot is claimed and its file inventory announced first, pages are written
// one at a time (each refused immediately if it cites a file the inventory does
// not contain), and only a deliberate publish makes the whole set visible. A run
// that dies between page two and page nine leaves the previous wiki in place.

var wikiCmd = &cobra.Command{
	Use:   "wiki",
	Short: "Write the generated code wiki for a project repository",
}

var wikiStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a wiki snapshot and announce the repository's file inventory",
	Long: "Claims the project's single in-flight wiki build and records the commit it describes\n" +
		"plus the list of files present at that commit. Page citations are validated against\n" +
		"that list, so a page cannot cite a file the repository does not have.",
	RunE: runWikiStart,
}

var wikiPageSetCmd = &cobra.Command{
	Use:   "set",
	Short: "Write one page into a wiki snapshot",
	RunE:  runWikiPageSet,
}

var wikiPublishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish a wiki snapshot, replacing the previous one in one step",
	RunE:  runWikiPublish,
}

var wikiPageCmd = &cobra.Command{
	Use:   "page",
	Short: "Wiki page commands",
}

func init() {
	wikiPageCmd.AddCommand(wikiPageSetCmd)
	wikiCmd.AddCommand(wikiStartCmd)
	wikiCmd.AddCommand(wikiPageCmd)
	wikiCmd.AddCommand(wikiPublishCmd)

	wikiStartCmd.Flags().String("project", "", "Project id (required)")
	wikiStartCmd.Flags().String("resource", "", "Repository resource id, when the project tracks several")
	wikiStartCmd.Flags().String("commit", "", "Commit SHA the wiki describes (required)")
	wikiStartCmd.Flags().String("paths-from", "", "File holding one repository-relative path per line, or - for stdin. Defaults to `git ls-files` in the working directory")
	wikiStartCmd.Flags().String("output", "", "Set to json for machine-readable output")

	wikiPageSetCmd.Flags().String("project", "", "Project id (required)")
	wikiPageSetCmd.Flags().String("snapshot", "", "Snapshot id from `wiki start` (required)")
	wikiPageSetCmd.Flags().String("slug", "", "Page slug (required)")
	wikiPageSetCmd.Flags().String("title", "", "Page title (required)")
	wikiPageSetCmd.Flags().String("content-file", "", "File holding the page's Markdown, or - for stdin (required)")
	wikiPageSetCmd.Flags().String("citations-file", "", "JSON array of {path, start_line, end_line}, or - for stdin")
	wikiPageSetCmd.Flags().StringArray("cite", nil, "A citation as path or path:start-end; repeatable")
	wikiPageSetCmd.Flags().String("output", "", "Set to json for machine-readable output")

	wikiPublishCmd.Flags().String("project", "", "Project id (required)")
	wikiPublishCmd.Flags().String("snapshot", "", "Snapshot id to publish (required)")
	wikiPublishCmd.Flags().String("output", "", "Set to json for machine-readable output")
}

type wikiSnapshot struct {
	ID        string `json:"id"`
	CommitSha string `json:"commit_sha"`
	State     string `json:"state"`
	PageCount int    `json:"page_count"`
}

type wikiPage struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

type wikiCitation struct {
	Path      string `json:"path"`
	StartLine *int32 `json:"start_line,omitempty"`
	EndLine   *int32 `json:"end_line,omitempty"`
}

func runWikiStart(cmd *cobra.Command, _ []string) error {
	projectID, _ := cmd.Flags().GetString("project")
	commit, _ := cmd.Flags().GetString("commit")
	if strings.TrimSpace(projectID) == "" {
		return fmt.Errorf("--project is required")
	}
	if strings.TrimSpace(commit) == "" {
		return fmt.Errorf("--commit is required")
	}
	pathsFrom, _ := cmd.Flags().GetString("paths-from")
	paths, err := readRepoPaths(pathsFrom)
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("no repository paths to announce: page citations are validated against this list, so it cannot be empty")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	body := map[string]any{"commit_sha": commit, "repo_paths": paths}
	if resource, _ := cmd.Flags().GetString("resource"); strings.TrimSpace(resource) != "" {
		body["resource_id"] = resource
	}
	var snapshot wikiSnapshot
	if err := client.PostJSON(ctx, wikiPath(projectID, "/wiki/snapshots"), body, &snapshot); err != nil {
		return fmt.Errorf("start wiki snapshot: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, snapshot)
	}
	fmt.Fprintf(os.Stdout, "Wiki snapshot %s building at %s (%d files announced)\n", snapshot.ID, snapshot.CommitSha, len(paths))
	return nil
}

func runWikiPageSet(cmd *cobra.Command, _ []string) error {
	projectID, _ := cmd.Flags().GetString("project")
	snapshotID, _ := cmd.Flags().GetString("snapshot")
	slug, _ := cmd.Flags().GetString("slug")
	title, _ := cmd.Flags().GetString("title")
	for name, value := range map[string]string{"project": projectID, "snapshot": snapshotID, "slug": slug, "title": title} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("--%s is required", name)
		}
	}
	contentFile, _ := cmd.Flags().GetString("content-file")
	if strings.TrimSpace(contentFile) == "" {
		return fmt.Errorf("--content-file is required")
	}
	content, err := readFileOrStdin(contentFile)
	if err != nil {
		return err
	}
	citations, err := readCitations(cmd)
	if err != nil {
		return err
	}
	if len(citations) == 0 {
		return fmt.Errorf("a wiki page must carry at least one citation: pass --cite or --citations-file")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var page wikiPage
	path := wikiPath(projectID, "/wiki/snapshots/"+url.PathEscape(snapshotID)+"/pages")
	body := map[string]any{"slug": slug, "title": title, "content": content, "citations": citations}
	if err := client.PostJSON(ctx, path, body, &page); err != nil {
		return fmt.Errorf("write wiki page: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, page)
	}
	fmt.Fprintf(os.Stdout, "Wrote wiki page %s (%s)\n", page.Slug, page.Title)
	return nil
}

func runWikiPublish(cmd *cobra.Command, _ []string) error {
	projectID, _ := cmd.Flags().GetString("project")
	snapshotID, _ := cmd.Flags().GetString("snapshot")
	if strings.TrimSpace(projectID) == "" || strings.TrimSpace(snapshotID) == "" {
		return fmt.Errorf("--project and --snapshot are required")
	}
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var snapshot wikiSnapshot
	path := wikiPath(projectID, "/wiki/snapshots/"+url.PathEscape(snapshotID)+"/publish")
	if err := client.PostJSON(ctx, path, map[string]any{}, &snapshot); err != nil {
		return fmt.Errorf("publish wiki snapshot: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, snapshot)
	}
	fmt.Fprintf(os.Stdout, "Published wiki snapshot %s with %d pages\n", snapshot.ID, snapshot.PageCount)
	return nil
}

func wikiPath(projectID, suffix string) string {
	return "/api/projects/" + url.PathEscape(strings.TrimSpace(projectID)) + suffix
}

// readRepoPaths returns the file inventory to announce. With no source given it
// asks git, which is the list the run can actually vouch for.
func readRepoPaths(source string) ([]string, error) {
	if strings.TrimSpace(source) == "" {
		out, err := exec.Command("git", "ls-files").Output()
		if err != nil {
			return nil, fmt.Errorf("listing repository files with git failed; pass --paths-from instead: %w", err)
		}
		return splitNonEmptyLines(string(out)), nil
	}
	body, err := readFileOrStdin(source)
	if err != nil {
		return nil, err
	}
	return splitNonEmptyLines(body), nil
}

func splitNonEmptyLines(s string) []string {
	out := []string{}
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func readFileOrStdin(path string) (string, error) {
	if strings.TrimSpace(path) == "-" {
		body, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		return string(body), nil
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	return string(body), nil
}

// readCitations merges --citations-file and repeatable --cite.
func readCitations(cmd *cobra.Command) ([]wikiCitation, error) {
	out := []wikiCitation{}
	if file, _ := cmd.Flags().GetString("citations-file"); strings.TrimSpace(file) != "" {
		body, err := readFileOrStdin(file)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(body), &out); err != nil {
			return nil, fmt.Errorf("citations file must hold a JSON array of {path, start_line, end_line}: %w", err)
		}
	}
	cites, _ := cmd.Flags().GetStringArray("cite")
	for _, raw := range cites {
		citation, err := parseCiteFlag(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, citation)
	}
	return out, nil
}

// parseCiteFlag reads `path`, `path:12` or `path:12-40`.
func parseCiteFlag(raw string) (wikiCitation, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return wikiCitation{}, fmt.Errorf("--cite needs a repository-relative path")
	}
	idx := strings.LastIndex(raw, ":")
	if idx < 0 {
		return wikiCitation{Path: raw}, nil
	}
	path, span := raw[:idx], raw[idx+1:]
	start, end, ok := parseLineSpan(span)
	if !ok {
		// A colon that is not a line span is part of the path.
		return wikiCitation{Path: raw}, nil
	}
	citation := wikiCitation{Path: path, StartLine: &start}
	if end > 0 {
		citation.EndLine = &end
	}
	return citation, nil
}

func parseLineSpan(span string) (int32, int32, bool) {
	if span == "" {
		return 0, 0, false
	}
	first, second, hasDash := strings.Cut(span, "-")
	start, ok := parsePositiveInt32(first)
	if !ok {
		return 0, 0, false
	}
	if !hasDash {
		return start, 0, true
	}
	end, ok := parsePositiveInt32(second)
	if !ok {
		return 0, 0, false
	}
	return start, end, true
}

func parsePositiveInt32(s string) (int32, bool) {
	if s == "" {
		return 0, false
	}
	n := int32(0)
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int32(c-'0')
		if n > 1<<24 {
			return 0, false
		}
	}
	if n < 1 {
		return 0, false
	}
	return n, true
}
