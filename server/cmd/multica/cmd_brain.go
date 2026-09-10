package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// `multica brain` is how a run reads and writes the workspace Brain — the
// shared knowledge base every run gets injected under .multica/knowledge —
// and its capture inbox, the "capture first, organize later" half where
// anything worth keeping lands until a person files it.
// Auth is the ambient one: a run carries its task token, a human their PAT,
// and the server decides whether the note is attributed to an agent or a
// member from that.

var brainCmd = &cobra.Command{
	Use: "brain",
	// A parent command renders only its Short; a Long here would never be
	// printed. The per-command guidance lives on the leaves.
	Short: "Read and write the workspace knowledge base (Brain) and its capture inbox",
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
	brainCmd.AddCommand(brainCaptureCmd)
	brainCmd.AddCommand(brainInboxCmd)
	brainCmd.AddCommand(brainOrganizeCmd)
	brainCmd.AddCommand(brainReopenCmd)
	brainCmd.AddCommand(brainSuggestCmd)
	brainCmd.AddCommand(brainSearchCmd)
	brainCmd.AddCommand(brainDeleteCmd)

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

	brainCaptureCmd.Flags().String("url", "", "Capture this link")
	brainCaptureCmd.Flags().String("kind", "", "Capture kind: text, link or todo (inferred when omitted)")
	brainCaptureCmd.Flags().String("title-hint", "", "A hint for the note title, used when the capture is organized")
	brainCaptureCmd.Flags().String("file", "", "Capture this local file (image, audio or any document)")
	brainCaptureCmd.Flags().String("output", "text", "Output format: text or json")

	brainInboxCmd.Flags().String("status", "raw", "Which captures: raw, organized, discarded or all")
	brainInboxCmd.Flags().Int("limit", 0, "Max number of captures to return (1-200)")
	brainInboxCmd.Flags().String("output", "table", "Output format: table or json")
	brainInboxCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	brainOrganizeCmd.Flags().String("as", "", "What to do with the capture: note, merge or discard (required)")
	brainOrganizeCmd.Flags().String("title", "", "Note title (defaults to the title hint, then the first line)")
	brainOrganizeCmd.Flags().String("tags", "", "Comma-separated tags")
	brainOrganizeCmd.Flags().String("content", "", "Note body (defaults to the capture rendered as markdown)")
	brainOrganizeCmd.Flags().String("note", "", "Note to merge into (required with --as merge)")
	brainOrganizeCmd.Flags().Bool("pinned", false, "Pin the created note so every run receives it")
	brainOrganizeCmd.Flags().String("output", "text", "Output format: text or json")

	brainReopenCmd.Flags().String("output", "text", "Output format: text or json")
	brainSuggestCmd.Flags().String("output", "text", "Output format: text or json")

	brainSearchCmd.Flags().String("tag", "", "Only notes carrying this tag")
	brainSearchCmd.Flags().Bool("archived", false, "Include archived notes")
	brainSearchCmd.Flags().Int("limit", 0, "Max number of hits to return (1-100, default 20)")
	brainSearchCmd.Flags().String("output", "table", "Output format: table or json")
	brainSearchCmd.Flags().Bool("full-id", false, "Show full UUIDs in table output")

	brainDeleteCmd.Flags().Bool("yes", false, "Skip the confirmation prompt")
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

// --- capture inbox ---------------------------------------------------------
//
// "Capture first, organize later": `brain capture` drops anything worth
// keeping into the workspace's capture inbox in one gesture, and a person
// (or a later run) turns it into a note with `brain organize`. A capture is
// never filed behind anyone's back — that is the whole point of the inbox.

var brainCaptureCmd = &cobra.Command{
	Use:   "capture [text...]",
	Short: "Capture something into the Brain inbox, to be organized later",
	Long: `Capture a line of text, a link or a file into the workspace's Brain inbox.

Nothing is filed as a note yet: a capture waits in the inbox until someone
organizes it (multica brain organize). Text can come from the arguments or
from stdin when it is piped.`,
	RunE: runBrainCapture,
}

var brainInboxCmd = &cobra.Command{
	Use:   "inbox",
	Short: "List the Brain capture inbox",
	RunE:  runBrainInbox,
}

var brainOrganizeCmd = &cobra.Command{
	Use:   "organize <capture-id>",
	Short: "Turn a capture into a note, merge it into one, or discard it",
	Args:  exactArgs(1),
	RunE:  runBrainOrganize,
}

var brainReopenCmd = &cobra.Command{
	Use:   "reopen <capture-id>",
	Short: "Put a discarded capture back into the inbox",
	Args:  exactArgs(1),
	RunE:  runBrainReopen,
}

var brainSuggestCmd = &cobra.Command{
	Use:   "suggest <capture-id>",
	Short: "Ask the model how a capture should be filed",
	Args:  exactArgs(1),
	RunE:  runBrainSuggest,
}

var brainSearchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Ranked search over the workspace notes",
	Long: `Search the Brain by relevance rather than by recency.

The query accepts websearch syntax: "a quoted phrase", -negation, OR. When an
embeddings model is configured the lexical rank is fused with a vector rank, so
a note that uses different words than the query still surfaces.`,
	Args: exactArgs(1),
	RunE: runBrainSearch,
}

