package service

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The embedding text budget is a byte cap on free-text note title/content —
// CJK content (conventions.zh.mdx) whose cut lands mid-rune must not come
// back as invalid UTF-8.
func TestNoteEmbeddingTextIsUTF8Safe(t *testing.T) {
	title := strings.Repeat("标题", 10)
	content := strings.Repeat("中文混合内容ab测试😀", 2000)
	got := noteEmbeddingText(title, content)
	if !utf8.ValidString(got) {
		t.Fatalf("noteEmbeddingText produced invalid UTF-8: %q", got)
	}
	if len(got) > noteEmbeddingTextCap {
		t.Fatalf("noteEmbeddingText = %d bytes, want <=%d", len(got), noteEmbeddingTextCap)
	}
}
