package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Run-scoped secrets (K09), daemon side: the broker swaps tokens for
// values on the way out and refuses the call when the server refuses.

func TestSubstituteRunSecrets(t *testing.T) {
	t.Parallel()
	token := "mss_" + strings.Repeat("ab", 24)
	calls := 0
	resolve := func(_ context.Context, got string) (string, error) {
		calls++
		if got != token {
			return "", errors.New("unknown")
		}
		return `va"lue`, nil
	}
	raw := []byte(`{"params":{"arguments":{"auth":"Bearer ` + token + `","again":"` + token + `"}}}`)
	out, err := substituteRunSecrets(context.Background(), raw, resolve)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"params":{"arguments":{"auth":"Bearer va\"lue","again":"va\"lue"}}}`; string(out) != want || calls != 1 {
		t.Fatalf("out = %s (calls %d)", out, calls)
	}
	if same, err := substituteRunSecrets(context.Background(), []byte(`{"x":1}`), resolve); err != nil || string(same) != `{"x":1}` || calls != 1 {
		t.Fatal("no token must mean no call and no change")
	}
	if _, err := substituteRunSecrets(context.Background(), []byte(`{"t":"mss_`+strings.Repeat("00", 24)+`"}`), resolve); err == nil {
		t.Fatal("a refused token must fail the call explicitly")
	}
	if same, err := substituteRunSecrets(context.Background(), raw, nil); err != nil || string(same) != string(raw) {
		t.Fatal("no resolver leaves the body untouched")
	}
}
