package daemon

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

// F03 · a remediable error. An error task_message carries the classified
// failure reason plus, when one honestly applies, the key naming the UI action
// that fixes it.

func TestErrorRemediationInput(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name            string
		content         string
		wantReason      taskfailure.Reason
		wantRemediation string // "" means the key must be absent
	}{
		{
			name:            "provider auth sends the user to runtime settings",
			content:         "API Error: 401 Unauthorized",
			wantReason:      taskfailure.ReasonAgentProviderAuthOrAccess,
			wantRemediation: RemediationRuntimeSettings,
		},
		{
			name:            "quota is a settings problem, not a retry",
			content:         "You've hit your org's monthly usage limit",
			wantReason:      taskfailure.ReasonAgentProviderQuotaLimit,
			wantRemediation: RemediationRuntimeSettings,
		},
		{
			name:            "a rate limit is worth retrying",
			content:         "API Error: 429 Too Many Requests",
			wantReason:      taskfailure.ReasonAgentProviderCapacityOrRateLimit,
			wantRemediation: RemediationRerun,
		},
		{
			// Context overflow deliberately gets no button: re-running the same
			// prompt overflows again, and there is no setting that fixes it.
			name:       "context overflow offers nothing",
			content:    "API Error: prompt is too long: 250000 tokens > 200000 maximum",
			wantReason: taskfailure.ReasonAgentContextOverflow,
		},
		{
			name:       "an unrecognised error still names its reason",
			content:    "something nobody has classified yet",
			wantReason: taskfailure.ReasonAgentUnknown,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := errorRemediationInput(tc.content)
			if got == nil {
				t.Fatalf("errorRemediationInput(%q) = nil, want at least a reason", tc.content)
			}
			if reason, _ := got["reason"].(string); reason != tc.wantReason.String() {
				t.Errorf("reason = %q, want %q", reason, tc.wantReason)
			}
			remediation, present := got["remediation"]
			if tc.wantRemediation == "" {
				if present {
					t.Errorf("remediation = %v, want the key to be absent — "+
						"an empty key and a missing one must not both appear on the wire", remediation)
				}
				return
			}
			if got, _ := remediation.(string); got != tc.wantRemediation {
				t.Errorf("remediation = %q, want %q", got, tc.wantRemediation)
			}
		})
	}
}

// An error with no text classifies into the catch-all and says nothing a reader
// cannot already see, so it carries no input at all.
func TestErrorRemediationInputEmptyContent(t *testing.T) {
	t.Parallel()
	if got := errorRemediationInput(""); got != nil {
		t.Fatalf("errorRemediationInput(\"\") = %v, want nil", got)
	}
}

// Every remediation key the mapping can emit must be one of the three the
// client knows how to perform. A typo here becomes a dead button.
func TestRemediationKeysAreKnown(t *testing.T) {
	t.Parallel()
	known := map[string]bool{
		RemediationRerun:           true,
		RemediationRuntimeSettings: true,
		RemediationInstallCLI:      true,
	}
	for reason, key := range remediationForReason {
		if !known[key] {
			t.Errorf("reason %q maps to unknown remediation %q", reason, key)
		}
	}
}
