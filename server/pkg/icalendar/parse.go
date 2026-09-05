// Package icalendar reads the VEVENTs out of an iCalendar (RFC 5545) feed.
//
// Deliberately small. Multica subscribes to a read-only ICS URL to answer one
// question — "is a meeting happening right now, and what is it called?" — so
// this parses what that needs (DTSTART, DTEND/DURATION, SUMMARY, URL, simple
// RRULEs, EXDATE) and ignores the rest of the specification: VTODO, VALARM,
// VTIMEZONE definitions, attendees, RDATE, RECURRENCE-ID overrides, and
// monthly or yearly recurrence.
//
// The consequences of that are all in the same direction — an event this
// package misses is an event the caller does not name — except for a
// recurring meeting cancelled through a RECURRENCE-ID override, which still
// shows. A wrong title on one meeting is worth the several thousand lines a
// complete implementation costs.
package icalendar

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Event is one occurrence, with absolute times.
type Event struct {
	Summary string
	URL     string
	Start   time.Time
	End     time.Time
}

// Active reports whether the event covers t. The start is inclusive and the
// end exclusive, so back-to-back meetings never both match.
func (e Event) Active(t time.Time) bool {
	return !t.Before(e.Start) && t.Before(e.End)
}

const (
	// maxOccurrences bounds one recurring event's expansion. A daily event
	// over the longest window this package is asked for is 14; anything past
	// this is a malformed RRULE, not a calendar.
	maxOccurrences = 400
	// maxEvents bounds one feed. A shared team calendar is hundreds of events
	// a year, and the caller only ever reads a two-week window out of it.
	maxEvents = 5000
	// defaultDuration is how long a timed event with neither DTEND nor
	// DURATION is assumed to last. RFC 5545 says such an event is
	// zero-length, which would make it impossible to ever be "in progress" —
	// the one question this package exists to answer.
	defaultDuration = time.Hour
)

// Parse returns every occurrence that overlaps [from, until), sorted by start
// time. Recurrences are expanded within that window.
func Parse(data []byte, from, until time.Time) ([]Event, error) {
	if until.Before(from) {
		return nil, errors.New("icalendar: until is before from")
	}
	lines, err := unfold(data)
	if err != nil {
		return nil, err
	}
	var (
		out     []Event
		current map[string]property
		inEvent bool
	)
	for _, line := range lines {
		name, prop, ok := parseLine(line)
		if !ok {
			continue
		}
		switch {
		case name == "BEGIN" && prop.value == "VEVENT":
			inEvent, current = true, map[string]property{}
			continue
		case name == "END" && prop.value == "VEVENT":
			if inEvent {
				out = append(out, expand(current, from, until)...)
			}
			inEvent, current = false, nil
			if len(out) >= maxEvents {
				return sortByStart(out[:maxEvents]), nil
			}
			continue
		}
		if !inEvent {
			continue
		}
		// EXDATE may legally repeat; everything else this reads may not, and
		// the first value wins so a malformed repeat cannot shift an event.
		if name == "EXDATE" {
			existing := current[name]
			existing.value = strings.TrimPrefix(existing.value+","+prop.value, ",")
			if existing.params == nil {
				existing.params = prop.params
			}
			current[name] = existing
			continue
		}
		if _, seen := current[name]; !seen {
			current[name] = prop
		}
	}
	return sortByStart(out), nil
}

func sortByStart(events []Event) []Event {
	for i := 1; i < len(events); i++ {
		for j := i; j > 0 && events[j].Start.Before(events[j-1].Start); j-- {
			events[j], events[j-1] = events[j-1], events[j]
		}
	}
	return events
}

type property struct {
	params map[string]string
	value  string
}

// unfold joins continuation lines (a line beginning with a space or tab
// continues the previous one) and normalizes line endings.
func unfold(data []byte) ([]string, error) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// One folded property can legally be long; a 1 MiB single line is not a
	// calendar, and the caller already bounds the whole document.
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var out []string
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		if (line[0] == ' ' || line[0] == '\t') && len(out) > 0 {
			out[len(out)-1] += line[1:]
			continue
		}
		out = append(out, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("icalendar: read: %w", err)
	}
	return out, nil
}

// parseLine splits `NAME;PARAM=value:content` into its parts. Parameter
// values may be quoted, and a quoted one may contain a colon (TZID is the
// common case), so the content separator is the first colon outside quotes.
func parseLine(line string) (string, property, bool) {
	quoted := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			quoted = !quoted
		case ':':
			if quoted {
				continue
			}
			head, value := line[:i], line[i+1:]
			name, params := parseParams(head)
			if name == "" {
				return "", property{}, false
			}
			return name, property{params: params, value: value}, true
		}
	}
	return "", property{}, false
}

