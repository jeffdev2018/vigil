package engine

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// `/capture` is the chat-bot half of the Brain's "capture first, organize
// later" contract: a member drops a line or a link into the workspace's
// capture inbox from the conversation they are already in, and a person files
// it later from the Brain page. Nothing is turned into a note here — that is
// the whole point, and it is why the command is safe to hand to a group chat
// while `/issue` answers to triage.
//
// Matching follows the `/issue` rules exactly: case-sensitive, token-bounded,
// and only the first non-empty line can be a command. `/capture` and `/issue`
// are therefore mutually exclusive on the same message. The `/note` prefix
// used inside chat replies is unrelated and untouched.
const captureCommandPrefix = "/capture"

// captureCommandMaxRunes matches the server's own capture limit. A chat
// message longer than this is refused as usage rather than truncated: half a
// captured thought is worse than none.
const captureCommandMaxRunes = 20000

// CaptureCommand is one parsed /capture command. Exactly one of URL and
// Content may be empty, never both: a capture nobody can act on later is not
// worth a row.
type CaptureCommand struct {
	// Content is the captured text, empty when the member captured a bare
	// link.
	Content string
	// URL is the captured link, set only when the whole command body is one
	// http(s) URL. A link inside a sentence stays part of the text, where the
	// sentence explains why the link matters.
	URL string
}

// ParseCaptureCommand extracts a /capture command from a chat-message body.
// It returns (cmd, true) when the message qualifies and the caller should
// file a capture, and (cmd, false) otherwise — with cmd.Content and cmd.URL
// both empty for a bare `/capture`, which the Router answers with usage.
func ParseCaptureCommand(body string) (CaptureCommand, bool) {
	text, ok := parseLeadingCommand(body, captureCommandPrefix)
	if !ok {
		return CaptureCommand{}, false
	}
	text = strings.TrimSpace(text)
	if link, isLink := loneHTTPURL(text); isLink {
		return CaptureCommand{URL: link}, true
	}
	return CaptureCommand{Content: text}, true
}

// CaptureCommandIsEmpty reports a `/capture` with nothing after it. The
// Router answers those with usage instead of writing an empty row.
func (c CaptureCommand) IsEmpty() bool {
	return c.Content == "" && c.URL == ""
}

// TooLong reports a capture the server would refuse anyway.
func (c CaptureCommand) TooLong() bool {
	return utf8.RuneCountInString(c.Content) > captureCommandMaxRunes
}

// loneHTTPURL reports whether text is exactly one http(s) URL, so a shared
// link is captured AS a link (kind "link", the URL rendered as such) rather
// than as a line of prose that happens to be a URL.
func loneHTTPURL(text string) (string, bool) {
	if text == "" || strings.ContainsAny(text, " \t\n") {
		return "", false
	}
	u, err := url.Parse(text)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(text) > 2048 {
		return "", false
	}
	return text, true
}
