package icalendar

import (
	"strings"
	"testing"
	"time"
)

func TestWriteRendersAFeedClientsAccept(t *testing.T) {
	start := time.Date(2026, 9, 10, 14, 0, 0, 0, time.UTC)
	out := Write("Vigil · one", []OutboundEvent{
		{UID: "e1@vigil", Summary: "Design review; with Ada, Bob", Description: "Line one\nLine two", Location: "Room 4", URL: "https://vigil.example/one/calendar", Start: start, End: start.Add(time.Hour), Status: "CONFIRMED", Stamp: start},
		{UID: "e2@vigil", Summary: strings.Repeat("long ", 30), Start: start, End: start.Add(24 * time.Hour), AllDay: true, Stamp: start},
	})
	for _, want := range []string{
		"BEGIN:VCALENDAR\r\nVERSION:2.0\r\n", "X-WR-CALNAME:Vigil · one\r\n",
		"UID:e1@vigil\r\n", "DTSTART:20260910T140000Z\r\n", "DTEND:20260910T150000Z\r\n",
		"SUMMARY:Design review\\; with Ada\\, Bob\r\n", "DESCRIPTION:Line one\\nLine two\r\n", "LOCATION:Room 4\r\n", "STATUS:CONFIRMED\r\n",
		"DTSTART;VALUE=DATE:20260910\r\n", "DTEND;VALUE=DATE:20260911\r\n", "END:VCALENDAR\r\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("feed lacks %q:\n%s", want, out)
		}
	}
	for _, l := range strings.Split(out, "\r\n") {
		if len(l) > 75 {
			t.Errorf("line over 75 octets: %q", l)
		}
	}
	// The feed we write parses with the reader we already trust.
	events, err := Parse([]byte(out), start.Add(-time.Hour), start.Add(48*time.Hour))
	if err != nil || len(events) != 2 {
		t.Fatalf("parse back: %v, %d events", err, len(events))
	}
	if FeedFilename("Vigil · One!") != "vigil---one.ics" {
		t.Fatalf("filename = %q", FeedFilename("Vigil · One!"))
	}
}
