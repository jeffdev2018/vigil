package pricing

import (
	"math"
	"regexp"
	"strings"
)

// TicksPerUSD is the provider accounting unit used by task_usage.
const TicksPerUSD int64 = 10_000_000_000

// Rate is the USD price per one million tokens.
type Rate struct {
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
}

// Usage is the pricing input shared by budget estimates and settlement.
type Usage struct {
	Provider         string
	Model            string
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSDTicks     *int64
}

var rates = map[string]Rate{
	"claude-sonnet-5":              {2, 10, 0.20, 2.50},
	"claude-fable-5-1":             {10, 50, 0.25, 12.50},
	"claude-fable-5":               {10, 50, 1.00, 12.50},
	"claude-opus-5":                {5, 25, 0.50, 6.25},
	"claude-haiku-4-5":             {1, 5, 0.10, 1.25},
	"claude-sonnet-4-5":            {3, 15, 0.30, 3.75},
	"claude-sonnet-4-6":            {3, 15, 0.30, 3.75},
	"claude-opus-4-5":              {5, 25, 0.50, 6.25},
	"claude-opus-4-6":              {5, 25, 0.50, 6.25},
	"claude-opus-4-7":              {5, 25, 0.50, 6.25},
	"claude-opus-4-8":              {5, 25, 0.50, 6.25},
	"claude-opus-4-1":              {15, 75, 1.50, 18.75},
	"claude-opus-4":                {15, 75, 1.50, 18.75},
	"claude-sonnet-4":              {3, 15, 0.30, 3.75},
	"claude-haiku-3-5":             {0.80, 4, 0.08, 1.00},
	"gpt-5.6-sol":                  {5, 30, 0.50, 6.25},
	"gpt-5.6-terra":                {2.50, 15, 0.25, 3.125},
	"gpt-5.6-luna":                 {1, 6, 0.10, 1.25},
	"gpt-5.5":                      {5, 30, 0.50, 5},
	"gpt-5.4-mini":                 {0.75, 4.50, 0.075, 0.75},
	"gpt-5.4":                      {2.50, 15, 0.25, 2.50},
	"gpt-5.3-codex":                {1.75, 14, 0.175, 1.75},
	"gpt-5-codex":                  {1.25, 10, 0.125, 1.25},
	"gpt-5-mini":                   {0.25, 2, 0.025, 0.25},
	"gpt-5-nano":                   {0.05, 0.40, 0.005, 0.05},
	"gpt-5":                        {1.25, 10, 0.125, 1.25},
	"o3-mini":                      {1.10, 4.40, 0.55, 1.10},
	"o3":                           {2, 8, 0.50, 2},
	"o4-mini":                      {1.10, 4.40, 0.275, 1.10},
	"gpt-4o-mini":                  {0.15, 0.60, 0.075, 0.15},
	"gpt-4o":                       {2.50, 10, 1.25, 2.50},
	"deepseek-v4-flash":            {0.14, 0.28, 0.0028, 0.14},
	"deepseek-v4-pro":              {1.74, 3.48, 0.0145, 1.74},
	"deepseek-chat":                {0.14, 0.28, 0.0028, 0.14},
	"deepseek-reasoner":            {0.14, 0.28, 0.0028, 0.14},
	"kimi-k2.6":                    {0.95, 4.00, 0.16, 0.95},
	"kimi-k3":                      {3.0, 15.0, 0.30, 3.0},
	"kimi/k3":                      {3.0, 15.0, 0.30, 3.0},
	"glm-5.1":                      {1.4, 4.4, 0.26, 1.4},
	"glm-5":                        {1.0, 3.2, 0.2, 1.0},
	"glm-5-turbo":                  {1.2, 4.0, 0.24, 1.2},
	"glm-4.7":                      {0.6, 2.2, 0.11, 0.6},
	"glm-4.7-flashx":               {0.07, 0.4, 0.01, 0.07},
	"glm-4.7-flash":                {0, 0, 0, 0},
	"glm-4.6":                      {0.6, 2.2, 0.11, 0.6},
	"glm-4.5":                      {0.6, 2.2, 0.11, 0.6},
	"glm-4.5-x":                    {2.2, 8.9, 0.45, 2.2},
	"glm-4.5-air":                  {0.2, 1.1, 0.03, 0.2},
	"glm-4.5-airx":                 {1.1, 4.5, 0.22, 1.1},
	"glm-4.5-flash":                {0, 0, 0, 0},
	"qwen3.7-plus":                 {0.40, 1.60, 0.04, 0.50},
	"qwen3.6-flash":                {0.25, 1.50, 0.025, 0.3125},
	"qwen3.8-max":                  {2.00, 6.00, 0.17, 2.5},
	"qwen3.8-max-preview":          {0, 0, 0, 0},
	"grok-4.6":                     {2, 6, 0.50, 2},
	"grok-4.5":                     {2, 6, 0.30, 2},
	"grok-4.3":                     {1.25, 2.50, 0.20, 1.25},
	"grok-build-0.1":               {1, 2, 0.20, 1},
	"grok-4.20-multi-agent-0309":   {1.25, 2.50, 0.20, 1.25},
	"grok-4.20-0309-reasoning":     {1.25, 2.50, 0.20, 1.25},
	"grok-4.20-0309-non-reasoning": {1.25, 2.50, 0.20, 1.25},
	"cursor/auto":                  {1.25, 6, 0.25, 0},
	"cursor/composer-2.5-fast":     {3, 15, 0.5, 0},
	"cursor/composer-2.5":          {0.5, 2.5, 0.2, 0},
	"cursor/composer-2-fast":       {1.5, 7.5, 0.35, 0},
	"cursor/composer-2":            {0.5, 2.5, 0.2, 0},
	"cursor/composer-1.5":          {3.5, 17.5, 0.35, 0},
	"cursor/composer-1":            {1.25, 10, 0.125, 0},
	"cursor":                       {3, 15, 0.5, 0},
}

