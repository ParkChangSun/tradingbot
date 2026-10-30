package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"
	_ "time/tzdata" // Keep New York date handling available in minimal containers.
)

const (
	dailyHistoryCount      = 20
	dailyHistoryWindowDays = 120
	dailyHistoryFeed       = "iex"
	dailyHistoryAdjustment = "split"
)

// DailyBar prices are analytical float values, not amounts for order sizing.
type DailyBar struct {
	Timestamp              time.Time
	Open, High, Low, Close float64
	Volume                 uint64
}

type HistoryRequest struct {
	Symbol        string
	Start, Cutoff time.Time // Start inclusive, Cutoff exclusive.
	Count         int
}

type DailyHistory struct {
	Symbol, Feed, Adjustment string
	Bars                     []DailyBar // Oldest first, validated before policy evaluation.
}

type MarketData interface {
	DailyBars(context.Context, HistoryRequest) ([]DailyBar, error) // Newest first.
}

func newHistoryRequest(symbol string, now time.Time) (HistoryRequest, error) {
	if now.IsZero() {
		return HistoryRequest{}, fmt.Errorf("market clock timestamp is required")
	}
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		return HistoryRequest{}, fmt.Errorf("load market timezone: %w", err)
	}
	local := now.In(location)
	cutoff := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	return HistoryRequest{
		Symbol: strings.ToUpper(strings.TrimSpace(symbol)),
		Start:  cutoff.AddDate(0, 0, -dailyHistoryWindowDays),
		Cutoff: cutoff,
		Count:  dailyHistoryCount,
	}, nil
}

func validateHistoryRequest(req HistoryRequest) error {
	if strings.TrimSpace(req.Symbol) == "" {
		return fmt.Errorf("symbol is required")
	}
	if req.Count <= 0 || req.Count > dailyHistoryCount {
		return fmt.Errorf("history count must be between 1 and %d", dailyHistoryCount)
	}
	if req.Start.IsZero() || req.Cutoff.IsZero() || !req.Start.Before(req.Cutoff) {
		return fmt.Errorf("invalid history interval")
	}
	return nil
}

// validateDailyBars rejects malformed input rather than sorting or filling gaps.
// It does not prove exchange-session completeness or freshness.
func validateDailyBars(req HistoryRequest, bars []DailyBar) ([]DailyBar, error) {
	if err := validateHistoryRequest(req); err != nil {
		return nil, err
	}
	if len(bars) != req.Count {
		return nil, fmt.Errorf("insufficient or unexpected history: got %d bars, want %d", len(bars), req.Count)
	}
	seen := make(map[string]bool, len(bars))
	result := make([]DailyBar, len(bars))
	for i, bar := range bars {
		if bar.Timestamp.IsZero() || bar.Timestamp.Before(req.Start) || !bar.Timestamp.Before(req.Cutoff) {
			return nil, fmt.Errorf("bar %d timestamp outside history interval", i)
		}
		date := bar.Timestamp.In(req.Cutoff.Location()).Format(time.DateOnly)
		if seen[date] {
			return nil, fmt.Errorf("duplicate daily bar for %s", date)
		}
		seen[date] = true
		if i > 0 && !bar.Timestamp.Before(bars[i-1].Timestamp) {
			return nil, fmt.Errorf("bars must be newest first")
		}
		for _, price := range []float64{bar.Open, bar.High, bar.Low, bar.Close} {
			if math.IsNaN(price) || math.IsInf(price, 0) || price <= 0 {
				return nil, fmt.Errorf("bar %d has invalid OHLC price", i)
			}
		}
		if bar.Low > bar.High || bar.Low > bar.Open || bar.Low > bar.Close || bar.High < bar.Open || bar.High < bar.Close {
			return nil, fmt.Errorf("bar %d has inconsistent OHLC prices", i)
		}
		bar.Timestamp = bar.Timestamp.In(req.Cutoff.Location())
		result[len(bars)-1-i] = bar
	}
	return result, nil
}
