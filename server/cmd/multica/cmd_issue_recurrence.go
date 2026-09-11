package main

// `multica issue recurrence set|show|clear` — the CLI half of recurring
// issues (JEF-375). A rule lives on one issue (the source) and every
// occurrence it spawns carries it, so any member of the series answers the
// same `show`.
//
// The server owns every rule: the cron parser, the IANA timezone, the two
// modes, and the fact that only a member may write one (a run reads and
// asks). This file resolves the issue, turns the preset flags into the cron
// the server expects, and renders the answer. The refusals worth catching
// are 403 (a run tried to write) and 404 (the issue does not recur): both
// are product answers a caller has to read, not transport faults.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

var issueRecurrenceCmd = &cobra.Command{
	Use:   "recurrence",
	Short: "Repeat an issue on a schedule, or when it closes",
}

var issueRecurrenceSetCmd = &cobra.Command{
	Use:   "set <issue>",
	Short: "Make this issue repeat",
	Long: "Create the recurrence rule on this issue, or update the rule of the\n" +
		"series it already belongs to.\n\n" +
		"The schedule is a 5-field cron read in --timezone (default UTC):\n" +
		"  --cron \"0 9 * * 1\" --timezone Europe/Paris\n\n" +
		"Presets write that cron for you (24-hour HH:MM, in --timezone):\n" +
		"  --daily 09:00           →  0 9 * * *\n" +
		"  --weekdays 09:00        →  0 9 * * 1-5\n" +
		"  --weekly \"MON 09:00\"    →  0 9 * * 1\n" +
		"  --monthly \"1 09:00\"     →  0 9 1 * *\n" +
		"Use one preset at a time. An explicit --cron wins over any preset.\n\n" +
		"--mode on_close drops the clock: the next occurrence is created when the\n" +
		"current one reaches a done or cancelled status. --disabled files the rule\n" +
		"without arming it.\n\n" +
		"Refusals you may see:\n" +
		"  400  the cron, the timezone or the mode is not one the server runs\n" +
		"  403  a run tried to write a rule — read it and ask a member",
	Args: exactArgs(1),
	RunE: runIssueRecurrenceSet,
}

var issueRecurrenceShowCmd = &cobra.Command{
	Use:   "show <issue>",
	Short: "Show the recurrence rule of this issue's series",
	Long: "Print the rule, the issue it was set on, the latest occurrences and the\n" +
		"next runs. Any issue of the series answers the same rule.\n\n" +
		"An issue with no rule answers 404 \"this issue does not recur\".",
	Args: exactArgs(1),
	RunE: runIssueRecurrenceShow,
}

var issueRecurrenceClearCmd = &cobra.Command{
	Use:   "clear <issue>",
	Short: "Stop the series",
	Long: "Clear the recurrence rule. The series stops spawning; the occurrences\n" +
		"already created stay as ordinary issues.",
	Args: exactArgs(1),
	RunE: runIssueRecurrenceClear,
}

// recurrencePayload is the answer GET and PUT both return.
type recurrencePayload struct {
	Recurrence struct {
		ID              string  `json:"id"`
		IssueID         string  `json:"issue_id"`
		CronExpression  string  `json:"cron_expression"`
		Timezone        string  `json:"timezone"`
		Mode            string  `json:"mode"`
		Enabled         bool    `json:"enabled"`
		NextRunAt       *string `json:"next_run_at"`
		OccurrenceCount int     `json:"occurrence_count"`
	} `json:"recurrence"`
	Source struct {
		ID         string `json:"id"`
		Identifier string `json:"identifier"`
		Title      string `json:"title"`
	} `json:"source"`
	Occurrences []struct {
		ID         string  `json:"id"`
		Identifier string  `json:"identifier"`
		Title      string  `json:"title"`
		Status     string  `json:"status"`
		CreatedAt  string  `json:"created_at"`
		DueDate    *string `json:"due_date"`
	} `json:"occurrences"`
	NextRuns []string `json:"next_runs"`
}

var recurrenceWeekdays = map[string]int{
	"SUN": 0, "MON": 1, "TUE": 2, "WED": 3, "THU": 4, "FRI": 5, "SAT": 6,
	"SUNDAY": 0, "MONDAY": 1, "TUESDAY": 2, "WEDNESDAY": 3, "THURSDAY": 4, "FRIDAY": 5, "SATURDAY": 6,
}

// parseClock reads a 24-hour HH:MM into its two cron fields.
func parseClock(flag, value string) (hour, minute int, err error) {
	h, m, ok := strings.Cut(strings.TrimSpace(value), ":")
	if !ok {
		return 0, 0, fmt.Errorf("%s takes a 24-hour time HH:MM, got %q", flag, value)
	}
	hour, err = strconv.Atoi(strings.TrimSpace(h))
	if err != nil || hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("%s: %q is not an hour between 00 and 23", flag, h)
	}
	minute, err = strconv.Atoi(strings.TrimSpace(m))
	if err != nil || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("%s: %q is not a minute between 00 and 59", flag, m)
	}
	return hour, minute, nil
}

