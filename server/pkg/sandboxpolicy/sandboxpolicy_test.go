package sandboxpolicy

import (
	"reflect"
	"testing"
)

func ptr(p Policy) *Policy { return &p }

func TestMergeNetworkModeTakesTheStrictestLayer(t *testing.T) {
	cases := []struct {
		name   string
		layers []*Policy
		want   string
	}{
		{"no layers", nil, NetworkUnrestricted},
		{"all unrestricted", []*Policy{ptr(Policy{NetworkMode: "unrestricted"}), ptr(Policy{NetworkMode: "unrestricted"})}, NetworkUnrestricted},
		{"allowlist beats unrestricted", []*Policy{ptr(Policy{NetworkMode: "unrestricted"}), ptr(Policy{NetworkMode: "allowlist"})}, NetworkAllowlist},
		{"none beats allowlist, whichever comes first", []*Policy{ptr(Policy{NetworkMode: "none"}), ptr(Policy{NetworkMode: "allowlist"})}, NetworkNone},
		{"none beats allowlist, reverse order", []*Policy{ptr(Policy{NetworkMode: "allowlist"}), ptr(Policy{NetworkMode: "none"})}, NetworkNone},
		{"nil layers contribute nothing", []*Policy{nil, ptr(Policy{NetworkMode: "allowlist"}), nil}, NetworkAllowlist},
		{"an unknown mode cannot weaken the merge", []*Policy{ptr(Policy{NetworkMode: "none"}), ptr(Policy{NetworkMode: "bogus"})}, NetworkNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Merge(tc.layers...).NetworkMode; got != tc.want {
				t.Fatalf("Merge().NetworkMode = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMergeBlockSensitiveFilesIsTheOr(t *testing.T) {
	if Merge(ptr(Policy{NetworkMode: "unrestricted"}), ptr(Policy{NetworkMode: "unrestricted"})).BlockSensitiveFiles {
		t.Fatal("no layer sets it: false")
	}
	if !Merge(ptr(Policy{BlockSensitiveFiles: true}), nil, ptr(Policy{NetworkMode: "none"})).BlockSensitiveFiles {
		t.Fatal("one layer sets it: true")
	}
}

func TestMergeAllowedHostsIntersection(t *testing.T) {
	cases := []struct {
		name   string
		layers []*Policy
		want   []string
	}{
		{
			"intersection over the allowlist layers, normalized and deduped",
			[]*Policy{
				ptr(Policy{NetworkMode: "allowlist", AllowedHosts: []string{"API.Example.com ", "cdn.example.com", "api.example.com"}}),
				ptr(Policy{NetworkMode: "allowlist", AllowedHosts: []string{"api.example.com", "other.example.com"}}),
			},
			[]string{"api.example.com"},
		},
		{
			"an unrestricted layer does not widen the intersection",
			[]*Policy{
				ptr(Policy{NetworkMode: "unrestricted"}),
				ptr(Policy{NetworkMode: "allowlist", AllowedHosts: []string{"api.example.com"}}),
			},
			[]string{"api.example.com"},
		},
		{
			"an allowlist layer with no hosts is the most restrictive of all",
			[]*Policy{
				ptr(Policy{NetworkMode: "allowlist", AllowedHosts: []string{"api.example.com"}}),
				ptr(Policy{NetworkMode: "allowlist"}),
			},
			[]string{},
		},
		{
			"merged none carries no hosts, whatever the layers listed",
			[]*Policy{
				ptr(Policy{NetworkMode: "allowlist", AllowedHosts: []string{"api.example.com"}}),
				ptr(Policy{NetworkMode: "none"}),
			},
			[]string{},
		},
		{
			"merged unrestricted carries no hosts",
			[]*Policy{ptr(Policy{NetworkMode: "unrestricted", AllowedHosts: []string{"api.example.com"}})},
			[]string{},
		},
		{
			"a single allowlist layer passes its hosts through",
			[]*Policy{ptr(Policy{NetworkMode: "allowlist", AllowedHosts: []string{"b.example.com", "a.example.com"}})},
			[]string{"a.example.com", "b.example.com"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Merge(tc.layers...).AllowedHosts; !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Merge().AllowedHosts = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMergeNoLayerIsTheDefault(t *testing.T) {
	if got := Merge(); !reflect.DeepEqual(got, Default()) {
		t.Fatalf("Merge() = %+v, want %+v", got, Default())
	}
	if got := Merge(nil, nil); !reflect.DeepEqual(got, Default()) {
		t.Fatalf("Merge(nil, nil) = %+v, want %+v", got, Default())
	}
}

func TestIsDefault(t *testing.T) {
	if !Default().IsDefault() {
		t.Fatal("Default() is default")
	}
	if (Policy{NetworkMode: "unrestricted", AllowedHosts: []string{" ", ""}}).IsDefault() != true {
		t.Fatal("blank hosts normalize away: still default")
	}
	for _, p := range []Policy{
		{NetworkMode: "allowlist"},
		{NetworkMode: "none"},
		{NetworkMode: "unrestricted", BlockSensitiveFiles: true},
		{NetworkMode: "allowlist", AllowedHosts: []string{"api.example.com"}},
	} {
		if p.IsDefault() {
			t.Fatalf("%+v is not the default", p)
		}
	}
}

func TestIntersectHosts(t *testing.T) {
	got := IntersectHosts([]string{"API.example.com", "b.example.com"}, []string{"api.example.com", "c.example.com"})
	if !reflect.DeepEqual(got, []string{"api.example.com"}) {
		t.Fatalf("IntersectHosts = %v", got)
	}
	if got := IntersectHosts(nil, []string{"api.example.com"}); len(got) != 0 {
		t.Fatalf("nil runtime hosts intersect to nothing: %v", got)
	}
	if got := IntersectHosts([]string{"api.example.com"}, nil); len(got) != 0 {
		t.Fatalf("nil policy hosts intersect to nothing: %v", got)
	}
}

func TestValidateHostMatchesTheRuntimeRule(t *testing.T) {
	for _, ok := range []string{"api.example.com", "a-b.c-d.example.com", "x.y"} {
		if !ValidateHost(ok) {
			t.Errorf("%q must validate", ok)
		}
	}
	for _, bad := range []string{"", "localhost", "not a host", "API.Example.com", "-bad.example.com", "bad_.example.com", "example.com:443"} {
		if ValidateHost(bad) {
			t.Errorf("%q must not validate", bad)
		}
	}
}

func TestFromSettingsAndMetadata(t *testing.T) {
	if FromSettings(nil) != nil || FromSettings([]byte(`{ not json`)) != nil || FromSettings([]byte(`{}`)) != nil {
		t.Fatal("absent or unparseable layer reads as nil")
	}
	p := FromSettings([]byte(`{"sandbox_policy":{"network_mode":"none","allowed_hosts":[],"block_sensitive_files":true}}`))
	if p == nil || p.NetworkMode != NetworkNone || !p.BlockSensitiveFiles {
		t.Fatalf("settings layer: %+v", p)
	}
	q := FromMetadata([]byte(`{"sandbox_policy":{"network_mode":"allowlist","allowed_hosts":["api.example.com"]}}`))
	if q == nil || q.NetworkMode != NetworkAllowlist || len(q.AllowedHosts) != 1 {
		t.Fatalf("metadata layer: %+v", q)
	}
	if FromMetadata([]byte(`{"other":{"network_mode":"none"}}`)) != nil {
		t.Fatal("an unrelated metadata key is not an override")
	}
}
