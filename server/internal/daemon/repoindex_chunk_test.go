package daemon

import (
	"strings"
	"testing"
)

// Canonical layer for the chunking rules (K47). The walker and the HTTP calls
// are tested in repoindex_test.go; everything about WHERE a chunk starts and
// what it is named belongs here.

func chunkSymbols(chunks []RepoIndexChunk) []string {
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = c.Symbol
	}
	return out
}

func TestRepoIndexChunkGoFuncBoundaries(t *testing.T) {
	src := strings.Join([]string{
		"package daemon",
		"",
		"import \"fmt\"",
		"",
		"func Alpha() {",
		"\tfmt.Println(\"a\")",
		"}",
		"",
		"func (d *Daemon) Beta(x int) error {",
		"\treturn nil",
		"}",
		"",
		"type Gamma struct {",
		"\tField int",
		"}",
	}, "\n")

	chunks := ChunkSource(src)
	if got, want := chunkSymbols(chunks), []string{"", "Alpha", "Beta", "Gamma"}; len(got) != len(want) {
		t.Fatalf("symbols = %v, want %v", got, want)
	} else {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("symbols = %v, want %v", got, want)
			}
		}
	}

	// The receiver must not be mistaken for the method name: `func (d *Daemon)
	// Beta` names Beta, and a hint saying "Daemon" would point at the wrong
	// thing in every file with methods.
	beta := chunks[2]
	if beta.Symbol != "Beta" {
		t.Fatalf("method symbol = %q, want Beta (receiver must not win)", beta.Symbol)
	}
	if !strings.Contains(beta.Content, "return nil") {
		t.Errorf("method chunk lost its body: %q", beta.Content)
	}

	// Lines are 1-based and inclusive, so a hint can be opened directly.
	if chunks[0].StartLine != 1 {
		t.Errorf("preamble starts at line %d, want 1", chunks[0].StartLine)
	}
	if beta.StartLine != 9 {
		t.Errorf("Beta starts at line %d, want 9", beta.StartLine)
	}
	if last := chunks[len(chunks)-1]; last.EndLine != strings.Count(src, "\n")+1 {
		t.Errorf("last chunk ends at %d, want %d", last.EndLine, strings.Count(src, "\n")+1)
	}
}

func TestRepoIndexChunkIndentedDeclarationIsNotABoundary(t *testing.T) {
	// A nested function is part of its parent, not a top-level symbol. Column 0
	// is the entire heuristic, so this is the case that proves it holds.
	src := strings.Join([]string{
		"func Outer() {",
		"\tfunc inner() {}",
		"\tclass NotATopLevelClass {}",
		"}",
	}, "\n")
	chunks := ChunkSource(src)
	if len(chunks) != 1 || chunks[0].Symbol != "Outer" {
		t.Fatalf("indented declarations opened chunks: %v", chunkSymbols(chunks))
	}
}

func TestRepoIndexChunkTypeScriptForms(t *testing.T) {
	src := strings.Join([]string{
		"import { x } from \"y\";",
		"",
		"export function handler() {}",
		"",
		"export const Widget = () => null;",
		"",
		"export default class Page {}",
		"",
		"interface Props {}",
		"",
		"const notExported = 1;",
	}, "\n")
	got := chunkSymbols(ChunkSource(src))
	want := []string{"", "handler", "Widget", "Page", "Props"}
	if len(got) != len(want) {
		t.Fatalf("symbols = %v, want %v (bare `const` must not open a chunk)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("symbols = %v, want %v", got, want)
		}
	}
}

func TestRepoIndexChunkPythonForms(t *testing.T) {
	src := strings.Join([]string{
		"import os",
		"",
		"def load(path):",
		"    return open(path)",
		"",
		"class Store:",
		"    def get(self, k):",
		"        return None",
	}, "\n")
	got := chunkSymbols(ChunkSource(src))
	// `def get` is indented, so it belongs to Store rather than opening a chunk.
	want := []string{"", "load", "Store"}
	if len(got) != len(want) {
		t.Fatalf("symbols = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("symbols = %v, want %v", got, want)
		}
	}
}

