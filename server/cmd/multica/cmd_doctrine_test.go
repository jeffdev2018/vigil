package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// The brief the daemon injects tells agents to run
// `multica doctrine report --kind ... --summary ... [--passage ...]`, and the
// platform skill documents the rest of the tree. A renamed or dropped flag
// would make both of those lie.
func TestDoctrineCommandsExposeDocumentedFlags(t *testing.T) {
	for _, tc := range []struct {
		cmd   *cobra.Command
		flags []string
	}{
		{doctrineShowCmd, []string{"output"}},
		{doctrinePublishCmd, []string{"file", "stdin", "expected-revision", "note", "output"}},
		{doctrineHistoryCmd, []string{"limit", "cursor", "output"}},
		{doctrineDiffCmd, []string{"against", "output"}},
		{doctrineApproveCmd, []string{"note", "output"}},
		{doctrineRejectCmd, []string{"note", "output"}},
		{doctrineRestoreCmd, []string{"expected-revision", "note", "output"}},
		{doctrineReportCmd, []string{"kind", "summary", "passage", "issue", "output"}},
		{doctrineReportsCmd, []string{"status", "output"}},
		{doctrineAcknowledgeCmd, []string{"note", "output"}},
		{doctrineDismissCmd, []string{"note", "output"}},
	} {
		for _, name := range tc.flags {
			if tc.cmd.Flags().Lookup(name) == nil {
				t.Errorf("doctrine %s is missing the documented --%s flag", tc.cmd.Name(), name)
			}
		}
	}
	for _, cmd := range []*cobra.Command{doctrineDiffCmd, doctrineApproveCmd, doctrineRejectCmd, doctrineRestoreCmd, doctrineAcknowledgeCmd, doctrineDismissCmd} {
		if cmd.Args == nil {
			t.Errorf("doctrine %s must require its id argument", cmd.Name())
		}
	}
}

// --expected-revision guards the whole publication flow: without it a stale
// write would be merged instead of refused, so a missing flag must fail
// before the request goes out. Revision 0 (a workspace with no doctrine yet)
// is a legitimate value.
func TestDoctrineExpectedRevisionIsRequired(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Int("expected-revision", -1, "")
	if _, err := doctrineExpectedRevision(cmd); err == nil {
		t.Fatal("an unset --expected-revision must be an error, not a silent last-writer-wins publish")
	}
	if err := cmd.Flags().Set("expected-revision", "0"); err != nil {
		t.Fatal(err)
	}
	got, err := doctrineExpectedRevision(cmd)
	if err != nil || got != 0 {
		t.Fatalf("doctrineExpectedRevision = %d, %v; want 0, nil", got, err)
	}
}

func TestReadDoctrineTextSources(t *testing.T) {
	path := filepath.Join(t.TempDir(), "DOCTRINE.md")
	if err := os.WriteFile(path, []byte("rule one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	newCmd := func() *cobra.Command {
		cmd := &cobra.Command{}
		cmd.Flags().String("file", "", "")
		cmd.Flags().Bool("stdin", false, "")
		return cmd
	}

	if _, err := readDoctrineText(newCmd()); err == nil {
		t.Fatal("neither --file nor --stdin must be an error")
	}

	cmd := newCmd()
	if err := cmd.Flags().Set("file", path); err != nil {
		t.Fatal(err)
	}
	got, err := readDoctrineText(cmd)
	if err != nil || got != "rule one\n" {
		t.Fatalf("readDoctrineText(--file) = %q, %v", got, err)
	}

	if err := cmd.Flags().Set("stdin", "true"); err != nil {
		t.Fatal(err)
	}
	if _, err := readDoctrineText(cmd); err == nil {
		t.Fatal("--file with --stdin must be refused rather than picking one")
	}
}

func TestDoctrineDiffLabel(t *testing.T) {
	if got := doctrineDiffLabel(nil); got != "(nothing)" {
		t.Fatalf("nil label = %q", got)
	}
	if got := doctrineDiffLabel(map[string]any{"revision": float64(4)}); got != "revision 4" {
		t.Fatalf("revision label = %q", got)
	}
	// A pending or rejected proposal has no revision number yet.
	if got := doctrineDiffLabel(map[string]any{"revision": nil, "status": "pending", "id": "abc"}); got != "pending abc" {
		t.Fatalf("pending label = %q", got)
	}
}
