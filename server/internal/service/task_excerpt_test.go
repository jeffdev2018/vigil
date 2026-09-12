package service

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The effect journal (K69) stores excerpt(content, 200) as JSON; cutting a
// multi-byte rune in half turned the last character into U+FFFD.
func TestExcerptNeverSplitsARune(t *testing.T) {
	content := strings.Repeat("a", 199) + "é suite"
	got := excerpt(content, 200)
	if !utf8.ValidString(got) || len(got) > 200 {
		t.Fatalf("excerpt = %q (%d bytes), want valid UTF-8 within 200 bytes", got, len(got))
	}
	if got != strings.Repeat("a", 199) {
		t.Fatalf("excerpt = %q, want the cut before the split rune", got)
	}
	if got := excerpt("修复登录页面的错误", 7); got != "修复" {
		t.Fatalf("excerpt of CJK = %q, want 修复", got)
	}
	if got := excerpt("short", 200); got != "short" {
		t.Fatalf("short content = %q", got)
	}
}
