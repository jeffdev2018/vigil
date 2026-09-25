package util

import "unicode/utf8"

// TruncateUTF8Bytes returns the longest prefix of s that is at most maxBytes
// bytes long without splitting a multi-byte UTF-8 rune. Plain byte slicing
// (s[:n]) is a correctness bug the moment s can contain non-ASCII text — CJK
// product copy is a first-class requirement in this repo (see
// conventions.zh.mdx) — because it can land mid-rune and produce invalid
// UTF-8, which Postgres TEXT rejects (SQLSTATE 22021) and which corrupts
// anything else that assumes valid UTF-8 (model context, Slack messages,
// notification titles).
//
// s is returned unchanged when it already fits. maxBytes <= 0 returns "".
func TruncateUTF8Bytes(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	b := s[:maxBytes]
	// Back off while the tail is an incomplete/invalid trailing sequence —
	// which is exactly what a byte-boundary cut through a multi-byte rune
	// produces. DecodeLastRuneInString reports (RuneError, 1) for both a
	// genuinely invalid byte and an incomplete trailing multi-byte rune, so
	// this loop drops the partial rune entirely instead of emitting it
	// mangled.
	for len(b) > 0 {
		r, size := utf8.DecodeLastRuneInString(b)
		if r != utf8.RuneError || size != 1 {
			break
		}
		b = b[:len(b)-size]
	}
	return b
}

// TruncateUTF8Runes returns the longest prefix of s that has at most
// maxRunes runes. Use it where a budget is naturally expressed in
// characters (a display-facing limit) rather than bytes (a storage/wire
// limit, where TruncateUTF8Bytes is the right tool).
//
// s is returned unchanged when it already fits. maxRunes <= 0 returns "".
func TruncateUTF8Runes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}
