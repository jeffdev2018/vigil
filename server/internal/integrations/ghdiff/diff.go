// Package ghdiff turns a raw unified diff into the bounded, structured form a
// pull request walkthrough run is briefed with (F05 / JEF-16).
//
// It does not fetch. Fetching already exists and is provider-shaped:
// ghsnapshot.Client.PullRequestDiff for GitHub App installations,
// vcs.Provider.PullRequestDiff for Forgejo / Gitea / GitLab. This package is
// the part neither of them has — parsing the text into files and hunks, and
// bounding it so one enormous pull request cannot be turned into an
// unboundedly large agent brief.
//
// Capping is deterministic and whole-file: files are kept in the order the
// diff lists them until a cap would be crossed, and the rest are dropped and
// counted. A walkthrough of the first N files is useful; a walkthrough of a
// diff cut mid-hunk is not, and a walkthrough that failed because the PR was
// large is worse than both.
package ghdiff

import (
	"strconv"
	"strings"
)

const (
	// MaxBytes and MaxFiles are the caps one walkthrough brief is built under.
	MaxBytes = 1 << 20
	MaxFiles = 400
)

// Hunk is one contiguous changed region of a file.
type Hunk struct {
	// OldStart / NewStart are the 1-based line numbers from the @@ header. They
	// are 0 when the header did not carry a usable number, which is how a
	// malformed hunk stays parseable instead of poisoning the file.
	OldStart int `json:"old_start"`
	NewStart int `json:"new_start"`
	// Lines is the hunk body verbatim, header included: it is what the agent
	// reads and what the UI renders.
	Lines string `json:"lines"`
}

// File is one file's worth of a diff.
type File struct {
	Path  string `json:"path"`
	Hunks []Hunk `json:"hunks"`
}

// Diff is a parsed, possibly capped unified diff.
type Diff struct {
	Files []File `json:"files"`
	// Truncated reports that Files is a prefix of the real change.
	Truncated bool `json:"truncated"`
	// OmittedFiles counts the files dropped by the cap.
	OmittedFiles int `json:"omitted_files"`
}

// Parse reads a unified diff into files and hunks. Anything it cannot
// recognise is skipped rather than rejected: this text comes from three
// different forges, and a walkthrough of the parts we understood beats an
// error on a header we did not.
func Parse(raw string) Diff {
	out := Diff{Files: []File{}}
	var current *File
	var hunk *strings.Builder
	var pending Hunk

	flushHunk := func() {
		if current == nil || hunk == nil {
			return
		}
		pending.Lines = strings.TrimRight(hunk.String(), "\n")
		current.Hunks = append(current.Hunks, pending)
		hunk = nil
		pending = Hunk{}
	}
	flushFile := func() {
		flushHunk()
		if current != nil && current.Path != "" {
			out.Files = append(out.Files, *current)
		}
		current = nil
	}

	for _, line := range strings.Split(raw, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flushFile()
			current = &File{Path: gitHeaderPath(line), Hunks: []Hunk{}}
		case strings.HasPrefix(line, "+++ ") && current != nil && current.Path == "":
			// Forges that emit a bare `--- / +++` pair with no `diff --git`.
			current.Path = stripPrefix(strings.TrimSpace(line[4:]))
		case strings.HasPrefix(line, "--- ") && current == nil:
			current = &File{Hunks: []Hunk{}}
		case strings.HasPrefix(line, "@@"):
			if current == nil {
				continue // a hunk with no file header belongs to nothing
			}
			flushHunk()
			pending.OldStart, pending.NewStart = hunkStarts(line)
			hunk = &strings.Builder{}
			hunk.WriteString(line + "\n")
		default:
			if hunk != nil {
				hunk.WriteString(line + "\n")
			}
		}
	}
	flushFile()
	return out
}

// gitHeaderPath reads the b-side path out of `diff --git a/x b/x`. It splits
// on " b/" rather than on spaces so a path containing a space survives.
func gitHeaderPath(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	if i := strings.LastIndex(rest, " b/"); i >= 0 {
		return strings.TrimSpace(rest[i+3:])
	}
	return stripPrefix(strings.TrimSpace(rest))
}

// stripPrefix drops the a/ or b/ prefix git puts on diff paths. /dev/null (a
// deletion's b-side) is preserved as-is so the caller can see it.
func stripPrefix(p string) string {
	p = strings.TrimSuffix(p, "\t")
	for _, prefix := range []string{"a/", "b/"} {
		if strings.HasPrefix(p, prefix) {
			return p[len(prefix):]
		}
	}
	return p
}

// hunkStarts reads the two 1-based line numbers out of `@@ -12,7 +12,9 @@`.
// A number we cannot read stays 0 rather than failing the hunk.
func hunkStarts(header string) (oldStart, newStart int) {
	fields := strings.Fields(header)
	for _, f := range fields {
		switch {
		case strings.HasPrefix(f, "-") && oldStart == 0:
			oldStart = leadingInt(f[1:])
		case strings.HasPrefix(f, "+") && newStart == 0:
			newStart = leadingInt(f[1:])
		}
	}
	return oldStart, newStart
}

func leadingInt(s string) int {
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// Cap bounds a parsed diff to maxFiles and maxBytes, keeping whole files in
// order. A single file that alone exceeds maxBytes is still kept when it is
// the first one — a walkthrough of one huge file is a walkthrough; an empty
// one is not.
func Cap(d Diff, maxFiles, maxBytes int) Diff {
	out := Diff{Files: []File{}, Truncated: d.Truncated, OmittedFiles: d.OmittedFiles}
	size := 0
	for i, f := range d.Files {
		cost := fileBytes(f)
		if i > 0 && (len(out.Files) >= maxFiles || size+cost > maxBytes) {
			out.Truncated = true
			out.OmittedFiles += len(d.Files) - i
			break
		}
		out.Files = append(out.Files, f)
		size += cost
	}
	return out
}

func fileBytes(f File) int {
	n := len(f.Path)
	for _, h := range f.Hunks {
		n += len(h.Lines)
	}
	return n
}

// ParseAndCap is the one call the walkthrough enqueue makes.
func ParseAndCap(raw string) Diff { return Cap(Parse(raw), MaxFiles, MaxBytes) }

// Brief renders a capped diff back to the text an agent run is briefed with.
// Re-rendering rather than passing the raw diff through is what makes the cap
// real: the run sees exactly the files the stored walkthrough will describe.
func Brief(d Diff) string {
	var b strings.Builder
	for _, f := range d.Files {
		b.WriteString("--- " + f.Path + "\n")
		for _, h := range f.Hunks {
			b.WriteString(h.Lines + "\n")
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}
