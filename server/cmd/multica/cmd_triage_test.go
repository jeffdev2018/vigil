package main

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestTriageVerdictFromFlags(t *testing.T) {
	for name, tc := range map[string]struct {
		accept, dismiss bool
		want            string
		wantErr         bool
	}{
		"accept":  {accept: true, want: "accept"},
		"dismiss": {dismiss: true, want: "dismiss"},
		"both":    {accept: true, dismiss: true, wantErr: true},
		"neither": {wantErr: true},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := triageVerdictFromFlags(tc.accept, tc.dismiss)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("verdict(%v, %v) = %q, want an error", tc.accept, tc.dismiss, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("verdict(%v, %v): %v", tc.accept, tc.dismiss, err)
			}
			if got != tc.want {
				t.Fatalf("verdict = %q, want %q", got, tc.want)
			}
		})
	}
}

// The skill tells agents to run `multica triage list --pending` and
// `multica triage verdict <id> --accept|--dismiss --reason`. A renamed or
// dropped flag would make that documentation lie.
func TestTriageCommandsExposeDocumentedFlags(t *testing.T) {
	for _, tc := range []struct {
		cmd   *cobra.Command
		flags []string
	}{
		{triageListCmd, []string{"pending", "state", "include-snoozed", "limit", "output"}},
		{triageVerdictCmd, []string{"accept", "dismiss", "reason", "output"}},
	} {
		for _, name := range tc.flags {
			if tc.cmd.Flags().Lookup(name) == nil {
				t.Fatalf("triage %s is missing the documented --%s flag", tc.cmd.Name(), name)
			}
		}
	}
	if triageVerdictCmd.Args == nil {
		t.Fatal("triage verdict must require the item id argument")
	}
}
