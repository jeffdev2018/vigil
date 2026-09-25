package engine

// `/schedule` is the chat-bot half of "an autopilot from a sentence" (réveil
// programmé): a member says "every Monday at 9, list the open tickets" in the
// conversation they are already in, and the workspace's model turns it into a
// scheduled automation — title, cron, timezone, and the instruction each run
// follows.
//
// Nothing starts. The autopilot is filed PAUSED with its schedule disabled;
// the reply says what was understood and points at the Autopilots page, where
// a person activates it. That is the whole safety story of handing this
// command to a group chat: the worst a stranger's sentence can do is create a
// row somebody has to say yes to.
//
// Matching follows the `/issue` and `/capture` rules exactly: case-sensitive,
// token-bounded, and only the first non-empty line can be a command. The three
// are therefore mutually exclusive on the same message.

import (
	"errors"
	"strings"
	"unicode/utf8"
)

const scheduleCommandPrefix = "/schedule"

// scheduleCommandMaxRunes matches the server's own draft limit. A longer
// sentence is refused as usage rather than truncated: half a schedule read by
// a model is worse than none.
const scheduleCommandMaxRunes = 2000

// ErrAutopilotModelUnavailable is the proposer's answer when the workspace has
// no model to draft with. It is a configuration fact, not a failure, so the
// Router turns it into a reply instead of an error the platform would retry.
var ErrAutopilotModelUnavailable = errors.New("no model is configured to draft an autopilot")

// ErrAutopilotNotUnderstood is the proposer's answer when the sentence did not
// produce a usable schedule (empty, too long, or a cron the parser rejects).
// Also a reply, for the same reason: retrying the same words costs another
// model call and produces the same answer.
var ErrAutopilotNotUnderstood = errors.New("that did not read as a schedule")

// ScheduleCommand is one parsed /schedule command.
type ScheduleCommand struct {
	// Text is the sentence the model reads: the schedule and the task.
	Text string
}

// ParseScheduleCommand extracts a /schedule command from a chat-message body.
// It returns (cmd, true) when the message qualifies and the caller should
// propose an autopilot, and (cmd, false) otherwise — with cmd.Text empty for a
// bare `/schedule`, which the Router answers with usage.
func ParseScheduleCommand(body string) (ScheduleCommand, bool) {
	text, ok := parseLeadingCommand(body, scheduleCommandPrefix)
	if !ok {
		return ScheduleCommand{}, false
	}
	return ScheduleCommand{Text: strings.TrimSpace(text)}, true
}

// IsEmpty reports a `/schedule` with nothing after it.
func (c ScheduleCommand) IsEmpty() bool { return c.Text == "" }

// TooLong reports a sentence the server would refuse anyway.
func (c ScheduleCommand) TooLong() bool {
	return utf8.RuneCountInString(c.Text) > scheduleCommandMaxRunes
}
