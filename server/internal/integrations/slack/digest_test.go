package slack

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/slack-go/slack"
)

// K64 debt: the digest renders as sections plus one actions block; links
// become URL buttons, callbacks carry their value; long text is chunked.
func TestDigestBlocks(t *testing.T) {
	text := strings.Repeat("line of the digest\n", 400) // ~7600 chars → 3 sections
	blocks := DigestBlocks(text, []channel.DigestAction{{Label: "Answer: Keep it", Value: "decide|i|d|keep"}, {Label: "Open the briefing", URL: "https://app/acme/inbox?view=briefing"}})
	sections, actions := 0, 0
	for _, b := range blocks {
		switch v := b.(type) {
		case *slack.SectionBlock:
			sections++
			if len(v.Text.Text) > 3000 {
				t.Fatalf("section too long: %d", len(v.Text.Text))
			}
		case *slack.ActionBlock:
			actions++
			btns := v.Elements.ElementSet
			if len(btns) != 2 {
				t.Fatalf("buttons = %d", len(btns))
			}
			first, ok := btns[0].(*slack.ButtonBlockElement)
			if !ok || first.ActionID != DigestActionID || first.Value != "decide|i|d|keep" || first.URL != "" {
				t.Fatalf("decide button = %+v", btns[0])
			}
			second := btns[1].(*slack.ButtonBlockElement)
			if second.URL != "https://app/acme/inbox?view=briefing" {
				t.Fatalf("link button = %+v", second)
			}
		}
	}
	if sections < 3 || actions != 1 {
		t.Fatalf("sections = %d actions = %d", sections, actions)
	}
	if got := chunkText("a\nb", 10); len(got) != 1 || got[0] != "a\nb" {
		t.Fatalf("short chunk = %q", got)
	}
}

// chunkText must never split a multi-byte rune, whether or not there is a
// newline near the cut point. CJK product copy is a first-class requirement
// (conventions.zh.mdx), and a mid-rune byte cut produces invalid UTF-8 that
// Slack rejects.
func TestChunkTextNeverSplitsARune(t *testing.T) {
	// No newline anywhere near the cut point: forces the `cut = limit`
	// fallback path that previously used a raw byte index.
	noNewline := strings.Repeat("中文混合内容ab测试😀", 30)
	for _, chunk := range chunkText(noNewline, 2900) {
		if !utf8.ValidString(chunk) {
			t.Fatalf("chunk is not valid UTF-8: %q", chunk)
		}
	}

	// A newline lands close to the byte budget, right after a multi-byte
	// rune sequence.
	withNewline := strings.Repeat("中", 966) + "\n" + strings.Repeat("文", 966)
	for _, chunk := range chunkText(withNewline, 2900) {
		if !utf8.ValidString(chunk) {
			t.Fatalf("chunk is not valid UTF-8: %q", chunk)
		}
	}
}
