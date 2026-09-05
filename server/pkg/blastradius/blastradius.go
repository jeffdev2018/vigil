// Package blastradius resolves per-project autonomy rules by path pattern.
// One implementation for the server (approval gates) and the daemon, so a
// path never resolves differently on the two sides.
package blastradius

import (
	"errors"
	"regexp"
	"strings"
)

const (
	LevelAutonomous   = "autonomous"
	LevelReadOnly     = "read_only"
	LevelDualApproval = "dual_approval"
)

var Levels = []string{LevelAutonomous, LevelReadOnly, LevelDualApproval}

func ValidLevel(level string) bool {
	for _, l := range Levels {
		if l == level {
			return true
		}
	}
	return false
}

// Rule is (pattern, level); ID is carried through for callers.
type Rule struct {
	ID      string
	Pattern string
	Level   string
}

// Compile turns a .gitignore-like glob into a regexp: `**` spans
// directories, `*` and `?` stay inside one segment, a bare directory
// pattern covers everything under it.
func Compile(pattern string) (*regexp.Regexp, error) {
	pattern = strings.Trim(strings.TrimSpace(pattern), "/")
	if pattern == "" {
		return nil, errors.New("path_pattern is required")
	}
	if !strings.ContainsAny(pattern, "*?[") {
		return regexp.Compile("^" + regexp.QuoteMeta(pattern) + "(/.*)?$")
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch {
		case c == '*' && i+1 < len(pattern) && pattern[i+1] == '*':
			i++
			if i+1 < len(pattern) && pattern[i+1] == '/' {
				i++
				b.WriteString("(?:.*/)?")
			} else {
				b.WriteString(".*")
			}
		case c == '*':
			b.WriteString("[^/]*")
		case c == '?':
			b.WriteString("[^/]")
		case c == '[':
			return nil, errors.New("character classes are not supported in path_pattern")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}

// Specificity orders patterns like .gitignore does: the more literal
// characters, the more specific. Wildcards count for nothing.
func Specificity(pattern string) int {
	pattern = strings.Trim(strings.TrimSpace(pattern), "/")
	n := 0
	for _, c := range pattern {
		if c != '*' && c != '?' {
			n++
		}
	}
	return n
}

// Resolve returns the most specific rule matching path. With equal
// specificity the first rule wins, which Conflicts keeps from mattering.
func Resolve(rules []Rule, path string) (Rule, bool) {
	path = strings.TrimPrefix(strings.TrimSpace(path), "/")
	best, bestSpec, found := Rule{}, -1, false
	for _, r := range rules {
		re, err := Compile(r.Pattern)
		if err != nil || !re.MatchString(path) {
			continue
		}
		if spec := Specificity(r.Pattern); spec > bestSpec {
			best, bestSpec, found = r, spec, true
		}
	}
	return best, found
}

// skeleton is a sample path the pattern matches: wildcards replaced by a
// plain segment, so two patterns can be tested against each other.
func skeleton(pattern string) string {
	p := strings.Trim(strings.TrimSpace(pattern), "/")
	p = strings.ReplaceAll(p, "**/", "x/")
	p = strings.ReplaceAll(p, "**", "x")
	p = strings.ReplaceAll(p, "*", "x")
	p = strings.ReplaceAll(p, "?", "x")
	return p
}

// Conflicts finds an existing rule of the same specificity and a different
// level that shares a path with the candidate; such a pair would be
// decided by insertion order, which is refused instead.
func Conflicts(existing []Rule, candidate Rule) (Rule, bool) {
	cre, err := Compile(candidate.Pattern)
	if err != nil {
		return Rule{}, false
	}
	spec := Specificity(candidate.Pattern)
	for _, r := range existing {
		if r.Level == candidate.Level || Specificity(r.Pattern) != spec {
			continue
		}
		rre, err := Compile(r.Pattern)
		if err != nil {
			continue
		}
		if cre.MatchString(skeleton(r.Pattern)) || rre.MatchString(skeleton(candidate.Pattern)) {
			return r, true
		}
	}
	return Rule{}, false
}

// Worst folds the levels touched by a set of paths into the one that must
// govern the whole action: read_only beats dual_approval beats autonomous.
// ok is false when no path resolved to any rule.
func Worst(rules []Rule, paths []string) (level string, ok bool) {
	rank := map[string]int{LevelAutonomous: 1, LevelDualApproval: 2, LevelReadOnly: 3}
	best := 0
	for _, p := range paths {
		r, found := Resolve(rules, p)
		if !found {
			continue
		}
		if rank[r.Level] > best {
			best, level = rank[r.Level], r.Level
		}
	}
	return level, best > 0
}
