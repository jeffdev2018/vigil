package handler

import (
	"math/big"
	"strconv"
	"time"

	"github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// This is a frozen observation, not an invoice. Decimal strings retain provider
// ticks exactly across Go, JSON and JavaScript, even above Number.MAX_SAFE_INTEGER.
type DeliveryUsageSnapshot struct {
	CapturedAt       string               `json:"captured_at"`
	Status           string               `json:"status"`
	AvailableUSD     *string              `json:"available_usd"`
	ReportedUSD      string               `json:"reported_usd"`
	EstimatedUSD     string               `json:"estimated_usd"`
	RunIDs           []string             `json:"run_ids"`
	RunsWithoutUsage int                  `json:"runs_without_usage"`
	NonterminalRuns  int                  `json:"nonterminal_runs"`
	UnpricedSlices   int                  `json:"unpriced_slices"`
	Slices           []DeliveryUsageSlice `json:"slices"`
}

type DeliveryUsageSlice struct {
	TaskID       string              `json:"task_id"`
	Provider     string              `json:"provider"`
	Model        string              `json:"model"`
	Tokens       [4]string           `json:"tokens"` // input, output, cache read, cache write
	ReportedUSD  *string             `json:"reported_usd"`
	EstimatedUSD *string             `json:"estimated_usd"`
	Price        *metrics.ModelPrice `json:"catalog_price,omitempty"`
}

func snapshotDeliveryUsage(rows []db.ListIssueDeliveryUsageRow, now time.Time) DeliveryUsageSnapshot {
	result := DeliveryUsageSnapshot{CapturedAt: now.UTC().Format(time.RFC3339Nano), Status: "unavailable", RunIDs: []string{}, Slices: []DeliveryUsageSlice{}}
	seen := map[string]bool{}
	reported, estimated := new(big.Rat), new(big.Rat)
	priced, estimates := 0, 0
	for _, row := range rows {
		id := uuidToString(row.TaskID)
		if !seen[id] {
			seen[id] = true
			result.RunIDs = append(result.RunIDs, id)
			if row.Status != "completed" && row.Status != "failed" && row.Status != "cancelled" {
				result.NonterminalRuns++
			}
		}
		if !row.UsageTaskID.Valid {
			result.RunsWithoutUsage++
			continue
		}
		counts := [4]int64{row.InputTokens, row.OutputTokens, row.CacheReadTokens, row.CacheWriteTokens}
		entry := DeliveryUsageSlice{TaskID: id, Provider: row.Provider, Model: row.Model}
		valid := true
		for i, value := range counts {
			entry.Tokens[i] = strconv.FormatInt(value, 10)
			if value < 0 {
				valid = false
			}
		}
		if row.CostUsdTicks.Valid && row.CostUsdTicks.Int64 >= 0 {
			amount := new(big.Rat).SetFrac(big.NewInt(row.CostUsdTicks.Int64), big.NewInt(metrics.CostUSDTicksPerUSD))
			reported.Add(reported, amount)
			value := amount.FloatString(10)
			entry.ReportedUSD = &value
			priced++
		} else if price, ok := metrics.PriceForModelAlias(row.Model); ok && valid && !row.CostUsdTicks.Valid {
			// Reuse the server's configured catalog and preserve its actual quote.
			// Local browser pricing overrides cannot rewrite an accepted result.
			rates := [4]float64{price.InputPerM, price.OutputPerM, price.CacheReadPerM, price.CacheWritePerM}
			amount := new(big.Rat)
			for i, rate := range rates {
				unit, ok := new(big.Rat).SetString(strconv.FormatFloat(rate, 'f', -1, 64))
				if !ok || unit.Sign() < 0 {
					valid = false
					break
				}
				part := new(big.Rat).Mul(big.NewRat(counts[i], 1_000_000), unit)
				amount.Add(amount, part)
			}
			if valid {
				estimated.Add(estimated, amount)
				value := amount.FloatString(10)
				entry.EstimatedUSD = &value
				entry.Price = &price
				priced++
				estimates++
			} else {
				result.UnpricedSlices++
			}
		} else {
			result.UnpricedSlices++
		}
		result.Slices = append(result.Slices, entry)
	}
	result.ReportedUSD, result.EstimatedUSD = reported.FloatString(10), estimated.FloatString(10)
	if priced > 0 {
		available := new(big.Rat).Add(reported, estimated).FloatString(10)
		result.AvailableUSD = &available
		result.Status = "reported"
		if estimates > 0 {
			result.Status = "estimated"
		}
		if result.RunsWithoutUsage > 0 || result.NonterminalRuns > 0 || result.UnpricedSlices > 0 {
			result.Status = "partial"
		}
	}
	return result
}
