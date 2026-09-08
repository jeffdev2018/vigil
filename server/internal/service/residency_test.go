package service

import (
	"encoding/json"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Data residency (K46). The compliance decision is the whole feature: every
// routing path funnels through RuntimeCompliant, so the matrix lives here and
// the DB-backed tests only check that the paths call it.

func TestDataResidencyPolicyFromSettings(t *testing.T) {
	tests := []struct {
		name     string
		settings string
		want     DataResidencyPolicy
	}{
		{
			name:     "empty settings are no constraint",
			settings: "",
			want:     DataResidencyPolicy{RegionAllowlist: []string{}, BannedProviders: []string{}},
		},
		{
			name:     "settings without the key are no constraint",
			settings: `{"workflow_limits":{"max_legs":3}}`,
			want:     DataResidencyPolicy{RegionAllowlist: []string{}, BannedProviders: []string{}},
		},
		{
			name:     "unparseable settings degrade to no constraint",
			settings: `{ not json`,
			want:     DataResidencyPolicy{RegionAllowlist: []string{}, BannedProviders: []string{}},
		},
		{
			name:     "tokens are normalized on read, not only on write",
			settings: `{"data_residency_policy":{"region_allowlist":[" EU-WEST-1 ","eu-west-1","", "af-south-1"],"banned_providers":["Codex"],"require_on_prem":true}}`,
			want: DataResidencyPolicy{
				RegionAllowlist: []string{"af-south-1", "eu-west-1"},
				BannedProviders: []string{"codex"},
				RequireOnPrem:   true,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DataResidencyPolicyFromSettings([]byte(tt.settings))
			if !policyEqual(got, tt.want) {
				t.Errorf("policy = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestDataResidencyPolicyRestrictive(t *testing.T) {
	tests := []struct {
		name   string
		policy DataResidencyPolicy
		want   bool
	}{
		{name: "empty", policy: DataResidencyPolicy{}, want: false},
		{name: "regions only", policy: DataResidencyPolicy{RegionAllowlist: []string{"eu-west-1"}}, want: true},
		{name: "banned providers only", policy: DataResidencyPolicy{BannedProviders: []string{"codex"}}, want: true},
		{name: "on-prem only", policy: DataResidencyPolicy{RequireOnPrem: true}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.Restrictive(); got != tt.want {
				t.Errorf("Restrictive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidDataResidencyPolicy(t *testing.T) {
	long := make([]string, dataResidencyMaxListLen+1)
	for i := range long {
		long[i] = string(rune('a' + i%26))
	}
	if !ValidDataResidencyPolicy(NormalizeDataResidencyPolicy(DataResidencyPolicy{RegionAllowlist: []string{"eu-west-1"}})) {
		t.Error("a one-region policy must be valid")
	}
	if ValidDataResidencyPolicy(NormalizeDataResidencyPolicy(DataResidencyPolicy{RegionAllowlist: long})) {
		t.Errorf("a policy over the %d-entry cap must be refused", dataResidencyMaxListLen)
	}
	// Normalization collapses duplicates BEFORE the cap, so twenty spellings
	// of one region are one entry rather than a rejection.
	dupes := make([]string, dataResidencyMaxListLen+5)
	for i := range dupes {
		dupes[i] = " EU-West-1 "
	}
	if !ValidDataResidencyPolicy(NormalizeDataResidencyPolicy(DataResidencyPolicy{RegionAllowlist: dupes})) {
		t.Error("duplicates must be collapsed before the cap is applied")
	}
}

func runtimeRow(mode, provider string) db.AgentRuntime {
	return db.AgentRuntime{RuntimeMode: mode, Provider: provider, Name: "rt"}
}

func profileRow(region string, onPrem bool) *db.RuntimeComplianceProfile {
	return &db.RuntimeComplianceProfile{Region: region, OnPrem: onPrem}
}

func TestRuntimeComplianceMatrix(t *testing.T) {
	eu := DataResidencyPolicy{RegionAllowlist: []string{"eu-west-1", "eu-central-1"}}
	onPrem := DataResidencyPolicy{RequireOnPrem: true}
	banned := DataResidencyPolicy{BannedProviders: []string{"codex", "gemini"}}

	tests := []struct {
		name       string
		policy     DataResidencyPolicy
		runtime    db.AgentRuntime
		profile    *db.RuntimeComplianceProfile
		wantOK     bool
		wantReason string
	}{
		{
			name:    "no policy admits an undeclared cloud runtime",
			policy:  DataResidencyPolicy{},
			runtime: runtimeRow("cloud", "codex"),
			wantOK:  true,
		},
		{
			name:       "banned provider is refused whatever it declares",
			policy:     banned,
			runtime:    runtimeRow("local", "codex"),
			profile:    profileRow("eu-west-1", true),
			wantReason: ResidencyReasonBannedProvider,
		},
		{
			name:       "provider match is case-insensitive",
			policy:     banned,
			runtime:    runtimeRow("local", "  Codex "),
			profile:    profileRow("eu-west-1", true),
			wantReason: ResidencyReasonBannedProvider,
		},
		{
			name:    "an unbanned provider passes a provider-only policy",
			policy:  banned,
			runtime: runtimeRow("cloud", "claude"),
			wantOK:  true,
		},
		{
			name:       "a cloud runtime can never be on-prem",
			policy:     onPrem,
			runtime:    runtimeRow("cloud", "claude"),
			profile:    profileRow("eu-west-1", true),
			wantReason: ResidencyReasonNotOnPrem,
		},
		{
			name:       "an undeclared local runtime is not on-prem either (fail closed)",
			policy:     onPrem,
			runtime:    runtimeRow("local", "claude"),
			wantReason: ResidencyReasonNotOnPrem,
		},
		{
			name:       "a local runtime declared off-prem is refused",
			policy:     onPrem,
			runtime:    runtimeRow("local", "claude"),
			profile:    profileRow("eu-west-1", false),
			wantReason: ResidencyReasonNotOnPrem,
		},
		{
			name:    "a local runtime declared on-prem passes",
			policy:  onPrem,
			runtime: runtimeRow("local", "claude"),
			profile: profileRow("eu-west-1", true),
			wantOK:  true,
		},
		{
			name:       "an undeclared runtime fails a region allowlist (fail closed)",
			policy:     eu,
			runtime:    runtimeRow("local", "claude"),
			wantReason: ResidencyReasonRegionNotAllowed,
		},
		{
			name:       "a region outside the allowlist is refused",
			policy:     eu,
			runtime:    runtimeRow("cloud", "claude"),
			profile:    profileRow("us-east-1", false),
			wantReason: ResidencyReasonRegionNotAllowed,
		},
		{
			name:    "region match is case-insensitive and trimmed",
			policy:  eu,
			runtime: runtimeRow("cloud", "claude"),
			profile: profileRow("  EU-West-1 ", false),
			wantOK:  true,
		},
		{
			name:       "the provider check wins over the region check",
			policy:     DataResidencyPolicy{RegionAllowlist: []string{"eu-west-1"}, BannedProviders: []string{"codex"}},
			runtime:    runtimeRow("local", "codex"),
			profile:    profileRow("us-east-1", true),
			wantReason: ResidencyReasonBannedProvider,
		},
		{
			name:       "the on-prem check wins over the region check",
			policy:     DataResidencyPolicy{RegionAllowlist: []string{"eu-west-1"}, RequireOnPrem: true},
			runtime:    runtimeRow("cloud", "claude"),
			profile:    profileRow("us-east-1", false),
			wantReason: ResidencyReasonNotOnPrem,
		},
		{
			name:    "every constraint satisfied at once",
			policy:  DataResidencyPolicy{RegionAllowlist: []string{"eu-west-1"}, BannedProviders: []string{"codex"}, RequireOnPrem: true},
			runtime: runtimeRow("local", "claude"),
			profile: profileRow("eu-west-1", true),
			wantOK:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := RuntimeCompliant(NormalizeDataResidencyPolicy(tt.policy), tt.runtime, tt.profile)
			if ok != tt.wantOK {
				t.Errorf("ok = %v (reason %q), want %v", ok, reason, tt.wantOK)
			}
			if reason != tt.wantReason {
				t.Errorf("reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

// The policy is persisted as JSON in workspace.settings, so the wire shape has
// to survive a round trip — a renamed field would silently disable the policy.
func TestDataResidencyPolicyJSONRoundTrip(t *testing.T) {
	in := DataResidencyPolicy{RegionAllowlist: []string{"eu-west-1"}, BannedProviders: []string{"codex"}, RequireOnPrem: true}
	raw, err := json.Marshal(map[string]any{"data_residency_policy": in})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := DataResidencyPolicyFromSettings(raw); !policyEqual(got, in) {
		t.Errorf("round trip = %+v, want %+v", got, in)
	}
}

func policyEqual(a, b DataResidencyPolicy) bool {
	if a.RequireOnPrem != b.RequireOnPrem ||
		len(a.RegionAllowlist) != len(b.RegionAllowlist) ||
		len(a.BannedProviders) != len(b.BannedProviders) {
		return false
	}
	for i := range a.RegionAllowlist {
		if a.RegionAllowlist[i] != b.RegionAllowlist[i] {
			return false
		}
	}
	for i := range a.BannedProviders {
		if a.BannedProviders[i] != b.BannedProviders[i] {
			return false
		}
	}
	return true
}
