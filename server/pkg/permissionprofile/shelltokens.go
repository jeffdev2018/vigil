package permissionprofile

import "strings"

// Shell parsing for AllowedCommands, deliberately small and deliberately
// fail-closed. It answers two questions: where does one command end and the
// next begin, and what are this command's words. Anything it cannot answer
// with certainty it refuses, because an allowlist that admits what it did not
// understand admits everything.

// splitShellSegments cuts a command line on the operators that start a new
// command — && || ; | and a newline — without cutting inside quotes. It
// refuses a line holding a command substitution: `echo $(rm -rf /)` would
// otherwise read as `echo`, and the substitution runs whatever it likes.
func splitShellSegments(command string) ([]string, bool) {
	var segments []string
	var current strings.Builder
	var quote byte
	flush := func() {
		if s := strings.TrimSpace(current.String()); s != "" {
			segments = append(segments, s)
		}
		current.Reset()
	}
	for i := 0; i < len(command); i++ {
		c := command[i]
		if quote != 0 {
			// Inside single quotes nothing is special, not even a backslash.
			if quote == '\'' {
				if c == '\'' {
					quote = 0
				}
				current.WriteByte(c)
				continue
			}
			if c == '\\' && i+1 < len(command) {
				current.WriteByte(c)
				i++
				current.WriteByte(command[i])
				continue
			}
			if c == '"' {
				quote = 0
			}
			// A substitution inside double quotes still runs.
			if c == '$' && i+1 < len(command) && command[i+1] == '(' {
				return nil, false
			}
			// Process substitution `<(cmd)` / `>(cmd)` runs cmd as well.
			if (c == '<' || c == '>') && i+1 < len(command) && command[i+1] == '(' {
				return nil, false
			}
			if c == '`' {
				return nil, false
			}
			current.WriteByte(c)
			continue
		}
		switch {
		case c == '\\' && i+1 < len(command):
			current.WriteByte(c)
			i++
			current.WriteByte(command[i])
		case c == '\'' || c == '"':
			quote = c
			current.WriteByte(c)
		case c == '`':
			return nil, false
		case c == '$' && i+1 < len(command) && command[i+1] == '(':
			return nil, false
		case (c == '<' || c == '>') && i+1 < len(command) && command[i+1] == '(':
			// Process substitution: `git log <(sh -c ...)` runs the inner
			// command whatever the allowlist says about `git log`.
			return nil, false
		case c == '&' || c == '|':
			// && and || start a command; a single & backgrounds one; a single
			// | pipes into one. All of them end this segment.
			if i+1 < len(command) && command[i+1] == c {
				i++
			}
			flush()
		case c == ';' || c == '\n':
			flush()
		default:
			current.WriteByte(c)
		}
	}
	if quote != 0 {
		return nil, false
	}
	flush()
	return segments, true
}

// shellTokens splits one segment into words, removing the quotes that grouped
// them. An unterminated quote is refused rather than guessed at.
func shellTokens(segment string) ([]string, bool) {
	var tokens []string
	var current strings.Builder
	var quote byte
	started := false
	flush := func() {
		if started {
			tokens = append(tokens, current.String())
			current.Reset()
			started = false
		}
	}
	for i := 0; i < len(segment); i++ {
		c := segment[i]
		if quote != 0 {
			if quote == '\'' {
				if c == '\'' {
					quote = 0
					continue
				}
				current.WriteByte(c)
				continue
			}
			if c == '\\' && i+1 < len(segment) {
				i++
				current.WriteByte(segment[i])
				continue
			}
			if c == '"' {
				quote = 0
				continue
			}
			current.WriteByte(c)
			continue
		}
		switch {
		case c == '\\' && i+1 < len(segment):
			i++
			current.WriteByte(segment[i])
			started = true
		case c == '\'' || c == '"':
			quote = c
			started = true
		case c == ' ' || c == '\t':
			flush()
		default:
			current.WriteByte(c)
			started = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	flush()
	return tokens, true
}
