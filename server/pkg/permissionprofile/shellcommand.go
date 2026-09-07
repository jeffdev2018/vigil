package permissionprofile

import "strings"

// Shell command matching for AllowedCommands.
//
// This is the decision a PreToolUse hook makes. It matters that it runs there
// and not against a provider's own permission rules: those match a prefix of
// the command the model proposed, and a prefix loses to chaining and to
// wrapping. A hook receives the final command string, so `cd /tmp && rm x`,
// `sh -c 'rm x'` and `sudo rm x` all arrive intact and are all decidable.
//
// The rule is fail-closed at every step. A command that cannot be parsed, a
// segment whose head cannot be read, a shell invocation whose script is not a
// single literal — each is refused rather than passed. An allowlist that
// admits what it failed to understand is not an allowlist.

// AllowsCommand reports whether every segment of a shell command is permitted
// by the profile, and names the first segment that is not. An empty allowlist,
// or one holding "*", permits everything: that is the default profile and it
// must stay free.
func (p Profile) AllowsCommand(command string) (ok bool, refused string) {
	if p.AllowsAnyCommand() {
		return true, ""
	}
	segments, parseOK := splitShellSegments(command)
	if !parseOK {
		return false, strings.TrimSpace(command)
	}
	if len(segments) == 0 {
		return true, ""
	}
	for _, segment := range segments {
		if allowed, why := p.allowsSegment(segment, 0); !allowed {
			return false, why
		}
	}
	return true, ""
}

// shellRecursionLimit bounds `sh -c` inside `sh -c`. Anything deeper is
// refused rather than followed: a command that needs four shells to express
// itself is not one an allowlist was written for.
const shellRecursionLimit = 3

func (p Profile) allowsSegment(segment string, depth int) (bool, string) {
	tokens, parseOK := shellTokens(segment)
	if !parseOK {
		return false, segment
	}
	tokens = stripCommandEnvelope(tokens)
	if len(tokens) == 0 {
		return false, segment
	}
	// A shell invoked with a script is the script's decision, not the shell's:
	// allowing `sh` would allow everything.
	if script, isShell := shellScriptArgument(tokens); isShell {
		if depth >= shellRecursionLimit || script == "" {
			return false, segment
		}
		nested, parseOK := splitShellSegments(script)
		if !parseOK || len(nested) == 0 {
			return false, segment
		}
		for _, inner := range nested {
			if allowed, why := p.allowsSegment(inner, depth+1); !allowed {
				return false, why
			}
		}
		return true, ""
	}
	for _, pattern := range p.AllowedCommands {
		if matchCommandPattern(strings.Fields(strings.TrimSpace(pattern)), tokens) {
			return true, ""
		}
	}
	return false, strings.Join(tokens, " ")
}

// matchCommandPattern matches a whitespace-split pattern against a command's
// tokens. A literal token must match exactly; "*" matches the rest of the
// command, including nothing. A pattern with no "*" matches its tokens as a
// prefix, so "git status" admits "git status --short": the operator named a
// command, not an exact invocation.
func matchCommandPattern(pattern, tokens []string) bool {
	for i, want := range pattern {
		if want == "*" {
			return true
		}
		if i >= len(tokens) || tokens[i] != want {
			return false
		}
	}
	return true
}

// envelopePrefixes are the wrappers a command can hide behind while still
// being the same command. VAR=value assignments are stripped the same way.
var envelopePrefixes = map[string]bool{
	"sudo": true, "doas": true, "nohup": true, "command": true,
	"time": true, "exec": true, "nice": true, "stdbuf": true, "setsid": true,
}

// stripCommandEnvelope removes leading wrappers and environment assignments so
// `sudo rm x` and `FOO=1 rm x` are read as `rm x`. `env` is followed only past
// its assignments; `env -i` and friends change what runs, so the flag form is
// left in place and will not match any pattern.
func stripCommandEnvelope(tokens []string) []string {
	for len(tokens) > 0 {
		head := tokens[0]
		switch {
		case envelopePrefixes[head]:
			tokens = tokens[1:]
		case head == "env":
			rest := tokens[1:]
			for len(rest) > 0 && strings.Contains(rest[0], "=") && !strings.HasPrefix(rest[0], "-") {
				rest = rest[1:]
			}
			if len(rest) == len(tokens[1:]) && len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
				return tokens // `env -i …`: not a transparent wrapper
			}
			tokens = rest
		case strings.Contains(head, "=") && !strings.HasPrefix(head, "-") && !strings.Contains(head, "/"):
			tokens = tokens[1:]
		default:
			return tokens
		}
	}
	return tokens
}

var shellNames = map[string]bool{"sh": true, "bash": true, "zsh": true, "dash": true, "ksh": true, "busybox": true}

// shellScriptArgument returns the script a shell was asked to run. The second
// value reports that the head IS a shell, so a caller can refuse a shell whose
// script it cannot read rather than treat it as an ordinary command.
func shellScriptArgument(tokens []string) (string, bool) {
	head := tokens[0]
	if slash := strings.LastIndex(head, "/"); slash >= 0 {
		head = head[slash+1:]
	}
	if !shellNames[head] {
		return "", false
	}
	for i := 1; i < len(tokens); i++ {
		// -c, and the combined forms a shell accepts (-lc, -ec, …).
		if strings.HasPrefix(tokens[i], "-") && strings.Contains(tokens[i], "c") && i+1 < len(tokens) {
			return tokens[i+1], true
		}
	}
	return "", true
}
