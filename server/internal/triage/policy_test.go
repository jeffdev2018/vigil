package triage

import "testing"

func TestDecide(t *testing.T) {
	cases := []struct {
		mode string
		want Route
	}{
		{"gate", RouteQueue},
		{"blocked", RouteDrop},
		{"direct", RouteDirect},
		{"", RouteDirect},      // unset fails open
		{"bogus", RouteDirect}, // misconfiguration fails open
	}
	for _, tc := range cases {
		if got := Decide(tc.mode); got != tc.want {
			t.Errorf("Decide(%q) = %q, want %q", tc.mode, got, tc.want)
		}
	}
}

func TestNormalizeTitle(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  Hello   WORLD \t\n", "hello world"},
		{"already lower", "already lower"},
		{"Tabs\tand\nnewlines", "tabs and newlines"},
	}
	for _, tc := range cases {
		if got := NormalizeTitle(tc.in); got != tc.want {
			t.Errorf("NormalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeTitleMatchesIssueDuplicateGuard(t *testing.T) {
	// issueguard matches on lower(btrim(regexp_replace(title,'[[:space:]]+',' ','g'))).
	// Two spellings of the same title must normalize identically so queue
	// collapse and issue duplicate detection agree.
	a := NormalizeTitle("Payment gateway timeout")
	b := NormalizeTitle("  payment   gateway\ttimeout ")
	if a != b {
		t.Fatalf("normalization disagrees with itself: %q vs %q", a, b)
	}
}
