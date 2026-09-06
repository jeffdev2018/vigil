package daemon

import (
	"path"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Chunking for the shared semantic repo index (K47).
//
// This file is pure: bytes in, chunks out, no git and no network. The walker
// and the server calls live in repoindex.go.

const (
	// repoIndexMaxFileBytes skips files too large to be worth indexing. A 200 KB
	// source file is a generated bundle, a lockfile or a data blob; chunking it
	// buys a run nothing and costs the index thousands of rows.
	repoIndexMaxFileBytes = 200 * 1024

	// repoIndexWindowLines is the fallback chunk height, and also the ceiling a
	// symbol chunk is split at. 120 lines is roughly a screen and a half: large
	// enough that a normal function lands in one chunk, small enough that a hint
	// points at a place rather than at a file.
	repoIndexWindowLines = 120

	// repoIndexBinarySniffBytes is how much of a file is examined for the NUL
	// bytes and invalid UTF-8 that mark it binary. Text files are text from the
	// first byte, so sniffing the whole of a large binary would be wasted I/O.
	repoIndexBinarySniffBytes = 8192
)

// repoIndexSkipDirs are directory names never indexed, at any depth. These hold
// code the repository did not write (dependencies, vendored trees) or code it
// generated (build output). Indexing them would bury a repo's own symbols under
// its dependencies' — the ranking failure is worse than the storage cost.
var repoIndexSkipDirs = map[string]struct{}{
	".git":         {},
	"node_modules": {},
	"vendor":       {},
	"dist":         {},
}

// repoIndexSymbolBoundary matches a top-level declaration. Column 0 is the whole
// heuristic: an indented `func` is a closure or a method body, a `func` at the
// left margin is a thing the repository named. That single rule is what lets one
// expression serve Go, TypeScript, Python, Rust, Java and C# without a
// per-language parser — the languages disagree about syntax, not about
// indenting their nested declarations.
//
// The optional parenthesised group before the name absorbs a Go method receiver
// (`func (d *Daemon) runTask`), so the captured symbol is the method name rather
// than the receiver.
var repoIndexSymbolBoundary = regexp.MustCompile(
	`^(?:export\s+(?:default\s+)?)?` +
		`(?:pub\s+|public\s+|private\s+|protected\s+|internal\s+|abstract\s+|static\s+|final\s+)*` +
		`(?:async\s+)?` +
		`(?:func|function|def|class|interface|struct|enum|impl|trait|fn|type)\s+` +
		`(?:\([^)]*\)\s*)?` +
		`([A-Za-z_$][A-Za-z0-9_$]*)`)

// repoIndexExportedConstBoundary catches the one assignment form worth treating
// as a declaration: `export const Name =` / `export const Name:`, which is how
// most of the TypeScript world writes a component or a hook. Bare `const` is
// deliberately excluded — every top-level constant would otherwise open a chunk.
var repoIndexExportedConstBoundary = regexp.MustCompile(
	`^export\s+(?:default\s+)?const\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*[:=]`)

// RepoIndexChunk is one unit of a file, as produced here and sent to the server.
// Lines are 1-based and inclusive at both ends, matching what an editor shows.
type RepoIndexChunk struct {
	Symbol    string
	StartLine int
	EndLine   int
	Content   string
}

// RepoIndexSkipPath reports whether a repository-relative path must not be
// indexed at all, because a segment of it names a dependency or build tree.
func RepoIndexSkipPath(relPath string) bool {
	for _, segment := range strings.Split(path.Clean(relPath), "/") {
		if _, skip := repoIndexSkipDirs[segment]; skip {
			return true
		}
	}
	return false
}

// RepoIndexIndexableFile reports whether a file's bytes are worth chunking:
// small enough, and text rather than binary.
//
// "Binary" is a NUL byte or invalid UTF-8 in the sniffed prefix. Both matter:
// a NUL catches images and compiled objects, and invalid UTF-8 catches
// everything else that would otherwise be stored as a corrupt string and
// embedded as noise.
func RepoIndexIndexableFile(content []byte) bool {
	if len(content) == 0 || len(content) > repoIndexMaxFileBytes {
		return false
	}
	head := content
	if len(head) > repoIndexBinarySniffBytes {
		head = head[:repoIndexBinarySniffBytes]
	}
	for _, b := range head {
		if b == 0 {
			return false
		}
	}
	// Trailing bytes of the sniff window can be a legitimately split rune, so
	// only reject invalid UTF-8 when the whole file was examined.
	if len(head) == len(content) && !utf8.Valid(head) {
		return false
	}
	return true
}

// ChunkSource splits a source file into indexable chunks.
//
// Boundaries are top-level declarations; the text before the first one (imports,
// package/module header) becomes its own chunk so a query about an import still
// lands somewhere. A file with no recognised declaration — Markdown, YAML, a
// config — falls back to fixed windows, which is why the function never returns
// zero chunks for a non-empty file.
//
// A declaration longer than repoIndexWindowLines is itself windowed, and every
// window keeps the enclosing symbol: a hint pointing at line 400 of a 600-line
// function should still say which function that is.
func ChunkSource(content string) []RepoIndexChunk {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	lines := strings.Split(content, "\n")

	type boundary struct {
		line   int // 0-based index into lines
		symbol string
	}
	var boundaries []boundary
	for i, line := range lines {
		if symbol, ok := repoIndexSymbolAt(line); ok {
			boundaries = append(boundaries, boundary{line: i, symbol: symbol})
		}
	}

	var out []RepoIndexChunk
	appendRange := func(symbol string, start, end int) {
		// Window anything taller than the ceiling, including the preamble.
		for from := start; from <= end; from += repoIndexWindowLines {
			to := from + repoIndexWindowLines - 1
			if to > end {
				to = end
			}
			body := strings.Join(lines[from:to+1], "\n")
			if strings.TrimSpace(body) == "" {
				continue
			}
			out = append(out, RepoIndexChunk{
				Symbol:    symbol,
				StartLine: from + 1,
				EndLine:   to + 1,
				Content:   body,
			})
		}
	}

	if len(boundaries) == 0 {
		appendRange("", 0, len(lines)-1)
		return out
	}
	if boundaries[0].line > 0 {
		appendRange("", 0, boundaries[0].line-1)
	}
	for i, b := range boundaries {
		end := len(lines) - 1
		if i+1 < len(boundaries) {
			end = boundaries[i+1].line - 1
		}
		appendRange(b.symbol, b.line, end)
	}
	return out
}

// repoIndexSymbolAt returns the declared symbol on a line, when the line opens a
// top-level declaration. A line starting with whitespace is never a boundary.
func repoIndexSymbolAt(line string) (string, bool) {
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return "", false
	}
	if m := repoIndexExportedConstBoundary.FindStringSubmatch(line); m != nil {
		return m[1], true
	}
	if m := repoIndexSymbolBoundary.FindStringSubmatch(line); m != nil {
		return m[1], true
	}
	return "", false
}