func TestRepoIndexChunkFallbackWindow(t *testing.T) {
	// No declaration anywhere: a config or a Markdown file still has to produce
	// chunks, or a query about it can never match.
	var b strings.Builder
	for i := range 300 {
		b.WriteString("line ")
		b.WriteString(strings.Repeat("x", 3))
		b.WriteString(string(rune('0' + i%10)))
		b.WriteString("\n")
	}
	chunks := ChunkSource(b.String())
	if len(chunks) != 3 {
		t.Fatalf("300 lines produced %d chunks, want 3 windows of %d", len(chunks), repoIndexWindowLines)
	}
	if chunks[0].StartLine != 1 || chunks[0].EndLine != 120 {
		t.Errorf("first window = %d..%d, want 1..120", chunks[0].StartLine, chunks[0].EndLine)
	}
	if chunks[1].StartLine != 121 || chunks[1].EndLine != 240 {
		t.Errorf("second window = %d..%d, want 121..240", chunks[1].StartLine, chunks[1].EndLine)
	}
	for _, c := range chunks {
		if c.Symbol != "" {
			t.Errorf("fallback window carries symbol %q, want empty", c.Symbol)
		}
	}
}

func TestRepoIndexChunkLongDeclarationIsWindowedAndKeepsItsSymbol(t *testing.T) {
	var b strings.Builder
	b.WriteString("func Huge() {\n")
	for range 250 {
		b.WriteString("\tstep()\n")
	}
	b.WriteString("}\n")
	chunks := ChunkSource(b.String())
	if len(chunks) < 3 {
		t.Fatalf("a 252-line function produced %d chunks, want it windowed", len(chunks))
	}
	for i, c := range chunks {
		if c.Symbol != "Huge" {
			t.Fatalf("window %d carries symbol %q, want Huge on every window of the same declaration", i, c.Symbol)
		}
		if c.EndLine-c.StartLine+1 > repoIndexWindowLines {
			t.Fatalf("window %d spans %d lines, over the %d ceiling", i, c.EndLine-c.StartLine+1, repoIndexWindowLines)
		}
	}
}

func TestRepoIndexChunkEmptyAndBlank(t *testing.T) {
	for _, src := range []string{"", "   ", "\n\n\n"} {
		if chunks := ChunkSource(src); len(chunks) != 0 {
			t.Errorf("ChunkSource(%q) = %d chunks, want none", src, len(chunks))
		}
	}
}

func TestRepoIndexIndexableFileRejectsBinaryAndOversize(t *testing.T) {
	cases := []struct {
		name    string
		content []byte
		want    bool
	}{
		{"plain source", []byte("package main\n"), true},
		{"utf8 source", []byte("// café ☕\nfunc a() {}\n"), true},
		{"nul byte", []byte("PNG\x00\x01\x02binary"), false},
		{"invalid utf8", []byte{0x66, 0x6f, 0x6f, 0xff, 0xfe, 0x0a}, false},
		{"empty", []byte{}, false},
		{"oversize", make([]byte, repoIndexMaxFileBytes+1), false},
	}
	for _, tc := range cases {
		if got := RepoIndexIndexableFile(tc.content); got != tc.want {
			t.Errorf("%s: RepoIndexIndexableFile = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRepoIndexSkipPath(t *testing.T) {
	cases := map[string]bool{
		"server/internal/daemon/daemon.go":   false,
		"node_modules/react/index.js":        true,
		"web/node_modules/react/index.js":    true,
		"vendor/github.com/x/y.go":           true,
		"apps/web/dist/bundle.js":            true,
		".git/config":                        true,
		"src/vendors.ts":                     false,
		"packages/core/distributed/index.ts": false,
	}
	for path, want := range cases {
		if got := RepoIndexSkipPath(path); got != want {
			t.Errorf("RepoIndexSkipPath(%q) = %v, want %v", path, got, want)
		}
	}
}