// splitPresetArg splits "MON 09:00" / "1 09:00" into its two halves.
func splitPresetArg(flag, value string) (first, clock string, err error) {
	fields := strings.Fields(strings.ReplaceAll(strings.TrimSpace(value), ",", " "))
	if len(fields) != 2 {
		return "", "", fmt.Errorf("%s takes two values in one argument, e.g. %s \"MON 09:00\"", flag, flag)
	}
	return fields[0], fields[1], nil
}

// buildRecurrenceCron turns whichever preset flag was set into a 5-field
// cron. It returns "" when none was: the caller then needs --cron, or a mode
// that has no clock.
func buildRecurrenceCron(daily, weekdays, weekly, monthly string) (string, error) {
	presets := []struct{ flag, value string }{
		{"--daily", daily}, {"--weekdays", weekdays}, {"--weekly", weekly}, {"--monthly", monthly},
	}
	given := make([]string, 0, len(presets))
	for _, p := range presets {
		if strings.TrimSpace(p.value) != "" {
			given = append(given, p.flag)
		}
	}
	if len(given) > 1 {
		return "", fmt.Errorf("%s were given together; a recurrence has one schedule, pick one (or write it yourself with --cron)", strings.Join(given, " and "))
	}
	if len(given) == 0 {
		return "", nil
	}
	switch given[0] {
	case "--daily":
		h, m, err := parseClock("--daily", daily)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d %d * * *", m, h), nil
	case "--weekdays":
		h, m, err := parseClock("--weekdays", weekdays)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d %d * * 1-5", m, h), nil
	case "--weekly":
		day, clock, err := splitPresetArg("--weekly", weekly)
		if err != nil {
			return "", err
		}
		dow, ok := recurrenceWeekdays[strings.ToUpper(day)]
		if !ok {
			return "", fmt.Errorf("--weekly: %q is not a weekday; write MON, TUE, WED, THU, FRI, SAT or SUN", day)
		}
		h, m, err := parseClock("--weekly", clock)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d %d * * %d", m, h, dow), nil
	default:
		dayOfMonth, clock, err := splitPresetArg("--monthly", monthly)
		if err != nil {
			return "", err
		}
		dom, err := strconv.Atoi(strings.TrimSpace(dayOfMonth))
		if err != nil || dom < 1 || dom > 31 {
			return "", fmt.Errorf("--monthly: %q is not a day of the month between 1 and 31", dayOfMonth)
		}
		h, m, err := parseClock("--monthly", clock)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%d %d %d * *", m, h, dom), nil
	}
}

// recurrenceRefusal surfaces the two refusals that are product answers: a
// run that may not write (403) and an issue with no rule (404).
func recurrenceRefusal(action, display string, err error) error {
	var httpErr *cli.HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusForbidden:
			return fmt.Errorf("%s: %s — read it with `multica issue recurrence show %s` and ask a member", display, serverErrorMessage(httpErr), display)
		case http.StatusNotFound:
			return fmt.Errorf("%s: %s", display, serverErrorMessage(httpErr))
		}
	}
	return fmt.Errorf("%s: %w", action, err)
}

func runIssueRecurrenceSet(cmd *cobra.Command, args []string) error {
	cron, _ := cmd.Flags().GetString("cron")
	cron = strings.TrimSpace(cron)
	if cron == "" {
		daily, _ := cmd.Flags().GetString("daily")
		weekdays, _ := cmd.Flags().GetString("weekdays")
		weekly, _ := cmd.Flags().GetString("weekly")
		monthly, _ := cmd.Flags().GetString("monthly")
		built, err := buildRecurrenceCron(daily, weekdays, weekly, monthly)
		if err != nil {
			return err
		}
		cron = built
	}
	mode, _ := cmd.Flags().GetString("mode")
	mode = strings.TrimSpace(mode)
	if cron == "" && mode != "on_close" {
		return fmt.Errorf("a scheduled recurrence needs a cron: --cron \"0 9 * * 1\", or a preset (--daily, --weekdays, --weekly, --monthly). Use --mode on_close to repeat when the issue closes instead")
	}

	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}

	disabled, _ := cmd.Flags().GetBool("disabled")
	body := map[string]any{"enabled": !disabled}
	if cron != "" {
		body["cron_expression"] = cron
	}
	if tz, _ := cmd.Flags().GetString("timezone"); strings.TrimSpace(tz) != "" {
		body["timezone"] = strings.TrimSpace(tz)
	}
	if mode != "" {
		body["mode"] = mode
	}

	var resp recurrencePayload
	if err := client.PutJSON(ctx, "/api/issues/"+issueRef.ID+"/recurrence", body, &resp); err != nil {
		return recurrenceRefusal("set recurrence", issueRef.Display, err)
	}
	return printRecurrence(cmd, resp)
}

