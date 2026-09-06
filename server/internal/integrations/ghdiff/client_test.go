package ghdiff

import (
	"strings"
	"testing"
)

// Parsing, capping and briefing. The fetch itself lives in ghsnapshot /
// vcs and is tested there — including that a throttled diff fetch surfaces as
// a retryable RateLimitError (ghsnapshot.TestPullRequestDiffReportsRateLimit),
// which is what lets the walkthrough leave a head unclaimed and retry.

const twoFileDiff = `diff --git a/api/client.go b/api/client.go
index 1111111..2222222 100644
--- a/api/client.go
+++ b/api/client.go
@@ -12,7 +12,9 @@ func Fetch() error {
 	ctx := context.Background()
-	return doIt(ctx)
+	if err := doIt(ctx); err != nil {
+		return err
+	}
+	return nil
 }
@@ -40,3 +42,4 @@ func Close() {
 	pool.Close()
+	log.Info("closed")
diff --git a/api/client_test.go b/api/client_test.go
--- a/api/client_test.go
+++ b/api/client_test.go
@@ -1,2 +1,3 @@
 package api
+// covered
`

func TestParseReadsFilesAndHunkStarts(t *testing.T) {
	d := Parse(twoFileDiff)
	if len(d.Files) != 2 {
		t.Fatalf("files = %d, want 2: %+v", len(d.Files), d.Files)
	}
	if d.Files[0].Path != "api/client.go" || d.Files[1].Path != "api/client_test.go" {
		t.Fatalf("paths = %q, %q", d.Files[0].Path, d.Files[1].Path)
	}
	if got := len(d.Files[0].Hunks); got != 2 {
		t.Fatalf("first file hunks = %d, want 2", got)
	}
	first := d.Files[0].Hunks[0]
	if first.OldStart != 12 || first.NewStart != 12 {
		t.Errorf("hunk 1 starts = (%d, %d), want (12, 12)", first.OldStart, first.NewStart)
	}
	second := d.Files[0].Hunks[1]
	if second.OldStart != 40 || second.NewStart != 42 {
		t.Errorf("hunk 2 starts = (%d, %d), want (40, 42)", second.OldStart, second.NewStart)
	}
	// The body is verbatim, header included: it is what the UI renders.
	if !strings.HasPrefix(first.Lines, "@@ -12,7 +12,9 @@") || !strings.Contains(first.Lines, "+		return err") {
		t.Errorf("hunk body lost content: %q", first.Lines)
	}
}

func TestParseToleratesMalformedInput(t *testing.T) {
	cases := map[string]string{
		"empty":             "",
		"prose":             "no diff here at all\njust words\n",
		"hunk with no file": "@@ -1,1 +1,1 @@\n-a\n+b\n",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if d := Parse(raw); len(d.Files) != 0 {
				t.Fatalf("files = %+v, want none", d.Files)
			}
		})
	}
	// An unreadable @@ header keeps the hunk with zeroed starts rather than
	// dropping the file: the explanation is still worth having.
	d := Parse("diff --git a/x.go b/x.go\n@@ what @@\n+line\n")
	if len(d.Files) != 1 || len(d.Files[0].Hunks) != 1 {
		t.Fatalf("malformed header dropped the hunk: %+v", d)
	}
	if h := d.Files[0].Hunks[0]; h.OldStart != 0 || h.NewStart != 0 {
		t.Errorf("starts = (%d, %d), want (0, 0)", h.OldStart, h.NewStart)
	}
}

func TestParseKeepsPathsWithSpaces(t *testing.T) {
	d := Parse("diff --git a/my dir/file.go b/my dir/file.go\n@@ -1 +1 @@\n+x\n")
	if len(d.Files) != 1 || d.Files[0].Path != "my dir/file.go" {
		t.Fatalf("path = %+v", d.Files)
	}
}

func TestCapDropsWholeFilesAndCounts(t *testing.T) {
	d := Parse(twoFileDiff)

	// File cap: keep the first, count the rest as omitted.
	capped := Cap(d, 1, MaxBytes)
	if len(capped.Files) != 1 || !capped.Truncated || capped.OmittedFiles != 1 {
		t.Fatalf("file cap: files=%d truncated=%v omitted=%d", len(capped.Files), capped.Truncated, capped.OmittedFiles)
	}
	if capped.Files[0].Path != "api/client.go" {
		t.Errorf("cap kept %q, want the first file in diff order", capped.Files[0].Path)
	}

	// Byte cap: a file is never cut in half.
	byteCapped := Cap(d, MaxFiles, len(twoFileDiff)/2)
	if !byteCapped.Truncated || byteCapped.OmittedFiles == 0 {
		t.Fatalf("byte cap did not truncate: %+v", byteCapped)
	}
	for _, f := range byteCapped.Files {
		if len(f.Hunks) == 0 {
			t.Errorf("file %q kept with no hunks — the cap cut inside a file", f.Path)
		}
	}

	// Under the caps nothing is touched.
	if plain := Cap(d, MaxFiles, MaxBytes); plain.Truncated || plain.OmittedFiles != 0 || len(plain.Files) != 2 {
		t.Fatalf("uncapped diff was modified: %+v", plain)
	}
}

func TestCapKeepsTheFirstFileEvenWhenItAloneExceedsTheCap(t *testing.T) {
	// A one-file PR bigger than the cap must still produce a walkthrough:
	// truncating to nothing is the failure mode this guards.
	huge := "diff --git a/big.go b/big.go\n@@ -1 +1 @@\n+" + strings.Repeat("x", 4096) + "\n"
	capped := Cap(Parse(huge), MaxFiles, 16)
	if len(capped.Files) != 1 {
		t.Fatalf("files = %d, want the single oversized file kept", len(capped.Files))
	}
}

func TestBriefRendersOnlyTheCappedFiles(t *testing.T) {
	brief := Brief(Cap(Parse(twoFileDiff), 1, MaxBytes))
	if !strings.Contains(brief, "api/client.go") {
		t.Fatalf("brief lost the kept file: %q", brief)
	}
	if strings.Contains(brief, "client_test.go") {
		t.Errorf("brief leaked a file the cap dropped: %q", brief)
	}
}
