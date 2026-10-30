package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunPrintsCycleResult(t *testing.T) {
	broker := &fakeBroker{
		clock: MarketClock{IsOpen: true},
		positions: []Position{
			{Symbol: "AAPL"},
		},
	}
	var output bytes.Buffer

	if err := run(context.Background(), &output, broker, HoldPolicy{}, &fakeMarketData{}, "AAPL"); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	const want = "daily cycle outcome=no_action dry_run=true positions=1 open_orders=0 market_open=true history=ready symbol=AAPL bars=20 latest_bar=2026-10-29 feed=iex adjustment=split\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}

func TestRunPrintsGuardedCycle(t *testing.T) {
	broker := &fakeBroker{account: AccountSnapshot{TradingBlocked: true}}
	data := &fakeMarketData{}
	var output bytes.Buffer
	if err := run(context.Background(), &output, broker, HoldPolicy{}, data, "AAPL"); err != nil {
		t.Fatal(err)
	}
	const want = "daily cycle outcome=trading_blocked dry_run=true positions=0 open_orders=0 market_open=false history=skipped\n"
	if output.String() != want || data.calls != 0 {
		t.Fatalf("output=%q data calls=%d", output.String(), data.calls)
	}
}

func TestRunPrintsClosedMarketHistoryDate(t *testing.T) {
	// The broker clock is already the next UTC day, but still October 29 in New York.
	now := time.Date(2026, 10, 30, 2, 0, 0, 0, time.UTC)
	broker := &fakeBroker{clock: MarketClock{Timestamp: now}}
	req, err := newHistoryRequest("AAPL", now)
	if err != nil {
		t.Fatal(err)
	}
	bars := historyFixture(req)
	for i := range bars {
		bars[i].Timestamp = bars[i].Timestamp.UTC()
	}
	var output bytes.Buffer
	if err := run(context.Background(), &output, broker, HoldPolicy{}, &fakeMarketData{bars: bars}, "AAPL"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "market_open=false history=ready symbol=AAPL bars=20 latest_bar=2026-10-28") {
		t.Fatalf("output=%q", output.String())
	}
}

type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }
func TestRunPropagatesOutputError(t *testing.T) {
	failure := errors.New("write failed")
	err := run(context.Background(), failingWriter{failure}, &fakeBroker{}, HoldPolicy{}, &fakeMarketData{}, "AAPL")
	if !errors.Is(err, failure) {
		t.Fatalf("error=%v", err)
	}
}