func runIssueRecurrenceShow(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	var resp recurrencePayload
	if err := client.GetJSON(ctx, "/api/issues/"+issueRef.ID+"/recurrence", &resp); err != nil {
		return recurrenceRefusal("read recurrence", issueRef.Display, err)
	}
	return printRecurrence(cmd, resp)
}

func runIssueRecurrenceClear(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()

	issueRef, err := resolveIssueRef(ctx, client, args[0])
	if err != nil {
		return fmt.Errorf("resolve issue: %w", err)
	}
	if err := client.DeleteJSON(ctx, "/api/issues/"+issueRef.ID+"/recurrence"); err != nil {
		return recurrenceRefusal("clear recurrence", issueRef.Display, err)
	}
	fmt.Fprintf(os.Stdout, "%s no longer recurs. The occurrences already created stay as ordinary issues.\n", issueRef.Display)
	return nil
}

func printRecurrence(cmd *cobra.Command, resp recurrencePayload) error {
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	rec := resp.Recurrence
	state := "enabled"
	if !rec.Enabled {
		state = "disabled"
	}
	source := resp.Source.Identifier
	if source == "" {
		source = resp.Source.ID
	}
	if rec.Mode == "on_close" {
		fmt.Fprintf(os.Stdout, "%s repeats when the current occurrence closes (%s, %s).\n", source, rec.Timezone, state)
	} else {
		fmt.Fprintf(os.Stdout, "%s repeats on %q in %s (%s).\n", source, rec.CronExpression, rec.Timezone, state)
	}
	if resp.Source.Title != "" {
		fmt.Fprintf(os.Stdout, "Source: %s — %s\n", source, resp.Source.Title)
	}
	fmt.Fprintf(os.Stdout, "Occurrences so far: %d\n", rec.OccurrenceCount)
	if len(resp.NextRuns) > 0 {
		fmt.Fprintf(os.Stdout, "Next runs: %s\n", strings.Join(resp.NextRuns, ", "))
	}
	if len(resp.Occurrences) == 0 {
		return nil
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	headers := []string{"ID", "ISSUE", "TITLE", "STATUS", "CREATED", "DUE"}
	rows := make([][]string, 0, len(resp.Occurrences))
	for _, o := range resp.Occurrences {
		due := ""
		if o.DueDate != nil {
			due = *o.DueDate
		}
		rows = append(rows, []string{displayID(o.ID, fullID), o.Identifier, o.Title, o.Status, o.CreatedAt, due})
	}
	fmt.Fprintln(os.Stdout)
	cli.PrintTable(os.Stdout, headers, rows)
	return nil
}

func init() {
	issueRecurrenceSetCmd.Flags().String("cron", "", "5-field cron read in --timezone, e.g. \"0 9 * * 1\". Wins over any preset.")
	issueRecurrenceSetCmd.Flags().String("daily", "", "Preset: every day at HH:MM (24-hour), e.g. --daily 09:00")
	issueRecurrenceSetCmd.Flags().String("weekdays", "", "Preset: Monday to Friday at HH:MM, e.g. --weekdays 09:00")
	issueRecurrenceSetCmd.Flags().String("weekly", "", "Preset: one weekday at HH:MM, e.g. --weekly \"MON 09:00\"")
	issueRecurrenceSetCmd.Flags().String("monthly", "", "Preset: one day of the month at HH:MM, e.g. --monthly \"1 09:00\"")
	issueRecurrenceSetCmd.Flags().String("timezone", "", "IANA timezone the schedule is read in (default UTC)")
	issueRecurrenceSetCmd.Flags().String("mode", "", "schedule (the cron fires) or on_close (the next occurrence is created when the current one closes)")
	issueRecurrenceSetCmd.Flags().Bool("disabled", false, "File the rule without arming it")
	issueRecurrenceSetCmd.Flags().String("output", "table", "Output format: table or json")
	issueRecurrenceSetCmd.Flags().Bool("full-id", false, "Show full UUIDs instead of short ids")

	issueRecurrenceShowCmd.Flags().String("output", "table", "Output format: table or json")
	issueRecurrenceShowCmd.Flags().Bool("full-id", false, "Show full UUIDs instead of short ids")

	issueRecurrenceCmd.AddCommand(issueRecurrenceSetCmd, issueRecurrenceShowCmd, issueRecurrenceClearCmd)
	issueCmd.AddCommand(issueRecurrenceCmd)
}
