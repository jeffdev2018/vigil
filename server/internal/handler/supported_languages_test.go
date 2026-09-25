package handler

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// supportedLanguages must accept exactly the locales the clients offer:
// a locale the UI lists but the server refuses makes the language switch fail
// with a 400 (French was missing).
func TestSupportedLanguagesMatchClientLocales(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "packages", "core", "i18n", "types.ts"))
	if err != nil {
		t.Fatalf("read client locales: %v", err)
	}
	m := regexp.MustCompile(`SUPPORTED_LOCALES[^=]*=\s*\[([^\]]*)\]`).FindSubmatch(src)
	if m == nil {
		t.Fatal("SUPPORTED_LOCALES not found in packages/core/i18n/types.ts")
	}
	var client []string
	for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(m[1], -1) {
		client = append(client, string(q[1]))
	}
	var server []string
	for lang := range supportedLanguages {
		server = append(server, lang)
	}
	sort.Strings(client)
	sort.Strings(server)
	if len(client) != len(server) {
		t.Fatalf("server languages %v, client locales %v", server, client)
	}
	for i := range client {
		if client[i] != server[i] {
			t.Fatalf("server languages %v, client locales %v", server, client)
		}
	}
}
