package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Daemon execution memory + DAEMON.md export (F24 / JEF-15).
//
// `memory set` is the one command here an AGENT runs, from inside a run of the
// daemon it is writing for: the server authorizes the write on the run's task
// token, so a member calling it gets 403. It sends If-Match with the revision
// it just read, so two overlapping runs of the same daemon cannot silently
// overwrite each other.

var autopilotMemoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Read or write a daemon's execution memory",
}

var autopilotMemoryGetCmd = &cobra.Command{
	Use:   "get <autopilot-id>",
	Short: "Print a daemon's execution memory",
	Args:  exactArgs(1),
	RunE:  runAutopilotMemoryGet,
}

var autopilotMemorySetCmd = &cobra.Command{
	Use:   "set <autopilot-id>",
	Short: "Replace a daemon's execution memory (agents only, from inside that daemon's run)",
	Args:  exactArgs(1),
	RunE:  runAutopilotMemorySet,
}

var autopilotExportCmd = &cobra.Command{
	Use:   "export <autopilot-id>",
	Short: "Print an autopilot as a DAEMON.md declaration",
	Args:  exactArgs(1),
	RunE:  runAutopilotExport,
}

func init() {
	autopilotCmd.AddCommand(autopilotMemoryCmd)
	autopilotCmd.AddCommand(autopilotExportCmd)
	autopilotMemoryCmd.AddCommand(autopilotMemoryGetCmd)
	autopilotMemoryCmd.AddCommand(autopilotMemorySetCmd)

	autopilotMemoryGetCmd.Flags().String("output", "text", "Output format: text or json")
	autopilotMemorySetCmd.Flags().String("content", "", "New memory content; omit to read it from stdin")
	autopilotMemorySetCmd.Flags().String("output", "json", "Output format: text or json")
	autopilotExportCmd.Flags().String("out", "", "Write the declaration to this file instead of stdout")
}

type autopilotMemoryPayload struct {
	AutopilotID     string  `json:"autopilot_id"`
	Content         string  `json:"content"`
	Revision        int     `json:"revision"`
	UpdatedByTaskID *string `json:"updated_by_task_id"`
	UpdatedAt       string  `json:"updated_at"`
	Truncated       bool    `json:"truncated"`
}

func runAutopilotMemoryGet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	var memory autopilotMemoryPayload
	if err := client.GetJSON(ctx, "/api/autopilots/"+args[0]+"/memory", &memory); err != nil {
		return fmt.Errorf("get autopilot memory: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, memory)
	}
	if memory.Content == "" {
		cmd.Println("(no memory yet)")
		return nil
	}
	cmd.Println(memory.Content)
	return nil
}

func runAutopilotMemorySet(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	autopilotID := args[0]

	content, _ := cmd.Flags().GetString("content")
	if !cmd.Flags().Changed("content") {
		// stdin is the ergonomic path for an agent writing a multi-line note:
		// `multica autopilot memory set <id> < note.md`.
		raw, rerr := io.ReadAll(cmd.InOrStdin())
		if rerr != nil {
			return fmt.Errorf("read memory from stdin: %w", rerr)
		}
		content = string(raw)
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("memory content is empty; pass --content or pipe the text on stdin")
	}

	// Read the current revision so the write carries a precondition. Without
	// it the last writer silently wins, and two overlapping runs of the same
	// daemon would each think their note survived.
	var current autopilotMemoryPayload
	if err := client.GetJSON(ctx, "/api/autopilots/"+autopilotID+"/memory", &current); err != nil {
		return fmt.Errorf("read current autopilot memory: %w", err)
	}

	var updated autopilotMemoryPayload
	if err := client.PutJSONWithHeaders(
		ctx,
		"/api/autopilots/"+autopilotID+"/memory",
		map[string]string{"content": content},
		&updated,
		map[string]string{"If-Match": fmt.Sprintf("%d", current.Revision)},
	); err != nil {
		return fmt.Errorf("set autopilot memory: %w", err)
	}

	if updated.Truncated {
		cmd.PrintErrln("note: the memory exceeded 8 KiB and was cut at a line boundary")
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, updated)
	}
	cmd.Printf("Memory saved (revision %d)\n", updated.Revision)
	return nil
}

func runAutopilotExport(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	markdown, err := client.GetText(ctx, "/api/autopilots/"+args[0]+"/export")
	if err != nil {
		return fmt.Errorf("export autopilot: %w", err)
	}
	outPath, _ := cmd.Flags().GetString("out")
	if outPath == "" {
		cmd.Print(markdown)
		return nil
	}
	if err := os.WriteFile(outPath, []byte(markdown), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", outPath, err)
	}
	cmd.Printf("Wrote %s\n", outPath)
	return nil
}
