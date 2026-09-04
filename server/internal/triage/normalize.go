package triage

import (
	"regexp"
	"strings"
)

var titleWhitespace = regexp.MustCompile(`[[:space:]]+`)

// NormalizeTitle mirrors issueguard's SQL normalization —
// lower(btrim(regexp_replace(title,'[[:space:]]+',' ','g'))) — so "duplicate
// inside the queue" and "duplicate of an issue" mean the same thing.
func NormalizeTitle(title string) string {
	return strings.ToLower(strings.TrimSpace(titleWhitespace.ReplaceAllString(title, " ")))
}
