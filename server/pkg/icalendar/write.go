package icalendar

import (
	"fmt"
	"strings"
	"time"
)

// OutboundEvent is one VEVENT of a feed Multica publishes.
type OutboundEvent struct {
	UID         string
	Summary     string
	Description string
	Location    string
	URL         string
	Start       time.Time
	End         time.Time
	AllDay      bool
	Status      string // CONFIRMED | TENTATIVE | CANCELLED
	Stamp       time.Time
}

// Write renders a VCALENDAR the common clients subscribe to (RFC 5545
// subset: VEVENT with UID, DTSTAMP, DTSTART, DTEND, SUMMARY, DESCRIPTION,
// LOCATION, URL, STATUS). Times are emitted in UTC; all-day events use
// VALUE=DATE. Lines are folded at 75 octets and text is escaped per the RFC.
func Write(name string, events []OutboundEvent) string {
	var b strings.Builder
	line := func(s string) { b.WriteString(fold(s)); b.WriteString("\r\n") }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//Multica//Vigil calendar//EN")
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	if name != "" {
		line("X-WR-CALNAME:" + escape(name))
	}
	for _, e := range events {
		line("BEGIN:VEVENT")
		line("UID:" + escape(e.UID))
		stamp := e.Stamp
		if stamp.IsZero() {
			stamp = time.Now()
		}
		line("DTSTAMP:" + stamp.UTC().Format("20060102T150405Z"))
		if e.AllDay {
			line("DTSTART;VALUE=DATE:" + e.Start.UTC().Format("20060102"))
			line("DTEND;VALUE=DATE:" + e.End.UTC().Format("20060102"))
		} else {
			line("DTSTART:" + e.Start.UTC().Format("20060102T150405Z"))
			line("DTEND:" + e.End.UTC().Format("20060102T150405Z"))
		}
		line("SUMMARY:" + escape(e.Summary))
		if e.Description != "" {
			line("DESCRIPTION:" + escape(e.Description))
		}
		if e.Location != "" {
			line("LOCATION:" + escape(e.Location))
		}
		if e.URL != "" {
			line("URL:" + e.URL)
		}
		if e.Status != "" {
			line("STATUS:" + e.Status)
		}
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	return b.String()
}

func escape(s string) string {
	r := strings.NewReplacer("\\", "\\\\", ";", "\\;", ",", "\\,", "\r\n", "\\n", "\n", "\\n")
	return r.Replace(s)
}

// fold breaks a content line into 75-octet chunks continued by one space.
func fold(s string) string {
	if len(s) <= 75 {
		return s
	}
	var out strings.Builder
	first := true
	for len(s) > 0 {
		limit := 75
		if !first {
			limit = 74
		}
		if len(s) <= limit {
			if !first {
				out.WriteString("\r\n ")
			}
			out.WriteString(s)
			break
		}
		cut := limit
		for cut > 0 && !isBoundary(s, cut) {
			cut--
		}
		if cut == 0 {
			cut = limit
		}
		if !first {
			out.WriteString("\r\n ")
		}
		out.WriteString(s[:cut])
		s = s[cut:]
		first = false
	}
	return out.String()
}

// isBoundary reports whether s[i:] starts on a UTF-8 rune boundary.
func isBoundary(s string, i int) bool { return i >= len(s) || (s[i]&0xC0) != 0x80 }

// FeedFilename is the attachment name a client sees for a feed.
func FeedFilename(name string) string {
	slug := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return '-'
	}, name)
	return fmt.Sprintf("%s.ics", strings.Trim(slug, "-"))
}
