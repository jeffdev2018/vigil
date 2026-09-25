package packs

import (
	"strings"
	"testing"
)

func TestBuiltinCatalogueParses(t *testing.T) {
	all, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("no built-in pack")
	}
	seen := map[string]bool{}
	for _, p := range all {
		if seen[p.Manifest.ID] {
			t.Fatalf("duplicate pack id %q", p.Manifest.ID)
		}
		seen[p.Manifest.ID] = true
		if !p.Builtin || len(p.Body) == 0 {
			t.Fatalf("%s: not marked builtin or empty body", p.Manifest.ID)
		}
	}
}

func TestParseRejectsBadManifests(t *testing.T) {
	cases := map[string]string{
		"no manifest": "agents: []\n",
		"bad id":      "pack: {id: Bad_ID, version: 1.0.0, title: x, summary: y, domain: ops, metric: {label: a, description: b}}\n",
		"bad version": "pack: {id: ok, version: 1, title: x, summary: y, domain: ops, metric: {label: a, description: b}}\n",
		"bad domain":  "pack: {id: ok, version: 1.0.0, title: x, summary: y, domain: nope, metric: {label: a, description: b}}\n",
		"no metric":   "pack: {id: ok, version: 1.0.0, title: x, summary: y, domain: ops}\n",
		"bad prereq":  "pack: {id: ok, version: 1.0.0, title: x, summary: y, domain: ops, metric: {label: a, description: b}, prerequisites: [{kind: magic, name: x}]}\n",
		"not yaml":    "pack: [unclosed\n",
	}
	for name, raw := range cases {
		if _, err := Parse([]byte(raw)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	p, err := Parse([]byte("pack: {id: ok, version: 1.2.3, title: x, summary: y, domain: ops, metric: {label: a, description: b}}\nagents:\n  - name: A\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(p.Body), `"agents"`) || strings.Contains(string(p.Body), `"pack"`) {
		t.Fatalf("body = %s", p.Body)
	}
}

func TestCompareVersions(t *testing.T) {
	if CompareVersions("1.0.0", "1.0.1") != -1 || CompareVersions("1.10.0", "1.9.0") != 1 || CompareVersions("2.0.0", "2.0.0") != 0 {
		t.Fatal("semver order")
	}
}
