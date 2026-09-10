package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/multica-ai/multica/server/internal/cli"
)

// Native calendar (OS plan, chantier 19) from the terminal: the agenda, the
// events of a window, free slots for people and agents, a proposal.

var calendarCmd = &cobra.Command{
	Use:   "calendar",
	Short: "The workspace calendar: agenda, events, free slots, proposals",
}

var calendarAgendaCmd = &cobra.Command{
	Use:   "agenda",
	Short: "Everything dated in a window: events, issues due, cycles, meetings",
	Args:  exactArgs(0),
	RunE:  runCalendarAgenda,
}

var calendarEventsCmd = &cobra.Command{
	Use:   "events",
	Short: "Events overlapping a window",
	Args:  exactArgs(0),
	RunE:  runCalendarEvents,
}

var calendarSlotsCmd = &cobra.Command{
	Use:   "slots",
	Short: "Free windows every participant can make",
	Long: "Members are free inside 09:00-18:00 on weekdays in --tz, outside their events; " +
		"agents are free outside their events at any hour. --participants takes member:<user id> or agent:<agent id>, comma-separated.",
	Args: exactArgs(0),
	RunE: runCalendarSlots,
}

var calendarProposeCmd = &cobra.Command{
	Use:   "propose",
	Short: "Propose an event on an issue (a person accepts); a member schedules it directly",
	Args:  exactArgs(0),
	RunE:  runCalendarPropose,
}

func init() {
	for _, c := range []*cobra.Command{calendarAgendaCmd, calendarEventsCmd, calendarSlotsCmd} {
		c.Flags().String("from", "", "window start, RFC 3339 (default now)")
		c.Flags().String("to", "", "window end, RFC 3339 (default a week ahead)")
	}
	for _, c := range []*cobra.Command{calendarAgendaCmd, calendarEventsCmd, calendarSlotsCmd, calendarProposeCmd} {
		c.Flags().String("output", "table", "Output format: table or json")
	}
	calendarEventsCmd.Flags().Bool("full-id", false, "print full ids")
	calendarSlotsCmd.Flags().String("participants", "", "member:<user id>,agent:<agent id>")
	calendarSlotsCmd.Flags().Int("duration", 30, "minutes")
	calendarSlotsCmd.Flags().String("tz", "UTC", "IANA zone for working hours")
	calendarProposeCmd.Flags().String("issue", "", "issue id or identifier (required for an agent)")
	calendarProposeCmd.Flags().String("title", "", "title")
	calendarProposeCmd.Flags().String("starts", "", "start, RFC 3339")
	calendarProposeCmd.Flags().String("ends", "", "end, RFC 3339")
	calendarProposeCmd.Flags().String("description", "", "what the event is for")
	calendarProposeCmd.Flags().String("timezone", "UTC", "IANA zone the times are shown in")
	calendarProposeCmd.Flags().String("location", "", "place or link")
	calendarProposeCmd.Flags().String("participants", "", "member:<user id>,agent:<agent id>")
	calendarCmd.AddCommand(calendarAgendaCmd, calendarEventsCmd, calendarSlotsCmd, calendarProposeCmd)
}

