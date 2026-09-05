package main

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// `multica brain` is how a run reads and writes the workspace Brain — the
// shared knowledge base every run gets injected under .multica/knowledge.
// Auth is the ambient one: a run carries its task token, a human their PAT,
// and the server decides whether the note is attributed to an agent or a
// member from that.

var brainCmd = &cobra.Command{
	Use:   "brain",
	Short: "Read and write the workspace knowledge base (Brain)",
}

var brainListCmd = &cobra.Command{
	Use:   "list",
	Short: "List workspace notes, optionally filtered by search text or tag",
	RunE:  runBrainList,
}

var brainShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Print one workspace note",
	Args:  exactArgs(1),
	RunE:  runBrainShow,
}

var brainSaveCmd = &cobra.Command{
	Use:   "save",
	Short: "Save a durable piece of workspace knowledge as a note",
	RunE:  runBrainSave,
}

var brainArchiveCmd = &cobra.Command{
	Use:   "archive <id>",
	Short: "Archive a note so it stops being injected into runs",
	Args:  exactArgs(1),
	RunE:  runBrainArchive,
}

func init() {
	brainCmd.AddCommand(brainListCmd)
	brainCmd.AddCommand(brainShowCmd)
	brainCmd.AddCommand(brainSaveCmd)
	brainCmd.AddCommand(brainArchiveCmd)

	brainListCmd.Flags().String("search", "", "Full-text search over title and body")
	brainListCmd.Flags().String("tag", "", "Only notes carrying this tag")
	brainListCmd.Flags().Bool("archived", false, "Include archived notes")
	brainListCmd.Flags().Int("limit", 0, "Max number of notes to return")
	brainListCmd.Flags().String("output", "table", "Output format: table or json")
	brainListCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	brainShowCmd.Flags().String("output", "markdown", "Output format: markdown or json")

	brainSaveCmd.Flags().String("title", "", "Note title (required)")
	brainSaveCmd.Flags().String("tags", "", "Comma-separated tags")
	brainSaveCmd.Flags().String("content", "", "Note body as markdown")
	brainSaveCmd.Flags().String("content-file", "", "Read the note body from this file (use - for stdin)")
	brainSaveCmd.Flags().Bool("pinned", false, "Pin the note so every run always receives it")
	brainSaveCmd.Flags().String("id", "", "Update this existing note instead of creating a new one")
	brainSaveCmd.Flags().String("output", "json", "Output format: table or json")

	brainArchiveCmd.Flags().String("output", "json", "Output format: table or json")
}

// brainNote is the subset of the API response the CLI renders. The endpoint
// returns more; decoding into a struct rather than map[string]any keeps the
// revision typed, which the update path depends on.
type brainNote struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Content   string   `json:"content"`
	Tags      []string `json:"tags"`
	Source    string   `json:"source"`
	Pinned    bool     `json:"pinned"`
	Revision  int64    `json:"revision"`
	UpdatedAt string   `json:"updated_at"`
}

