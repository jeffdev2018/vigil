package pricing

import "testing"

func TestEstimateTicksMatchesUIRules(t *testing.T) {
	tests := []struct {
		name  string
		usage Usage
		want  int64
	}{
		{
			name:  "gpt rate",
			usage: Usage{Model: "gpt-5.4", InputTokens: 1_000_000, OutputTokens: 1_000_000},
			want:  175_000_000_000,
		},
		{
			name:  "claude transport normalization",
			usage: Usage{Model: "anthropic/claude-opus-4.7-20250929", InputTokens: 1_000_000},
			want:  50_000_000_000,
		},
		{
			name:  "provider-qualified cursor model",
			usage: Usage{Provider: "Cursor", Model: "auto", OutputTokens: 1_000_000},
			want:  60_000_000_000,
		},
		{
			name:  "unknown model is unpriced",
			usage: Usage{Model: "unknown", InputTokens: 1_000_000},
			want:  0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := EstimateTicks(test.usage); got != test.want {
				t.Fatalf("EstimateTicks() = %d, want %d", got, test.want)
			}
		})
	}

	authoritative := int64(1234)
	if got := EstimateTicks(Usage{Model: "gpt-5.4", InputTokens: 1_000_000, CostUSDTicks: &authoritative}); got != authoritative {
		t.Fatalf("authoritative cost = %d, want %d", got, authoritative)
	}
}
