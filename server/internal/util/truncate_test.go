package util

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateUTF8Bytes(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		maxBytes int
		want     string
	}{
		{"fits untouched", "hello", 10, "hello"},
		{"ascii exact cut", "hello world", 5, "hello"},
		{"maxBytes <= 0", "hello", 0, ""},
		{"negative maxBytes", "hello", -1, ""},
		{"empty string", "", 5, ""},
		// 中 is 3 bytes (E4 B8 AD). A cut at byte 4 lands 1 byte into the
		// second rune; the naive s[:4] would emit a mangled trailing byte.
		{"CJK cut mid-rune backs off to the previous rune", "中文测试", 4, "中"},
		{"CJK cut exactly on a rune boundary keeps it", "中文测试", 6, "中文"},
		{"CJK cut on rune 0 boundary", "中文测试", 2, ""},
		// é is 2 bytes (C3 A9).
		{"accented text cut mid-rune", "café", 4, "caf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateUTF8Bytes(tt.in, tt.maxBytes)
			if got != tt.want {
				t.Fatalf("TruncateUTF8Bytes(%q, %d) = %q, want %q", tt.in, tt.maxBytes, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("TruncateUTF8Bytes(%q, %d) = %q is not valid UTF-8", tt.in, tt.maxBytes, got)
			}
			if len(got) > tt.maxBytes && tt.maxBytes > 0 {
				t.Fatalf("TruncateUTF8Bytes(%q, %d) = %q exceeds the byte budget", tt.in, tt.maxBytes, got)
			}
		})
	}
}

// A long CJK string cut at every possible byte budget must never produce
// invalid UTF-8 — the property the byte-slicing bug violated.
func TestTruncateUTF8BytesNeverProducesInvalidUTF8(t *testing.T) {
	s := strings.Repeat("中文混合ab测试😀", 20)
	for n := 0; n <= len(s)+1; n++ {
		got := TruncateUTF8Bytes(s, n)
		if !utf8.ValidString(got) {
			t.Fatalf("TruncateUTF8Bytes(s, %d) = %q is not valid UTF-8", n, got)
		}
		if len(got) > n {
			t.Fatalf("TruncateUTF8Bytes(s, %d) = %q (%d bytes) exceeds budget", n, got, len(got))
		}
	}
}

func TestTruncateUTF8Runes(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		maxRunes int
		want     string
	}{
		{"fits untouched", "hello", 10, "hello"},
		{"ascii exact cut", "hello world", 5, "hello"},
		{"maxRunes <= 0", "hello", 0, ""},
		{"CJK counted by rune not byte", "中文测试", 2, "中文"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateUTF8Runes(tt.in, tt.maxRunes)
			if got != tt.want {
				t.Fatalf("TruncateUTF8Runes(%q, %d) = %q, want %q", tt.in, tt.maxRunes, got, tt.want)
			}
		})
	}
}
