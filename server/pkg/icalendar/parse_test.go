package icalendar

import (
	"strings"
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func feed(events ...string) []byte {
	return []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\n" + strings.Join(events, "") + "END:VCALENDAR\r\n")
}

func TestParseReadsOneEvent(t *testing.T) {
	data := feed(`BEGIN:VEVENT
UID:1@example.test
SUMMARY:Weekly sync with the platform team
URL:https://meet.example.test/abc
DTSTART:20260904T090000Z
DTEND:20260904T093000Z
END:VEVENT
`)
	events, err := Parse(data, at("2026-09-04T00:00:00Z"), at("2026-09-05T00:00:00Z"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	got := events[0]
	if got.Summary != "Weekly sync with the platform team" || got.URL != "https://meet.example.test/abc" {
		t.Fatalf("event = %+v", got)
	}
	if !got.Start.Equal(at("2026-09-04T09:00:00Z")) || !got.End.Equal(at("2026-09-04T09:30:00Z")) {
		t.Fatalf("times = %v..%v", got.Start, got.End)
	}
	// The start is inclusive and the end exclusive, so two back-to-back
	// meetings never both claim the same instant.
	if !got.Active(at("2026-09-04T09:00:00Z")) || got.Active(at("2026-09-04T09:30:00Z")) {
		t.Fatal("Active must be [start, end)")
	}
}

func TestParseUnfoldsLinesAndUnescapesText(t *testing.T) {
	// RFC 5545 folds long lines with a leading space, and escapes commas and
	// semicolons in TEXT values.
	data := feed("BEGIN:VEVENT\r\nSUMMARY:Retro\\, planning and\r\n  demo\\; all of it\r\n" +
		"DTSTART:20260904T090000Z\r\nDTEND:20260904T100000Z\r\nEND:VEVENT\r\n")
	events, err := Parse(data, at("2026-09-04T00:00:00Z"), at("2026-09-05T00:00:00Z"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 1 || events[0].Summary != "Retro, planning and demo; all of it" {
		t.Fatalf("summary = %q", events[0].Summary)
	}
}

func TestParseHandlesZonesDatesAndDurations(t *testing.T) {
	data := feed(`BEGIN:VEVENT
SUMMARY:Paris standup
DTSTART;TZID=Europe/Paris:20260904T110000
DTEND;TZID=Europe/Paris:20260904T112000
END:VEVENT
BEGIN:VEVENT
SUMMARY:Company offsite
DTSTART;VALUE=DATE:20260904
DTEND;VALUE=DATE:20260905
END:VEVENT
BEGIN:VEVENT
SUMMARY:Duration only
DTSTART:20260904T140000Z
DURATION:PT1H30M
END:VEVENT
BEGIN:VEVENT
SUMMARY:No end at all
DTSTART:20260904T160000Z
END:VEVENT
`)
	events, err := Parse(data, at("2026-09-04T00:00:00Z"), at("2026-09-06T00:00:00Z"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4", len(events))
	}
	byName := map[string]Event{}
	for _, e := range events {
		byName[e.Summary] = e
	}
	// 11:00 in Paris is 09:00 UTC in September (CEST).
	if !byName["Paris standup"].Start.Equal(at("2026-09-04T09:00:00Z")) {
		t.Fatalf("TZID start = %v", byName["Paris standup"].Start)
	}
	offsite := byName["Company offsite"]
	if d := offsite.End.Sub(offsite.Start); d != 24*time.Hour {
		t.Fatalf("all-day length = %v, want 24h", d)
	}
	duration := byName["Duration only"]
	if d := duration.End.Sub(duration.Start); d != 90*time.Minute {
		t.Fatalf("DURATION length = %v, want 1h30m", d)
	}
	// RFC 5545 makes a DTEND-less timed event zero-length, which could never
	// be "in progress" — the one question this package answers.
	noEnd := byName["No end at all"]
	if d := noEnd.End.Sub(noEnd.Start); d != time.Hour {
		t.Fatalf("default length = %v, want 1h", d)
	}
}

func TestParseExpandsDailyAndWeeklyRecurrence(t *testing.T) {
	data := feed(`BEGIN:VEVENT
SUMMARY:Daily standup
DTSTART:20260901T080000Z
DTEND:20260901T081500Z
RRULE:FREQ=DAILY
EXDATE:20260903T080000Z
END:VEVENT
BEGIN:VEVENT
SUMMARY:Tue/Thu pairing
DTSTART:20260901T130000Z
DTEND:20260901T140000Z
RRULE:FREQ=WEEKLY;BYDAY=TU,TH;COUNT=4
END:VEVENT
`)
	from, until := at("2026-09-01T00:00:00Z"), at("2026-09-08T00:00:00Z")
	events, err := Parse(data, from, until)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	daily, pairing := 0, 0
	for _, e := range events {
		switch e.Summary {
		case "Daily standup":
			daily++
			if e.Start.Equal(at("2026-09-03T08:00:00Z")) {
				t.Fatal("EXDATE occurrence must be dropped")
			}
		case "Tue/Thu pairing":
			pairing++
			if d := e.Start.Weekday(); d != time.Tuesday && d != time.Thursday {
				t.Fatalf("pairing on a %v", d)
			}
		}
	}
	// Sept 1..7 is seven days, minus the excluded one.
	if daily != 6 {
		t.Fatalf("daily occurrences = %d, want 6", daily)
	}
	// The COUNT of 4 starts on Tuesday Sept 1, so the window holds Tue 1,
	// Thu 3, and the following Tue 8 falls outside it.
	if pairing != 2 {
		t.Fatalf("pairing occurrences = %d, want 2 in the window", pairing)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Start.Before(events[i-1].Start) {
			t.Fatal("events must come back sorted by start")
		}
	}
}

func TestParseKeepsWhatItCannotExpandToOneOccurrence(t *testing.T) {
	// Monthly is out of scope: the event still shows once rather than
	// disappearing, and no occurrence is invented.
	data := feed(`BEGIN:VEVENT
SUMMARY:Monthly review
DTSTART:20260904T090000Z
DTEND:20260904T100000Z
RRULE:FREQ=MONTHLY;BYMONTHDAY=4
END:VEVENT
`)
	events, err := Parse(data, at("2026-09-01T00:00:00Z"), at("2026-12-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want the single first occurrence", len(events))
	}
}

func TestParseWindowsAndIgnoresJunk(t *testing.T) {
	data := feed(`BEGIN:VEVENT
SUMMARY:Yesterday
DTSTART:20260903T090000Z
DTEND:20260903T100000Z
END:VEVENT
BEGIN:VEVENT
SUMMARY:Broken
DTSTART:not-a-date
END:VEVENT
BEGIN:VEVENT
SUMMARY:No start at all
END:VEVENT
BEGIN:VTODO
SUMMARY:A task, not an event
DTSTART:20260904T090000Z
END:VTODO
BEGIN:VEVENT
SUMMARY:Today
DTSTART:20260904T090000Z
DTEND:20260904T100000Z
END:VEVENT
`)
	events, err := Parse(data, at("2026-09-04T00:00:00Z"), at("2026-09-05T00:00:00Z"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(events) != 1 || events[0].Summary != "Today" {
		t.Fatalf("events = %+v, want only Today", events)
	}
	if _, err := Parse(nil, at("2026-09-05T00:00:00Z"), at("2026-09-04T00:00:00Z")); err == nil {
		t.Fatal("an inverted window must be an error, not an empty result")
	}
	if events, err := Parse([]byte("not a calendar at all"), at("2026-09-04T00:00:00Z"), at("2026-09-05T00:00:00Z")); err != nil || len(events) != 0 {
		t.Fatalf("garbage = %v, %v; want no events and no error", events, err)
	}
}

// A TZID containing a colon must survive the property/value split.
func TestParseLineSplitsOnTheColonOutsideQuotes(t *testing.T) {
	name, prop, ok := parseLine(`DTSTART;TZID="America/New_York":20260904T090000`)
	if !ok || name != "DTSTART" || prop.value != "20260904T090000" {
		t.Fatalf("parseLine = %q, %+v, %v", name, prop, ok)
	}
	if prop.params["TZID"] != "America/New_York" {
		t.Fatalf("TZID = %q", prop.params["TZID"])
	}
}
