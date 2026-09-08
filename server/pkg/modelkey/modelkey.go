// Package modelkey is the BYOK vocabulary (K48): which model vendors a key
// can be declared for, the environment variable each vendor's CLI reads, the
// vendor behind a runtime provider, and how a key is shown without leaking.
package modelkey

import "strings"

// Vendor is a model API vendor a key belongs to.
type Vendor struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	EnvVar string `json:"env_var"`
}

// Vendors lists the vendors a key may be declared for.
var Vendors = []Vendor{
	{ID: "anthropic", Label: "Anthropic", EnvVar: "ANTHROPIC_API_KEY"},
	{ID: "openai", Label: "OpenAI", EnvVar: "OPENAI_API_KEY"},
	{ID: "xai", Label: "xAI", EnvVar: "XAI_API_KEY"},
	{ID: "google", Label: "Google", EnvVar: "GEMINI_API_KEY"},
	{ID: "moonshot", Label: "Moonshot", EnvVar: "MOONSHOT_API_KEY"},
	{ID: "deepseek", Label: "DeepSeek", EnvVar: "DEEPSEEK_API_KEY"},
	{ID: "mistral", Label: "Mistral", EnvVar: "MISTRAL_API_KEY"},
	{ID: "openrouter", Label: "OpenRouter", EnvVar: "OPENROUTER_API_KEY"},
}

// VendorByID returns the vendor, or false for an unknown id.
func VendorByID(id string) (Vendor, bool) {
	for _, v := range Vendors {
		if v.ID == id {
			return v, true
		}
	}
	return Vendor{}, false
}

// runtimeVendors maps a runtime provider (the CLI) to the vendor whose key it
// spends. A CLI that fronts several vendors keeps its own configuration.
var runtimeVendors = map[string]string{
	"claude": "anthropic", "codex": "openai", "grok": "xai", "kimi": "moonshot",
	"qwen": "openrouter", "gemini": "google", "mistral": "mistral", "deepseek": "deepseek",
}

// VendorForRuntime is the vendor a runtime provider spends, or "" when the
// CLI is not tied to one.
func VendorForRuntime(provider string) string {
	return runtimeVendors[strings.ToLower(strings.TrimSpace(provider))]
}

// Hint is what a stored key is shown as: the first three and last four
// characters, never the middle.
func Hint(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 8 {
		return "***"
	}
	return key[:3] + "***" + key[len(key)-4:]
}
