package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Workspace transfer (K76) and DAEMON.md import (F24) from the terminal.
// Both were reachable from the UI only, while the docs described the commands;
// they run the same endpoints the UI does, with the same owner/admin gate and
// the same preview-before-write shape as `pack`.

var workspaceExportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export this workspace as a transfer bundle (.zip)",
	Long: "Writes the workspace's configuration — agents, skills, permission profiles, projects, goals, " +
		"autopilots, triage sources, organizations, work item types, statuses, labels, properties, views and rules — " +
		"as a zip you can import into another workspace.\n\n" +
		"No credential travels: agent env keys, autopilot trigger secrets and triage tokens are left behind and " +
		"listed in the bundle's manifest so the destination knows what to re-enter.",
	Args: exactArgs(0),
	RunE: runWorkspaceExport,
}

var workspaceImportCmd = &cobra.Command{
	Use:   "import <bundle.zip>",
	Short: "Import a transfer bundle into this workspace (owner/admin)",
	Long: "Applies a bundle in one transaction. --preview reports what the bundle would collide with and " +
		"changes nothing.\n\n" +
		"The strategy decides what happens on a name collision: rename keeps both (the default), " +
		"merge updates the workspace's row, skip leaves it alone.",
	Args: exactArgs(1),
	RunE: runWorkspaceImport,
}

var autopilotImportCmd = &cobra.Command{
	Use:   "import <DAEMON.md>",
	Short: "Import a DAEMON.md declaration as an autopilot",
	Long: "A declaration replaces what it declares: a trigger removed from the file stops firing after the next " +
		"import, and nothing is merged. Importing the same file twice changes nothing.\n\n" +
		"--preview parses and validates the file, reports the frontmatter, the resolved agent and whether a daemon " +
		"of that name already exists, and writes nothing. --strategy says what to do when one does: " +
		"fail (the default), overwrite, or rename.",
	Args: exactArgs(1),
	RunE: runAutopilotImport,
}

func init() {
	workspaceCmd.AddCommand(workspaceExportCmd, workspaceImportCmd)
	autopilotCmd.AddCommand(autopilotImportCmd)

	workspaceExportCmd.Flags().Bool("include-issues", false, "include issues and their comments")
	workspaceExportCmd.Flags().Bool("include-notes", false, "include Brain notes")
	workspaceExportCmd.Flags().Bool("template", true, "keep the bundle on the server so a new workspace can be created from it")
	workspaceExportCmd.Flags().String("name", "", "name recorded in the bundle (default: the workspace's name)")
	workspaceExportCmd.Flags().String("out", "", "write to this file (default: the name the server sends)")
	workspaceImportCmd.Flags().String("strategy", "rename", "collision strategy: rename, merge or skip")
	workspaceImportCmd.Flags().Bool("preview", false, "report collisions and pending secrets without writing")
	workspaceImportCmd.Flags().String("output", "table", "Output format: table or json")
	autopilotImportCmd.Flags().String("strategy", "fail", "when a daemon of that name exists: fail, overwrite or rename")
	autopilotImportCmd.Flags().Bool("preview", false, "validate and report without writing")
	autopilotImportCmd.Flags().String("output", "table", "Output format: table or json")
}

func runWorkspaceExport(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	includeIssues, _ := cmd.Flags().GetBool("include-issues")
	includeNotes, _ := cmd.Flags().GetBool("include-notes")
	template, _ := cmd.Flags().GetBool("template")
	name, _ := cmd.Flags().GetString("name")
	body := map[string]any{"include_issues": includeIssues, "include_notes": includeNotes, "template": template, "name": name}
	data, serverName, err := postFile(cmd.Context(), client, "/api/workspace-transfer/export", body)
	if err != nil {
		return fmt.Errorf("export workspace: %w", err)
	}
	return writeDownload(cmd, data, serverName, "workspace.multica.zip")
}

func runWorkspaceImport(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	strategy, err := transferStrategy(cmd)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read %s: %w", args[0], err)
	}
	name := filepath.Base(args[0])
	if preview, _ := cmd.Flags().GetBool("preview"); preview {
		var resp struct {
			Manifest struct {
				Name          string         `json:"name"`
				FormatVersion int            `json:"format_version"`
				Counts        map[string]int `json:"counts"`
			} `json:"manifest"`
			Collisions []packCollision `json:"collisions"`
			Secrets    []struct {
				Agent string `json:"agent"`
				Key   string `json:"key"`
			} `json:"secrets"`
			Strategies []string `json:"strategies"`
		}
		if err := postMultipart(cmd.Context(), client, "/api/workspace-transfer/preview", name, raw, nil, &resp); err != nil {
			return fmt.Errorf("preview import: %w", err)
		}
		if jsonOutput(cmd) {
			return cli.PrintJSON(os.Stdout, resp)
		}
		fmt.Fprintf(os.Stdout, "%s\tformat %d\n", resp.Manifest.Name, resp.Manifest.FormatVersion)
		rows := make([][]string, 0, len(resp.Manifest.Counts))
		for _, kind := range packCountKinds(resp.Manifest.Counts) {
			rows = append(rows, []string{kind, strconv.Itoa(resp.Manifest.Counts[kind])})
		}
		cli.PrintTable(os.Stdout, []string{"KIND", "COUNT"}, rows)
		if len(resp.Collisions) > 0 {
			fmt.Fprintf(os.Stdout, "collisions (%d):\n", len(resp.Collisions))
			for _, c := range resp.Collisions {
				fmt.Fprintf(os.Stdout, "  %s %s\n", c.Kind, c.Name)
			}
		}
		for _, s := range resp.Secrets {
			fmt.Fprintf(os.Stderr, "secret to re-enter after the import: %s %s\n", s.Agent, s.Key)
		}
		return nil
	}
	var resp struct {
		RunID  string     `json:"run_id"`
		Report packReport `json:"report"`
	}
	if err := postMultipart(cmd.Context(), client, "/api/workspace-transfer/import", name, raw, map[string]string{"strategy": strategy}, &resp); err != nil {
		return fmt.Errorf("import workspace: %w", err)
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fmt.Fprintf(os.Stdout, "%s\tstrategy %s\n", resp.RunID, strategy)
	rows := make([][]string, 0, len(resp.Report.Created))
	for _, kind := range packCountKinds(resp.Report.Created, resp.Report.Merged) {
		rows = append(rows, []string{kind, strconv.Itoa(resp.Report.Created[kind]), strconv.Itoa(resp.Report.Merged[kind])})
	}
	cli.PrintTable(os.Stdout, []string{"KIND", "CREATED", "MERGED"}, rows)
	for _, s := range resp.Report.Skipped {
		fmt.Fprintf(os.Stdout, "skipped %s %s\n", s.Kind, s.Name)
	}
	for _, warning := range resp.Report.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	return nil
}