func runBrainList(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	query := url.Values{}
	if search, _ := cmd.Flags().GetString("search"); search != "" {
		query.Set("search", search)
	}
	if tag, _ := cmd.Flags().GetString("tag"); tag != "" {
		query.Set("tag", tag)
	}
	if archived, _ := cmd.Flags().GetBool("archived"); archived {
		query.Set("archived", "true")
	}
	if limit, _ := cmd.Flags().GetInt("limit"); limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/api/workspace/notes"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp struct {
		Items []brainNote `json:"items"`
		Tags  []string    `json:"tags"`
	}
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("list workspace notes: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "TITLE", "TAGS", "SOURCE", "PINNED", "UPDATED"}
	rows := make([][]string, 0, len(resp.Items))
	for _, n := range resp.Items {
		pinned := ""
		if n.Pinned {
			pinned = "yes"
		}
		rows = append(rows, []string{
			displayID(n.ID, fullID),
			n.Title,
			strings.Join(n.Tags, ","),
			n.Source,
			pinned,
			relativeTimestamp(n.UpdatedAt),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func runBrainShow(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var note brainNote
	if err := client.GetJSON(ctx, "/api/workspace/notes/"+url.PathEscape(args[0]), &note); err != nil {
		return fmt.Errorf("get workspace note: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, note)
	}
	fmt.Fprintf(os.Stdout, "# %s\n\n", note.Title)
	fmt.Fprintf(os.Stdout, "- id: %s\n", note.ID)
	if len(note.Tags) > 0 {
		fmt.Fprintf(os.Stdout, "- tags: %s\n", strings.Join(note.Tags, ", "))
	}
	fmt.Fprintf(os.Stdout, "- source: %s\n- revision: %d\n\n%s\n", note.Source, note.Revision, note.Content)
	return nil
}

// readBrainContent resolves --content / --content-file into the note body.
// Exactly one of the two may be given: silently preferring one would make a
// run that passed both write a body it did not intend.
func readBrainContent(cmd *cobra.Command) (string, error) {
	content, _ := cmd.Flags().GetString("content")
	file, _ := cmd.Flags().GetString("content-file")
	if content != "" && file != "" {
		return "", fmt.Errorf("pass either --content or --content-file, not both")
	}
	if file == "" {
		return content, nil
	}
	var data []byte
	var err error
	if file == "-" {
		data, err = readAllStdin()
	} else {
		data, err = os.ReadFile(file)
	}
	if err != nil {
		return "", fmt.Errorf("read content file: %w", err)
	}
	return string(data), nil
}

func runBrainSave(cmd *cobra.Command, _ []string) error {
	title, _ := cmd.Flags().GetString("title")
	noteID, _ := cmd.Flags().GetString("id")
	if strings.TrimSpace(title) == "" && noteID == "" {
		return fmt.Errorf("--title is required")
	}
	content, err := readBrainContent(cmd)
	if err != nil {
		return err
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

	body := map[string]any{}
	if strings.TrimSpace(title) != "" {
		body["title"] = title
	}
	if content != "" {
		body["content"] = content
	}
	if tags, _ := cmd.Flags().GetString("tags"); tags != "" {
		body["tags"] = splitBrainTags(tags)
	}
	if cmd.Flags().Changed("pinned") {
		pinned, _ := cmd.Flags().GetBool("pinned")
		body["pinned"] = pinned
	}

	var note brainNote
	if noteID == "" {
		if err := client.PostJSON(ctx, "/api/workspace/notes", body, &note); err != nil {
			return fmt.Errorf("save workspace note: %w", err)
		}
	} else {
		// The PATCH carries the revision the server currently holds, so a note
		// edited between this read and the write is refused (409) instead of
		// being silently overwritten.
		var current brainNote
		if err := client.GetJSON(ctx, "/api/workspace/notes/"+url.PathEscape(noteID), &current); err != nil {
			return fmt.Errorf("get workspace note: %w", err)
		}
		body["revision"] = current.Revision
		if err := client.PatchJSON(ctx, "/api/workspace/notes/"+url.PathEscape(noteID), body, &note); err != nil {
			return fmt.Errorf("update workspace note: %w", err)
		}
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, note)
	}
	fmt.Fprintf(os.Stdout, "Saved note %s: %s\n", note.ID, note.Title)
	return nil
}

func splitBrainTags(raw string) []string {
	parts := strings.Split(raw, ",")
	tags := make([]string, 0, len(parts))
	for _, p := range parts {
		if tag := strings.TrimSpace(p); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func runBrainArchive(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var note brainNote
	if err := client.PostJSON(ctx, "/api/workspace/notes/"+url.PathEscape(args[0])+"/archive", nil, &note); err != nil {
		return fmt.Errorf("archive workspace note: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, note)
	}
	fmt.Fprintf(os.Stdout, "Archived note %s: %s\n", note.ID, note.Title)
	return nil
}
