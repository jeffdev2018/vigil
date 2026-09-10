package llm

import (
	"math"
	"testing"
)

func TestFitEmbeddingTruncatesAndRenormalisesWideVectors(t *testing.T) {
	wide := make([]float32, 3072)
	for i := range wide {
		wide[i] = 2
	}
	got := fitEmbedding("wide-model", wide)
	if len(got) != EmbeddingDimensions {
		t.Fatalf("len = %d, want %d", len(got), EmbeddingDimensions)
	}
	var norm float64
	for _, v := range got {
		norm += float64(v) * float64(v)
	}
	if math.Abs(math.Sqrt(norm)-1) > 1e-4 {
		t.Errorf("norm = %v, want 1 after truncation", math.Sqrt(norm))
	}
	// A vector that already fits is returned untouched, shorter ones too:
	// padding would fabricate a vector and the insert must fail instead.
	exact := make([]float32, EmbeddingDimensions)
	exact[0] = 5
	if got := fitEmbedding("m", exact); &got[0] != &exact[0] || got[0] != 5 {
		t.Errorf("an exact-width vector must be returned as is")
	}
	short := []float32{1, 2, 3}
	if got := fitEmbedding("m", short); len(got) != 3 {
		t.Errorf("a short vector must not be padded")
	}
}