var snapshotSuffix = regexp.MustCompile(`-(20\d{2}-\d{2}-\d{2}|20\d{6}|latest)$`)
var contextSuffix = regexp.MustCompile(`\[[^\]]+\]$`)

// Resolve returns the first exact rate after the same normalizations as the UI.
func Resolve(model, provider string) (Rate, bool) {
	for _, candidate := range pricingCandidates(model, provider) {
		if rate, ok := rates[candidate]; ok {
			return rate, true
		}
	}
	return Rate{}, false
}

func pricingCandidates(model, provider string) []string {
	if model == "" {
		return nil
	}
	base := canonicalCandidates(model)
	provider = strings.ToLower(strings.TrimSpace(provider))
	out := make([]string, 0, len(base)*2)
	if provider != "" {
		for _, candidate := range base {
			if strings.HasPrefix(candidate, provider+"/") {
				out = append(out, candidate)
			} else {
				out = append(out, provider+"/"+candidate)
			}
		}
	}
	return append(out, base...)
}

func canonicalCandidates(model string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 8)
	push := func(value string) {
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	stripProvider := func(value string) string {
		for {
			i, j := strings.IndexByte(value, '/'), strings.IndexByte(value, ':')
			separator := i
			if separator < 0 || (j >= 0 && j < separator) {
				separator = j
			}
			if separator <= 0 || !routingPrefix(value[:separator]) {
				return value
			}
			value = value[separator+1:]
		}
	}
	canonicalClaude := func(value string) string {
		if strings.HasPrefix(value, "claude-") {
			return strings.ReplaceAll(value, ".", "-")
		}
		return value
	}
	raw := model
	withoutProvider := stripProvider(raw)
	claude := canonicalClaude(withoutProvider)
	withoutContext := contextSuffix.ReplaceAllString(claude, "")
	for _, value := range []string{raw, withoutProvider, claude, withoutContext} {
		push(value)
	}
	for _, value := range []string{raw, withoutProvider, claude, withoutContext} {
		push(snapshotSuffix.ReplaceAllString(value, ""))
	}
	return out
}

func routingPrefix(value string) bool {
	if value == "" || value[0] < 'A' || (value[0] > 'Z' && value[0] < 'a') || value[0] > 'z' {
		return false
	}
	for _, r := range value[1:] {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return false
		}
	}
	return true
}

// EstimateTicks returns authoritative provider cost plus the fallback estimate.
func EstimateTicks(usage Usage) int64 {
	authoritative := int64(0)
	if usage.CostUSDTicks != nil && *usage.CostUSDTicks > 0 {
		authoritative = *usage.CostUSDTicks
		return authoritative
	}
	rate, ok := Resolve(usage.Model, usage.Provider)
	if !ok {
		return authoritative
	}
	estimatedUSD := (float64(usage.InputTokens)*rate.Input +
		float64(usage.OutputTokens)*rate.Output +
		float64(usage.CacheReadTokens)*rate.CacheRead +
		float64(usage.CacheWriteTokens)*rate.CacheWrite) / 1_000_000
	return authoritative + int64(math.Round(estimatedUSD*float64(TicksPerUSD)))
}

// EstimateTotalTicks prices a complete run composed of multiple usage rows.
func EstimateTotalTicks(rows []Usage) int64 {
	var total int64
	for _, row := range rows {
		total += EstimateTicks(row)
	}
	return total
}