// transferStrategy validates --strategy for a bundle import; the daemon
// import has its own vocabulary (fail / overwrite / rename).
func transferStrategy(cmd *cobra.Command) (string, error) {
	strategy, _ := cmd.Flags().GetString("strategy")
	switch strategy {
	case "", "rename", "merge", "skip":
		return strategy, nil
	default:
		return "", fmt.Errorf("--strategy must be rename, merge or skip")
	}
}

func daemonImportStrategy(cmd *cobra.Command) (string, error) {
	strategy, _ := cmd.Flags().GetString("strategy")
	switch strategy {
	case "", "fail", "overwrite", "rename":
		return strategy, nil
	default:
		return "", fmt.Errorf("--strategy must be fail, overwrite or rename")
	}
}

func runAutopilotImport(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	strategy, err := daemonImportStrategy(cmd)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("read %s: %w", args[0], err)
	}
	body := map[string]any{"markdown": string(raw), "strategy": strategy}
	if preview, _ := cmd.Flags().GetBool("preview"); preview {
		var resp struct {
			Valid  bool `json:"valid"`
			Errors []struct {
				Line    int    `json:"line"`
				Message string `json:"message"`
			} `json:"errors"`
			Warnings            []string `json:"warnings"`
			AgentID             string   `json:"agent_id"`
			AgentCandidates     []string `json:"agent_candidates"`
			Digest              string   `json:"digest"`
			ExistingAutopilotID string   `json:"existing_autopilot_id"`
			Unchanged           bool     `json:"unchanged"`
		}
		if err := client.PostJSON(cmd.Context(), "/api/autopilots/import/preview", body, &resp); err != nil {
			return fmt.Errorf("preview daemon import: %w", err)
		}
		if jsonOutput(cmd) {
			return cli.PrintJSON(os.Stdout, resp)
		}
		if !resp.Valid {
			for _, e := range resp.Errors {
				fmt.Fprintf(os.Stderr, "%s:%d\t%s\n", args[0], e.Line, e.Message)
			}
			return fmt.Errorf("%s is not a valid declaration", args[0])
		}
		fmt.Fprintf(os.Stdout, "valid\tdigest %s\n", resp.Digest)
		if resp.AgentID != "" {
			fmt.Fprintf(os.Stdout, "agent\t%s\n", resp.AgentID)
		} else if len(resp.AgentCandidates) > 0 {
			fmt.Fprintf(os.Stderr, "the agent this file names does not exist; this workspace has: %v\n", resp.AgentCandidates)
		}
		if resp.ExistingAutopilotID != "" {
			state := "would be replaced (--strategy overwrite) or kept alongside (rename)"
			if resp.Unchanged {
				state = "already carries this exact declaration: importing changes nothing"
			}
			fmt.Fprintf(os.Stdout, "existing\t%s\t%s\n", resp.ExistingAutopilotID, state)
		}
		for _, warning := range resp.Warnings {
			fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
		}
		return nil
	}
	var resp struct {
		Status    string `json:"status"`
		Autopilot struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"autopilot"`
		Triggers []struct {
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			Enabled bool   `json:"enabled"`
		} `json:"triggers"`
		SkillID  string   `json:"skill_id"`
		Digest   string   `json:"digest"`
		Warnings []string `json:"warnings"`
	}
	if err := client.PostJSON(cmd.Context(), "/api/autopilots/import", body, &resp); err != nil {
		return fmt.Errorf("import daemon: %w", err)
	}
	if jsonOutput(cmd) {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", resp.Autopilot.ID, resp.Status, resp.Autopilot.Title)
	rows := make([][]string, 0, len(resp.Triggers))
	for _, t := range resp.Triggers {
		rows = append(rows, []string{t.ID, t.Kind, strconv.FormatBool(t.Enabled)})
	}
	cli.PrintTable(os.Stdout, []string{"TRIGGER", "KIND", "ENABLED"}, rows)
	for _, warning := range resp.Warnings {
		fmt.Fprintf(os.Stderr, "warning: %s\n", warning)
	}
	return nil
}