var brainDeleteCmd = &cobra.Command{
	Use:   "delete <capture-id>",
	Short: "Delete a capture for good",
	Args:  exactArgs(1),
	RunE:  runBrainDelete,
}

// brainCaptureSuggestion mirrors the server's suggestion payload.
type brainCaptureSuggestion struct {
	Title     string   `json:"title"`
	Tags      []string `json:"tags"`
	Summary   string   `json:"summary"`
	Action    string   `json:"action"`
	MergeNote *struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"merge_note"`
	Reason string `json:"reason"`
	Model  string `json:"model"`
}

// brainCapture is the subset of the capture response the CLI renders.
type brainCapture struct {
	ID                  string                  `json:"id"`
	Kind                string                  `json:"kind"`
	Content             string                  `json:"content"`
	URL                 string                  `json:"url"`
	TitleHint           string                  `json:"title_hint"`
	Origin              string                  `json:"origin"`
	Status              string                  `json:"status"`
	TranscriptionStatus string                  `json:"transcription_status"`
	Suggestion          *brainCaptureSuggestion `json:"suggestion"`
	NoteID              *string                 `json:"note_id"`
	CreatedAt           string                  `json:"created_at"`
}

// brainNoteHit is one ranked search result: the note plus why it ranked.
type brainNoteHit struct {
	brainNote
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
	LexRank *int64  `json:"lex_rank"`
	VecRank *int64  `json:"vec_rank"`
}

func runBrainCapture(cmd *cobra.Command, args []string) error {
	kind, _ := cmd.Flags().GetString("kind")
	if kind != "" && kind != "text" && kind != "link" && kind != "todo" {
		return fmt.Errorf("--kind must be text, link or todo (image, audio and file captures come from --file)")
	}
	file, _ := cmd.Flags().GetString("file")
	rawURL, _ := cmd.Flags().GetString("url")
	hint, _ := cmd.Flags().GetString("title-hint")

	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" && !stdinIsPiped() && rawURL == "" && file == "" {
		return fmt.Errorf("nothing to capture: pass text, --url or --file, or pipe the text in")
	}
	if text == "" && stdinIsPiped() {
		data, err := readAllStdin()
		if err != nil {
			return fmt.Errorf("read capture text from stdin: %w", err)
		}
		text = strings.TrimSpace(string(data))
	}
	if file != "" && kind != "" {
		// The server derives image/audio/file from the upload's content type;
		// honouring --kind too would let the two disagree.
		return fmt.Errorf("--kind does not apply to --file: the kind comes from the file's content type")
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

	var resp struct {
		Capture brainCapture `json:"capture"`
	}
	if file != "" {
		data, readErr := os.ReadFile(file)
		if readErr != nil {
			return fmt.Errorf("read capture file: %w", readErr)
		}
		if err := client.UploadBrainCapture(ctx, data, file, text, hint, "cli", &resp); err != nil {
			return fmt.Errorf("capture file: %w", err)
		}
	} else {
		body := map[string]any{"origin": "cli"}
		if text != "" {
			body["content"] = text
		}
		if rawURL != "" {
			body["url"] = rawURL
		}
		if hint != "" {
			body["title_hint"] = hint
		}
		if kind != "" {
			body["kind"] = kind
		}
		if err := client.PostJSON(ctx, "/api/brain/captures", body, &resp); err != nil {
			return fmt.Errorf("capture: %w", err)
		}
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fmt.Fprintf(os.Stdout, "Captured %s (%s) — organize it with: multica brain organize %s --as note\n", resp.Capture.ID, resp.Capture.Kind, resp.Capture.ID)
	if resp.Capture.TranscriptionStatus == "pending" {
		fmt.Fprintln(os.Stdout, "Transcription is running; the text lands on the capture when it finishes.")
	}
	return nil
}

// stdinIsPiped reports whether stdin carries data rather than a terminal, so
// `... | multica brain capture` works while a bare `multica brain capture`
// fails with usage instead of blocking on a read that will never return.
func stdinIsPiped() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice == 0
}

func runBrainInbox(cmd *cobra.Command, _ []string) error {
	status, _ := cmd.Flags().GetString("status")
	switch status {
	case "", "raw", "organized", "discarded", "all":
	default:
		return fmt.Errorf("--status must be raw, organized, discarded or all")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	query := url.Values{}
	if status != "" {
		query.Set("status", status)
	}
	if limit, _ := cmd.Flags().GetInt("limit"); limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/api/brain/captures"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp struct {
		Captures []brainCapture `json:"captures"`
		RawCount int64          `json:"raw_count"`
	}
	if err := client.GetJSON(ctx, path, &resp); err != nil {
		return fmt.Errorf("list brain captures: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "KIND", "STATUS", "ORIGIN", "AGE", "WHAT"}
	rows := make([][]string, 0, len(resp.Captures))
	for _, c := range resp.Captures {
		rows = append(rows, []string{
			displayID(c.ID, fullID),
			c.Kind,
			c.Status,
			c.Origin,
			relativeTimestamp(c.CreatedAt),
			brainCaptureSummary(c),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	fmt.Fprintf(os.Stdout, "\n%d capture(s) still raw.\n", resp.RawCount)
	return nil
}

// brainCaptureSummary is the one line that identifies a capture in a list:
// the title hint if the capture carries one, else its first line, else the URL.
func brainCaptureSummary(c brainCapture) string {
	if hint := strings.TrimSpace(c.TitleHint); hint != "" {
		return truncateBrainLine(hint)
	}
	for _, line := range strings.Split(c.Content, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return truncateBrainLine(trimmed)
		}
	}
	return truncateBrainLine(c.URL)
}

func truncateBrainLine(s string) string {
	const max = 60
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

func runBrainOrganize(cmd *cobra.Command, args []string) error {
	action, _ := cmd.Flags().GetString("as")
	switch action {
	case "note", "merge", "discard":
	default:
		return fmt.Errorf("--as must be note, merge or discard")
	}
	noteID, _ := cmd.Flags().GetString("note")
	if action == "merge" && strings.TrimSpace(noteID) == "" {
		return fmt.Errorf("--note <note-id> is required with --as merge")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	body := map[string]any{"action": action}
	if title, _ := cmd.Flags().GetString("title"); title != "" {
		body["title"] = title
	}
	if tags, _ := cmd.Flags().GetString("tags"); tags != "" {
		body["tags"] = splitBrainTags(tags)
	}
	if content, _ := cmd.Flags().GetString("content"); content != "" {
		body["content"] = content
	}
	if strings.TrimSpace(noteID) != "" {
		body["note_id"] = noteID
	}
	if cmd.Flags().Changed("pinned") {
		pinned, _ := cmd.Flags().GetBool("pinned")
		body["pinned"] = pinned
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp struct {
		Capture brainCapture `json:"capture"`
		Note    *brainNote   `json:"note"`
	}
	if err := client.PostJSON(ctx, "/api/brain/captures/"+url.PathEscape(args[0])+"/organize", body, &resp); err != nil {
		return fmt.Errorf("organize capture: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	if resp.Note != nil {
		fmt.Fprintf(os.Stdout, "Capture %s %sd into note %s: %s\n", resp.Capture.ID, action, resp.Note.ID, resp.Note.Title)
		return nil
	}
	fmt.Fprintf(os.Stdout, "Capture %s discarded.\n", resp.Capture.ID)
	return nil
}

func runBrainReopen(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp struct {
		Capture brainCapture `json:"capture"`
	}
	if err := client.PostJSON(ctx, "/api/brain/captures/"+url.PathEscape(args[0])+"/reopen", nil, &resp); err != nil {
		return fmt.Errorf("reopen capture: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fmt.Fprintf(os.Stdout, "Capture %s is back in the inbox.\n", resp.Capture.ID)
	return nil
}

func runBrainSuggest(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp struct {
		Capture brainCapture `json:"capture"`
	}
	if err := client.PostJSON(ctx, "/api/brain/captures/"+url.PathEscape(args[0])+"/suggest", nil, &resp); err != nil {
		// 503 is a configuration answer, not a failure to retry: the
		// workspace has no assist-layer model, so nothing can suggest.
		var httpErr *cli.HTTPError
		if errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusServiceUnavailable {
			return fmt.Errorf("no model configured for this workspace: suggestions need an assist-layer LLM. Organize the capture by hand with: multica brain organize %s --as note --title \"...\"", args[0])
		}
		return fmt.Errorf("suggest for capture: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	s := resp.Capture.Suggestion
	if s == nil {
		fmt.Fprintf(os.Stdout, "No suggestion for capture %s.\n", resp.Capture.ID)
		return nil
	}
	fmt.Fprintf(os.Stdout, "Suggested action: %s\n", s.Action)
	if s.Title != "" {
		fmt.Fprintf(os.Stdout, "Title: %s\n", s.Title)
	}
	if len(s.Tags) > 0 {
		fmt.Fprintf(os.Stdout, "Tags: %s\n", strings.Join(s.Tags, ", "))
	}
	if s.MergeNote != nil {
		fmt.Fprintf(os.Stdout, "Merge into: %s (%s)\n", s.MergeNote.Title, s.MergeNote.ID)
	}
	if s.Summary != "" {
		fmt.Fprintf(os.Stdout, "Summary: %s\n", s.Summary)
	}
	if s.Reason != "" {
		fmt.Fprintf(os.Stdout, "Why: %s\n", s.Reason)
	}
	return nil
}

func runBrainSearch(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}

	query := url.Values{}
	query.Set("q", args[0])
	if tag, _ := cmd.Flags().GetString("tag"); tag != "" {
		query.Set("tag", tag)
	}
	if archived, _ := cmd.Flags().GetBool("archived"); archived {
		query.Set("archived", "true")
	}
	if limit, _ := cmd.Flags().GetInt("limit"); limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", limit))
	}

	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var resp struct {
		Notes  []brainNoteHit `json:"notes"`
		Vector bool           `json:"vector"`
	}
	if err := client.GetJSON(ctx, "/api/workspace/notes/search?"+query.Encode(), &resp); err != nil {
		return fmt.Errorf("search workspace notes: %w", err)
	}

	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}

	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"#", "SCORE", "ID", "TITLE", "SNIPPET"}
	rows := make([][]string, 0, len(resp.Notes))
	for i, hit := range resp.Notes {
		rows = append(rows, []string{
			fmt.Sprintf("%d", i+1),
			fmt.Sprintf("%.4f", hit.Score),
			displayID(hit.ID, fullID),
			hit.Title,
			truncateBrainLine(stripSearchHighlight(hit.Snippet)),
		})
	}
	cli.PrintTable(os.Stdout, headers, rows)
	if !resp.Vector {
		fmt.Fprintln(os.Stdout, "\nLexical ranking only: no embeddings model is configured for this workspace.")
	}
	return nil
}

// stripSearchHighlight turns the server's <mark>-annotated snippet into one
// line of plain text. The table is not HTML, so the markers would only be
// noise — the rank column already says what matched.
func stripSearchHighlight(snippet string) string {
	plain := strings.NewReplacer("<mark>", "", "</mark>", "", "\n", " ", "\r", " ", "\t", " ").Replace(snippet)
	return strings.Join(strings.Fields(plain), " ")
}

func runBrainDelete(cmd *cobra.Command, args []string) error {
	yes, _ := cmd.Flags().GetBool("yes")
	if !yes {
		fmt.Printf("Delete capture %s for good? This cannot be undone. [y/N] ", args[0])
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if answer = strings.TrimSpace(strings.ToLower(answer)); answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
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

	if err := client.DeleteJSON(ctx, "/api/brain/captures/"+url.PathEscape(args[0])); err != nil {
		return fmt.Errorf("delete capture: %w", err)
	}
	fmt.Fprintf(os.Stdout, "Capture deleted: %s\n", args[0])
	return nil
}
