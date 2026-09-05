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
