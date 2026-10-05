package main

import (
	"bytes"
	"context"
	"testing"
)

func TestRunPrintsCycleResult(t *testing.T) {
	broker := &fakeBroker{
		clock: MarketClock{IsOpen: true},
		positions: []Position{
			{Symbol: "AAPL"},
		},
	}
	var output bytes.Buffer

	if err := run(context.Background(), &output, broker, HoldPolicy{}); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	const want = "daily cycle outcome=no_action dry_run=true positions=1 open_orders=0\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
