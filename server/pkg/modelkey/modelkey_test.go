package modelkey

import "testing"

func TestVocabulary(t *testing.T) {
	t.Parallel()
	if v, ok := VendorByID("anthropic"); !ok || v.EnvVar != "ANTHROPIC_API_KEY" {
		t.Fatal("anthropic vendor")
	}
	if _, ok := VendorByID("acme"); ok {
		t.Fatal("unknown vendor")
	}
	if VendorForRuntime(" Claude ") != "anthropic" || VendorForRuntime("codex") != "openai" || VendorForRuntime("cursor") != "" {
		t.Fatal("runtime → vendor")
	}
	if Hint("sk-ant-api03-abcdefghijklmnop1a2b") != "sk-***1a2b" || Hint("short") != "***" {
		t.Fatalf("hint: %s", Hint("sk-ant-api03-abcdefghijklmnop1a2b"))
	}
}
