package service

// Pure unit tests: no database.

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The embedding text budget is a byte cap on free-text note title/content —
// CJK content (conventions.zh.mdx) whose cut lands mid-rune must not come
// back as invalid UTF-8.
func TestPassageEmbeddingTextIsUTF8Safe(t *testing.T) {
	title := strings.Repeat("标题", 10)
	body := strings.Repeat("中文混合内容ab测试😀", 2000)
	got := passageEmbeddingText(title, "章节", body)
	if !utf8.ValidString(got) {
		t.Fatalf("passageEmbeddingText produced invalid UTF-8: %q", got)
	}
	if len(got) > noteEmbeddingTextCap {
		t.Fatalf("passageEmbeddingText = %d bytes, want <=%d", len(got), noteEmbeddingTextCap)
	}
}

func TestCalibrateVectorFloor(t *testing.T) {
	related := []float64{0.62, 0.70, 0.74, 0.81, 0.66, 0.77, 0.69, 0.72, 0.80, 0.75}
	unrelated := []float64{0.05, 0.12, 0.20, 0.31, 0.18, 0.09, 0.15, 0.22, 0.11, 0.14, 0.40, 0.25, 0.08, 0.17, 0.19, 0.13, 0.21, 0.10, 0.16, 0.24}
	floor, ok := calibrateVectorFloor(related, unrelated)
	// P95(unrelated) = 0.3145, P10(related) = 0.656: the midpoint.
	if !ok || floor < 0.48 || floor > 0.49 {
		t.Fatalf("floor = %v ok %v, want about 0.485", floor, ok)
	}
	if floor <= percentile(unrelated, 0.95) || floor >= percentile(related, 0.10) {
		t.Errorf("floor %v is not between the unrelated and related percentiles", floor)
	}

	// A model whose related pairs do not clear the unrelated ones gives no floor.
	if _, ok := calibrateVectorFloor([]float64{0.3, 0.35, 0.5}, []float64{0.2, 0.4, 0.45}); ok {
		t.Error("overlapping distributions calibrated a floor")
	}
	if _, ok := calibrateVectorFloor(nil, unrelated); ok {
		t.Error("no related pairs calibrated a floor")
	}
}
