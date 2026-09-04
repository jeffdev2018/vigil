package slack

import (
	"strings"
	"testing"

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
