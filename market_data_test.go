package main

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"
)

var testClockTime = time.Date(2026, 10, 30, 15, 0, 0, 0, time.UTC)

type fakeMarketData struct {
	calls        int
	request      HistoryRequest
	failure      error
	bars         []DailyBar
	quoteCalls   int
	quote        LatestQuote
	quoteFailure error
}

func (f *fakeMarketData) LatestQuote(_ context.Context, symbol string) (LatestQuote, error) {
	f.quoteCalls++
	if f.quoteFailure != nil {
		return LatestQuote{}, f.quoteFailure
	}
	if f.quote.Symbol != "" {
		return f.quote, nil
	}
	return LatestQuote{Symbol: symbol, Feed: "iex", Timestamp: testClockTime.Add(-time.Second), BidPrice: "99", AskPrice: "100"}, nil
}

func (f *fakeMarketData) DailyBars(_ context.Context, req HistoryRequest) ([]DailyBar, error) {
	f.calls++
	f.request = req
	if f.failure != nil {
		return nil, f.failure
	}
	if f.bars != nil {
		return f.bars, nil
	}
	return historyFixture(req), nil
}
func historyFixture(req HistoryRequest) []DailyBar {
	bars := make([]DailyBar, req.Count)
	for i := range bars {
		bars[i] = DailyBar{Timestamp: req.Cutoff.AddDate(0, 0, -i-1), Open: 100, High: 110, Low: 90, Close: 105, Volume: 0}
	}
	return bars
}
func TestHistoryCutoff(t *testing.T) {
	for _, tt := range []struct{ now, cutoff, start string }{
		{"2026-10-30T02:00:00Z", "2026-10-29T04:00:00Z", "2026-07-01T04:00:00Z"},
		{"2026-03-09T15:00:00Z", "2026-03-09T04:00:00Z", "2025-11-09T05:00:00Z"},
		{"2026-11-02T15:00:00Z", "2026-11-02T05:00:00Z", "2026-07-05T04:00:00Z"},
	} {
		now, _ := time.Parse(time.RFC3339, tt.now)
		req, err := newHistoryRequest("aapl", now)
		if err != nil {
			t.Fatal(err)
		}
		if req.Cutoff.UTC().Format(time.RFC3339) != tt.cutoff || req.Start.UTC().Format(time.RFC3339) != tt.start {
			t.Fatalf("now %s: start=%s cutoff=%s", tt.now, req.Start, req.Cutoff)
		}
	}
	if _, err := newHistoryRequest("AAPL", time.Time{}); err == nil {
		t.Fatal("zero clock accepted")
	}
}
func TestValidateDailyBars(t *testing.T) {
	req, err := newHistoryRequest("AAPL", testClockTime)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name   string
		mutate func([]DailyBar) []DailyBar
		want   string
	}{
		{"valid", func(b []DailyBar) []DailyBar { return b }, ""},
		{"empty", func(b []DailyBar) []DailyBar { return nil }, "history"},
		{"short", func(b []DailyBar) []DailyBar { return b[:19] }, "history"},
		{"duplicate", func(b []DailyBar) []DailyBar { b[1] = b[0]; return b }, "duplicate"},
		{"same date", func(b []DailyBar) []DailyBar { b[1].Timestamp = b[0].Timestamp.Add(time.Hour); return b }, "duplicate"},
		{"unordered", func(b []DailyBar) []DailyBar { b[0], b[1] = b[1], b[0]; return b }, "newest first"},
		{"current day", func(b []DailyBar) []DailyBar { b[0].Timestamp = req.Cutoff; return b }, "timestamp"},
		{"future", func(b []DailyBar) []DailyBar { b[0].Timestamp = req.Cutoff.AddDate(0, 0, 1); return b }, "timestamp"},
		{"old", func(b []DailyBar) []DailyBar { b[19].Timestamp = req.Start.Add(-time.Second); return b }, "timestamp"},
		{"zero timestamp", func(b []DailyBar) []DailyBar { b[0].Timestamp = time.Time{}; return b }, "timestamp"},
		{"zero price", func(b []DailyBar) []DailyBar { b[0].Open = 0; return b }, "invalid"},
		{"negative", func(b []DailyBar) []DailyBar { b[0].Close = -1; return b }, "invalid"},
		{"nan", func(b []DailyBar) []DailyBar { b[0].Close = math.NaN(); return b }, "invalid"},
		{"inf", func(b []DailyBar) []DailyBar { b[0].High = math.Inf(1); return b }, "invalid"},
		{"range", func(b []DailyBar) []DailyBar { b[0].Low = 106; return b }, "inconsistent"},
		{"high", func(b []DailyBar) []DailyBar { b[0].High = 99; return b }, "inconsistent"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.mutate(historyFixture(req))
			got, err := validateDailyBars(req, input)
			if tt.want != "" {
				if err == nil || !strings.Contains(err.Error(), tt.want) {
					t.Fatalf("error=%v want %s", err, tt.want)
				}
				return
			}
			if err != nil || len(got) != 20 || !got[0].Timestamp.Before(got[19].Timestamp) {
				t.Fatalf("history=%v error=%v", got, err)
			}
			if !input[0].Timestamp.After(input[19].Timestamp) {
				t.Fatal("mutated input")
			}
		})
	}
}
func TestCycleMarketDataGuardsAndFailures(t *testing.T) {
	for _, tt := range []struct {
		name                   string
		blocked, badData, fail bool
		wantCalls              int
	}{
		{name: "blocked", blocked: true},
		{name: "API error", fail: true, wantCalls: 1}, {name: "validation error", badData: true, wantCalls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			broker := &fakeBroker{account: AccountSnapshot{TradingBlocked: tt.blocked}}
			data := &fakeMarketData{}
			sentinel := errors.New("data unavailable")
			if tt.fail {
				data.failure = sentinel
			}
			if tt.badData {
				data.bars = []DailyBar{}
			}
			policy := &fakePolicy{decision: Decision{Action: DecisionHold}}
			cycle, err := NewDailyCycle(broker, policy, data, "AAPL")
			if err != nil {
				t.Fatal(err)
			}
			result, err := cycle.Run(context.Background())
			if tt.fail && !errors.Is(err, sentinel) {
				t.Fatalf("error=%v", err)
			}
			if tt.badData && (err == nil || !strings.Contains(err.Error(), "validate daily history")) {
				t.Fatalf("error=%v", err)
			}
			if tt.wantCalls == 0 && (err != nil || len(result.Snapshot.History.Bars) != 0) {
				t.Fatalf("result=%v error=%v", result, err)
			}
			if data.calls != tt.wantCalls || policy.calls != 0 {
				t.Fatalf("data calls=%d policy calls=%d", data.calls, policy.calls)
			}
		})
	}
}
func TestCycleValidatesMarketDataDependencies(t *testing.T) {
	for _, tt := range []struct {
		data   MarketData
		symbol string
		want   string
	}{
		{nil, "AAPL", "market data"}, {&fakeMarketData{}, " ", "symbol"},
	} {
		_, err := NewDailyCycle(&fakeBroker{}, HoldPolicy{}, tt.data, tt.symbol)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestCycleRequestsHistoryUsingBrokerClock(t *testing.T) {
	now := time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC)
	data := &fakeMarketData{}
	policy := &fakePolicy{decision: Decision{Action: DecisionHold}}
	cycle, err := NewDailyCycle(&fakeBroker{clock: MarketClock{Timestamp: now}}, policy, data, " brk.b ")
	if err != nil {
		t.Fatal(err)
	}
	result, err := cycle.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if data.calls != 1 || data.request.Symbol != "BRK.B" || data.request.Cutoff.UTC().Format(time.RFC3339) != "2026-10-29T04:00:00Z" {
		t.Fatalf("calls=%d request=%v", data.calls, data.request)
	}
	history := policy.snapshot.History
	if history.Symbol != "BRK.B" || history.Feed != "iex" || history.Adjustment != "split" || len(history.Bars) != 20 || !history.Bars[0].Timestamp.Before(history.Bars[19].Timestamp) || !result.DryRun {
		t.Fatalf("result=%v", result)
	}
}