func parseParams(head string) (string, map[string]string) {
	parts := splitOutsideQuotes(head, ';')
	if len(parts) == 0 {
		return "", nil
	}
	name := strings.ToUpper(strings.TrimSpace(parts[0]))
	if len(parts) == 1 {
		return name, nil
	}
	params := map[string]string{}
	for _, raw := range parts[1:] {
		key, value, ok := strings.Cut(raw, "=")
		if !ok {
			continue
		}
		params[strings.ToUpper(strings.TrimSpace(key))] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	return name, params
}

func splitOutsideQuotes(s string, sep byte) []string {
	var out []string
	quoted, start := false, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			quoted = !quoted
		case sep:
			if !quoted {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

// unescapeText applies the RFC 5545 TEXT escapes.
func unescapeText(s string) string {
	return strings.NewReplacer(`\n`, "\n", `\N`, "\n", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(s)
}

// parseTime reads one DATE or DATE-TIME value.
//
// A value with no zone and no TZID is "floating": RFC 5545 says it happens at
// that wall-clock time wherever it is read. There is no better answer here
// than the server's own zone, which is what time.ParseInLocation with
// time.Local gives — and the callers of this package all run on the server.
func parseTime(prop property) (time.Time, bool, error) {
	value := strings.TrimSpace(prop.value)
	if value == "" {
		return time.Time{}, false, errors.New("icalendar: empty date")
	}
	loc := time.Local
	if tzid := prop.params["TZID"]; tzid != "" {
		if l, err := time.LoadLocation(tzid); err == nil {
			loc = l
		}
	}
	if prop.params["VALUE"] == "DATE" || len(value) == 8 {
		t, err := time.ParseInLocation("20060102", value, loc)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("icalendar: date %q: %w", value, err)
		}
		return t, true, nil
	}
	if strings.HasSuffix(value, "Z") {
		t, err := time.ParseInLocation("20060102T150405Z", value, time.UTC)
		if err != nil {
			return time.Time{}, false, fmt.Errorf("icalendar: utc date-time %q: %w", value, err)
		}
		return t, false, nil
	}
	t, err := time.ParseInLocation("20060102T150405", value, loc)
	if err != nil {
		return time.Time{}, false, fmt.Errorf("icalendar: date-time %q: %w", value, err)
	}
	return t, false, nil
}

// parseDuration reads the RFC 5545 DURATION form (P[n]DT[n]H[n]M[n]S, plus
// weeks). Anything it cannot read is reported so the caller falls back.
func parseDuration(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	negative := strings.HasPrefix(value, "-")
	value = strings.TrimLeft(value, "+-")
	if !strings.HasPrefix(value, "P") {
		return 0, false
	}
	var total time.Duration
	number := ""
	for _, r := range value[1:] {
		if r >= '0' && r <= '9' {
			number += string(r)
			continue
		}
		if r == 'T' {
			continue
		}
		n, err := strconv.Atoi(number)
		if err != nil {
			return 0, false
		}
		number = ""
		switch r {
		case 'W':
			total += time.Duration(n) * 7 * 24 * time.Hour
		case 'D':
			total += time.Duration(n) * 24 * time.Hour
		case 'H':
			total += time.Duration(n) * time.Hour
		case 'M':
			total += time.Duration(n) * time.Minute
		case 'S':
			total += time.Duration(n) * time.Second
		default:
			return 0, false
		}
	}
	if number != "" {
		return 0, false
	}
	if negative {
		total = -total
	}
	return total, total > 0
}

// expand turns one VEVENT into the occurrences overlapping [from, until).
func expand(props map[string]property, from, until time.Time) []Event {
	startProp, ok := props["DTSTART"]
	if !ok {
		return nil
	}
	start, allDay, err := parseTime(startProp)
	if err != nil {
		return nil
	}
	end := eventEnd(props, start, allDay)
	base := Event{
		Summary: strings.TrimSpace(unescapeText(props["SUMMARY"].value)),
		URL:     strings.TrimSpace(props["URL"].value),
		Start:   start,
		End:     end,
	}
	excluded := exclusions(props)

	var out []Event
	add := func(e Event) {
		if e.Start.Before(until) && e.End.After(from) && !excluded[e.Start.Unix()] {
			out = append(out, e)
		}
	}
	rule, recurring := parseRRule(props["RRULE"].value)
	if !recurring {
		add(base)
		return out
	}
	duration := end.Sub(start)
	for i, at := 0, start; i < maxOccurrences; i++ {
		if !rule.until.IsZero() && at.After(rule.until) {
			break
		}
		if rule.count > 0 && i >= rule.count {
			break
		}
		if !at.Before(until) {
			break
		}
		occurrence := base
		occurrence.Start, occurrence.End = at, at.Add(duration)
		add(occurrence)
		next, ok := rule.next(at)
		if !ok {
			break
		}
		at = next
	}
	return out
}

func eventEnd(props map[string]property, start time.Time, allDay bool) time.Time {
	if endProp, ok := props["DTEND"]; ok {
		if end, _, err := parseTime(endProp); err == nil && end.After(start) {
			return end
		}
	}
	if d, ok := parseDuration(props["DURATION"].value); ok {
		return start.Add(d)
	}
	if allDay {
		return start.AddDate(0, 0, 1)
	}
	return start.Add(defaultDuration)
}

// exclusions indexes EXDATE by the start instant it cancels.
func exclusions(props map[string]property) map[int64]bool {
	prop, ok := props["EXDATE"]
	if !ok {
		return nil
	}
	out := map[int64]bool{}
	for _, value := range strings.Split(prop.value, ",") {
		if t, _, err := parseTime(property{params: prop.params, value: value}); err == nil {
			out[t.Unix()] = true
		}
	}
	return out
}

// rrule is the subset of RRULE this package expands: daily and weekly, with
// INTERVAL, COUNT, UNTIL and (weekly) BYDAY. A monthly or yearly rule is not
// recognized, so the event contributes only its first occurrence.
type rrule struct {
	weekly   bool
	interval int
	count    int
	until    time.Time
	byDay    map[time.Weekday]bool
}

var weekdayCodes = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

func parseRRule(value string) (rrule, bool) {
	if strings.TrimSpace(value) == "" {
		return rrule{}, false
	}
	out := rrule{interval: 1}
	freq := ""
	for _, part := range strings.Split(value, ";") {
		key, raw, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(key)) {
		case "FREQ":
			freq = strings.ToUpper(strings.TrimSpace(raw))
		case "INTERVAL":
			if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && n > 0 {
				out.interval = n
			}
		case "COUNT":
			if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && n > 0 {
				out.count = n
			}
		case "UNTIL":
			if t, _, err := parseTime(property{value: strings.TrimSpace(raw)}); err == nil {
				out.until = t
			}
		case "BYDAY":
			out.byDay = map[time.Weekday]bool{}
			for _, code := range strings.Split(raw, ",") {
				// Drop an ordinal prefix ("2MO"): this package does not do
				// "the second Monday", and treating it as a plain Monday
				// would invent occurrences.
				code = strings.TrimSpace(strings.ToUpper(code))
				if day, ok := weekdayCodes[code]; ok {
					out.byDay[day] = true
				}
			}
			if len(out.byDay) == 0 {
				out.byDay = nil
			}
		}
	}
	switch freq {
	case "DAILY":
		return out, true
	case "WEEKLY":
		out.weekly = true
		return out, true
	default:
		return rrule{}, false
	}
}

// next returns the occurrence after `at`, keeping the time of day.
func (r rrule) next(at time.Time) (time.Time, bool) {
	if !r.weekly {
		return at.AddDate(0, 0, r.interval), true
	}
	if len(r.byDay) == 0 {
		return at.AddDate(0, 0, 7*r.interval), true
	}
	// Walk day by day to the next selected weekday, jumping the skipped weeks
	// of an INTERVAL greater than one.
	for i := 1; i <= 7*r.interval+7; i++ {
		candidate := at.AddDate(0, 0, i)
		if !r.byDay[candidate.Weekday()] {
			continue
		}
		if r.interval > 1 && weeksBetween(at, candidate)%r.interval != 0 {
			continue
		}
		return candidate, true
	}
	return time.Time{}, false
}

// weeksBetween counts whole weeks between two days, from Sunday to Sunday, so
// an INTERVAL of 2 skips the right week rather than the right 14 days.
func weeksBetween(a, b time.Time) int {
	startOfWeek := func(t time.Time) time.Time {
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		return day.AddDate(0, 0, -int(day.Weekday()))
	}
	return int(startOfWeek(b).Sub(startOfWeek(a)).Hours() / 24 / 7)
}
