package secretscan

import (
	"strings"
	"testing"
)

func TestScrub(t *testing.T) {
	t.Parallel()
	out := string(JSON([]byte(`{"gateway":{"url":"https://x","token":"abc"},"servers":[{"apiKey":"k","plain":"ok","note":"Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0"}]}`)))
	for _, want := range []string{`"token":"***"`, `"apiKey":"***"`, `"note":"***"`, `"plain":"ok"`, `"url":"https://x"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("scrub: %s", out)
		}
	}
	if string(JSON([]byte("garbage"))) != "{}" || Text("key sk-abcdefghij1234 end") != "key *** end" || !Found("ghp_abcdefghijklmnop") || Found("plain text") {
		t.Fatal("garbage becomes an empty object; token shapes are masked and detected in text")
	}
}

// The walk is bounded like util.SanitizeJSONForPostgres: a subtree nested past
// the cap is masked, not walked, so a hostile payload (a remote MCP server's
// tool result) cannot dictate recursion depth — and a scrubber that stops
// looking must not pass what it did not look at.
func TestJSONMasksSubtreesPastTheDepthCap(t *testing.T) {
	deep := strings.Repeat(`{"a":`, maxDepth+5) + `"not-walked"` + strings.Repeat(`}`, maxDepth+5)
	out := string(JSON([]byte(deep)))
	if strings.Contains(out, "not-walked") {
		t.Fatalf("a value past the cap was kept unscanned: %s", out)
	}
	if !strings.Contains(out, `"`+Mask+`"`) {
		t.Fatalf("the subtree past the cap must be masked: %s", out)
	}
	shallow := `{"a":{"b":"keep"}}`
	if got := string(JSON([]byte(shallow))); got != shallow {
		t.Fatalf("shallow JSON = %s, want it untouched", got)
	}
	// The decoder's own nesting cap is far deeper; the walk must stay cheap.
	hostile := strings.Repeat(`[`, 9000) + strings.Repeat(`]`, 9000)
	if got := string(JSON([]byte(hostile))); !strings.Contains(got, Mask) {
		t.Fatalf("hostile nesting = %.80s…, want a masked subtree", got)
	}
}