func calendarWindowQuery(cmd *cobra.Command) string {
	from, _ := cmd.Flags().GetString("from")
	to, _ := cmd.Flags().GetString("to")
	if from == "" {
		from = time.Now().UTC().Format(time.RFC3339)
	}
	if to == "" {
		t, _ := time.Parse(time.RFC3339, from)
		to = t.Add(7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	}
	return "from=" + from + "&to=" + to
}

func runCalendarAgenda(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var resp map[string]any
	if err := client.GetJSON(cmd.Context(), "/api/calendar/agenda?"+calendarWindowQuery(cmd), &resp); err != nil {
		return fmt.Errorf("agenda: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	rows := [][]string{}
	if events, ok := resp["events"].([]any); ok {
		for _, e := range events {
			m, _ := e.(map[string]any)
			rows = append(rows, []string{"event", shortTime(strVal(m, "starts_at")), strVal(m, "title"), strVal(m, "status")})
		}
	}
	if issues, ok := resp["issues_due"].([]any); ok {
		for _, e := range issues {
			m, _ := e.(map[string]any)
			rows = append(rows, []string{"issue due", strVal(m, "due_date"), strVal(m, "identifier") + " " + strVal(m, "title"), strVal(m, "status")})
		}
	}
	if cycles, ok := resp["cycles"].([]any); ok {
		for _, e := range cycles {
			m, _ := e.(map[string]any)
			rows = append(rows, []string{"cycle", strVal(m, "start_date") + " → " + strVal(m, "end_date"), strVal(m, "name"), ""})
		}
	}
	if meetings, ok := resp["meetings"].([]any); ok {
		for _, e := range meetings {
			m, _ := e.(map[string]any)
			rows = append(rows, []string{"meeting", shortTime(strVal(m, "started_at")), strVal(m, "title"), strVal(m, "status")})
		}
	}
	cli.PrintTable(os.Stdout, []string{"KIND", "WHEN", "WHAT", "STATUS"}, rows)
	return nil
}

func runCalendarEvents(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	var resp struct {
		Events []map[string]any `json:"events"`
	}
	if err := client.GetJSON(cmd.Context(), "/api/calendar/events?"+calendarWindowQuery(cmd), &resp); err != nil {
		return fmt.Errorf("events: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	fullID, _ := cmd.Flags().GetBool("full-id")
	rows := make([][]string, 0, len(resp.Events))
	for _, e := range resp.Events {
		who := []string{}
		if parts, ok := e["participants"].([]any); ok {
			for _, p := range parts {
				m, _ := p.(map[string]any)
				name := strVal(m, "name")
				if name == "" {
					name = strVal(m, "type") + ":" + displayID(strVal(m, "id"), false)
				}
				who = append(who, name+" ("+strVal(m, "response")+")")
			}
		}
		rows = append(rows, []string{displayID(strVal(e, "id"), fullID), shortTime(strVal(e, "starts_at")), shortTime(strVal(e, "ends_at")), strVal(e, "title"), strVal(e, "status"), strings.Join(who, ", ")})
	}
	cli.PrintTable(os.Stdout, []string{"ID", "STARTS", "ENDS", "TITLE", "STATUS", "PARTICIPANTS"}, rows)
	return nil
}

func runCalendarSlots(cmd *cobra.Command, _ []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	participants, _ := cmd.Flags().GetString("participants")
	if strings.TrimSpace(participants) == "" {
		return fmt.Errorf("--participants is required")
	}
	duration, _ := cmd.Flags().GetInt("duration")
	tz, _ := cmd.Flags().GetString("tz")
	var resp struct {
		Slots []map[string]any `json:"slots"`
	}
	path := "/api/calendar/slots?" + calendarWindowQuery(cmd) + "&participants=" + participants + "&duration=" + strconv.Itoa(duration) + "&tz=" + tz
	if err := client.GetJSON(cmd.Context(), path, &resp); err != nil {
		return fmt.Errorf("slots: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	rows := make([][]string, 0, len(resp.Slots))
	for _, s := range resp.Slots {
		rows = append(rows, []string{shortTime(strVal(s, "starts_at")), shortTime(strVal(s, "ends_at"))})
	}
	cli.PrintTable(os.Stdout, []string{"STARTS", "ENDS"}, rows)
	if len(rows) == 0 {
		fmt.Fprintln(os.Stderr, "no free slot in this window")
	}
	return nil
}

func runCalendarPropose(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	title, _ := cmd.Flags().GetString("title")
	starts, _ := cmd.Flags().GetString("starts")
	ends, _ := cmd.Flags().GetString("ends")
	if strings.TrimSpace(title) == "" || starts == "" || ends == "" {
		return fmt.Errorf("--title, --starts and --ends are required")
	}
	body := map[string]any{"title": title, "starts_at": starts, "ends_at": ends}
	for _, key := range []string{"description", "timezone", "location"} {
		if v, _ := cmd.Flags().GetString(key); v != "" {
			body[key] = v
		}
	}
	if v, _ := cmd.Flags().GetString("issue"); v != "" {
		issueRef, err := resolveIssueRef(ctx, client, v)
		if err != nil {
			return fmt.Errorf("resolve issue: %w", err)
		}
		body["issue_id"] = issueRef.ID
	}
	if v, _ := cmd.Flags().GetString("participants"); v != "" {
		var parts []map[string]any
		for _, raw := range strings.Split(v, ",") {
			kind, id, ok := strings.Cut(strings.TrimSpace(raw), ":")
			if !ok {
				return fmt.Errorf("participants must be member:<id> or agent:<id>")
			}
			parts = append(parts, map[string]any{"type": kind, "id": id})
		}
		body["participants"] = parts
	}
	var resp map[string]any
	if err := client.PostJSON(ctx, "/api/calendar/events", body, &resp); err != nil {
		return fmt.Errorf("propose: %w", err)
	}
	if output, _ := cmd.Flags().GetString("output"); output == "json" {
		return cli.PrintJSON(os.Stdout, resp)
	}
	e, _ := resp["event"].(map[string]any)
	fmt.Fprintf(os.Stdout, "%s\t%s\t%s\n", strVal(e, "id"), strVal(e, "status"), strVal(e, "title"))
	if strVal(e, "status") == "proposed" {
		fmt.Fprintln(os.Stderr, "proposed: a person decides on the issue's Decision Card")
	}
	return nil
}

func shortTime(s string) string {
	if len(s) >= 16 {
		return strings.Replace(s[:16], "T", " ", 1)
	}
	return s
}
