package main

import (
	"testing"

	"github.com/spf13/cobra"
)

// The packs documentation (apps/docs/content/docs/packs.mdx) and the platform
// skill's references/packs.md both print these commands with these flags. A
// renamed or dropped flag would make both of them lie.
func TestPackCommandsExposeDocumentedFlags(t *testing.T) {
	for _, tc := range []struct {
		cmd   *cobra.Command
		flags []string
	}{
		{packListCmd, []string{"domain", "output"}},
		{packShowCmd, []string{"output"}},
		{packPreviewCmd, []string{"strategy", "output"}},
		{packInstallCmd, []string{"strategy", "force", "output"}},
		{packInstalledCmd, []string{"full-id", "output"}},
		{packItemsCmd, []string{"full-id", "output"}},
		{packUninstallCmd, []string{"output"}},
		{packDownloadCmd, []string{"out"}},
		{packPreviewFileCmd, []string{"strategy", "output"}},
		{packInstallFileCmd, []string{"strategy", "force", "output"}},
		{packExportCmd, []string{"id", "version", "title", "summary", "description", "domain", "wave",
			"metric-label", "metric-description", "metric-hint", "include-issues", "include-notes", "out"}},
		{workspaceExportCmd, []string{"include-issues", "include-notes", "template", "name", "out"}},
		{workspaceImportCmd, []string{"strategy", "preview", "output"}},
		{autopilotImportCmd, []string{"strategy", "preview", "output"}},
	} {
		for _, name := range tc.flags {
			if tc.cmd.Flags().Lookup(name) == nil {
				t.Errorf("%s is missing the documented --%s flag", tc.cmd.Name(), name)
			}
		}
	}
	for _, cmd := range []*cobra.Command{packShowCmd, packPreviewCmd, packInstallCmd, packItemsCmd,
		packUninstallCmd, packDownloadCmd, packPreviewFileCmd, packInstallFileCmd, workspaceImportCmd, autopilotImportCmd} {
		if cmd.Args == nil {
			t.Errorf("pack %s must require its argument", cmd.Name())
		}
	}
}

// A strategy is validated before the request goes out: sent as-is, an unknown
// value comes back as a 400 that reads like the pack was rejected.
func TestStrategyFlagsRejectUnknownValues(t *testing.T) {
	newCmd := func(value string) *cobra.Command {
		cmd := &cobra.Command{}
		cmd.Flags().String("strategy", "", "")
		if value != "" {
			if err := cmd.Flags().Set("strategy", value); err != nil {
				t.Fatal(err)
			}
		}
		return cmd
	}
	for _, value := range []string{"", "rename", "merge", "skip"} {
		if got, err := packStrategy(newCmd(value)); err != nil || got != value {
			t.Errorf("packStrategy(%q) = %q, %v; want it accepted", value, got, err)
		}
		if got, err := transferStrategy(newCmd(value)); err != nil || got != value {
			t.Errorf("transferStrategy(%q) = %q, %v; want it accepted", value, got, err)
		}
	}
	for _, value := range []string{"overwrite", "Merge", "force"} {
		if _, err := packStrategy(newCmd(value)); err == nil {
			t.Errorf("packStrategy(%q) must be refused locally", value)
		}
	}
	// The daemon import has its own vocabulary; mixing the two would send a
	// bundle strategy to an endpoint that does not know it.
	for _, value := range []string{"", "fail", "overwrite", "rename"} {
		if got, err := daemonImportStrategy(newCmd(value)); err != nil || got != value {
			t.Errorf("daemonImportStrategy(%q) = %q, %v; want it accepted", value, got, err)
		}
	}
	for _, value := range []string{"merge", "skip"} {
		if _, err := daemonImportStrategy(newCmd(value)); err == nil {
			t.Errorf("daemonImportStrategy(%q) must be refused: those are bundle strategies", value)
		}
	}
}

func newPackExportCmd(t *testing.T, values map[string]string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	for _, name := range []string{"id", "version", "title", "summary", "description", "domain", "metric-label", "metric-description", "metric-hint"} {
		cmd.Flags().String(name, "", "")
	}
	cmd.Flags().Int("wave", 0, "")
	for name, value := range values {
		if err := cmd.Flags().Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	return cmd
}

// The export endpoint validates the manifest server-side; checking the
// required fields here costs no request and names every missing flag at once.
func TestPackExportManifestRequiresTheManifestFields(t *testing.T) {
	if _, err := packExportManifest(newPackExportCmd(t, nil)); err == nil {
		t.Fatal("an empty manifest must be refused before the request")
	}
	// A blank-but-set flag is as missing as an unset one.
	partial := newPackExportCmd(t, map[string]string{"id": "helpdesk-it", "title": "  ", "summary": "One line."})
	if _, err := packExportManifest(partial); err == nil {
		t.Fatal("a whitespace-only --title must be refused")
	}
	full := newPackExportCmd(t, map[string]string{
		"id": "helpdesk-it", "version": "1.2.0", "title": "Helpdesk", "summary": "One line.",
		"domain": "helpdesk", "metric-label": "First-response delay", "metric-description": "Median time.",
	})
	if err := full.Flags().Set("wave", "2"); err != nil {
		t.Fatal(err)
	}
	manifest, err := packExportManifest(full)
	if err != nil {
		t.Fatalf("packExportManifest = %v", err)
	}
	if manifest["id"] != "helpdesk-it" || manifest["version"] != "1.2.0" || manifest["wave"] != 2 {
		t.Errorf("manifest = %v", manifest)
	}
	metric, ok := manifest["metric"].(map[string]any)
	if !ok || metric["label"] != "First-response delay" || metric["description"] != "Median time." {
		t.Errorf("manifest metric = %v", manifest["metric"])
	}
}

// The name in Content-Disposition decides where a download with no --out is
// written, so it is a path the server chooses. It must stay a bare filename.
func TestAttachmentFilenameCannotEscapeTheWorkingDirectory(t *testing.T) {
	for _, tc := range []struct{ header, want string }{
		{``, ""},
		{`attachment; filename="helpdesk-it-1.0.0.pack.yaml"`, "helpdesk-it-1.0.0.pack.yaml"},
		{`attachment; filename="../../etc/crontab"`, "crontab"},
		{`attachment; filename="/etc/crontab"`, "crontab"},
		{`attachment; filename=".."`, ""},
		{`attachment`, ""},
		{`??? not a header`, ""},
	} {
		if got := attachmentFilename(tc.header); got != tc.want {
			t.Errorf("attachmentFilename(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}

// A report's counts are printed in the pack format's own order, with anything
// the server adds later listed after instead of dropped.
func TestPackCountKindsOrdersKnownKindsFirst(t *testing.T) {
	got := packCountKinds(map[string]int{"agents": 2, "zebras": 1, "labels": 3}, map[string]int{"issue_types": 1, "agents": 1})
	want := []string{"issue_types", "labels", "agents", "zebras"}
	if len(got) != len(want) {
		t.Fatalf("packCountKinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("packCountKinds = %v, want %v", got, want)
		}
	}
}
