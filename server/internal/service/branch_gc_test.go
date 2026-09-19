package service

import "testing"

// Canonical coverage of the branch GC settings (JEF-388): a workspace reads
// "disabled" out of anything it did not deliberately write, and ttl_days is
// clamped to the documented range. The handler suite covers the sweep itself.

func TestBranchGCFromSettingsFallsBackAndClamps(t *testing.T) {
	cases := []struct {
		name string
		blob string
		want BranchGCSettings
	}{
		{"empty blob", "", BranchGCDefaults()},
		{"no key", `{"code_health":{"enabled":true}}`, BranchGCDefaults()},
		{"unparseable", `not json`, BranchGCDefaults()},
		{"disabled reads the default ttl", `{"branch_gc":{"enabled":false}}`, BranchGCDefaults()},
		{"ttl below range falls back", `{"branch_gc":{"enabled":true,"ttl_days":0}}`, BranchGCSettings{Enabled: true, TTLDays: BranchGCDefaultTTLDays}},
		{"ttl above range falls back", `{"branch_gc":{"enabled":true,"ttl_days":9000}}`, BranchGCSettings{Enabled: true, TTLDays: BranchGCDefaultTTLDays}},
		{"ttl at the low bound", `{"branch_gc":{"enabled":true,"ttl_days":1}}`, BranchGCSettings{Enabled: true, TTLDays: 1}},
		{"ttl at the high bound", `{"branch_gc":{"enabled":true,"ttl_days":365}}`, BranchGCSettings{Enabled: true, TTLDays: 365}},
		{"configured", `{"branch_gc":{"enabled":true,"ttl_days":14}}`, BranchGCSettings{Enabled: true, TTLDays: 14}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BranchGCFromSettings([]byte(tc.blob)); got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}
