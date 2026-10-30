package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

func main() {
	if err := loadEnvironmentFile(defaultEnvFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	config, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	broker, err := NewAlpacaBroker(config.APIKey, config.APISecret, &http.Client{
		Timeout: 15 * time.Second,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	marketData, err := NewAlpacaMarketData(config.APIKey, config.APISecret, &http.Client{Timeout: 15 * time.Second})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := run(context.Background(), os.Stdout, broker, HoldPolicy{}, marketData, config.TradingSymbol); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, w io.Writer, broker Broker, policy DecisionPolicy, marketData MarketData, symbol string) error {
	cycle, err := NewDailyCycle(broker, policy, marketData, symbol)
	if err != nil {
		return err
	}

	result, err := cycle.Run(ctx)
	if err != nil {
		return err
	}

	historySummary := "history=skipped"
	if history := result.Snapshot.History; len(history.Bars) > 0 {
		historySummary = fmt.Sprintf("history=ready symbol=%s bars=%d latest_bar=%s feed=%s adjustment=%s", history.Symbol, len(history.Bars), history.Bars[len(history.Bars)-1].Timestamp.Format(time.DateOnly), history.Feed, history.Adjustment)
	}
	_, err = fmt.Fprintf(
		w,
		"daily cycle outcome=%s dry_run=%t positions=%d open_orders=%d market_open=%t %s\n",
		result.Outcome,
		result.DryRun,
		len(result.Snapshot.Positions),
		len(result.Snapshot.OpenOrders),
		result.Snapshot.Clock.IsOpen, historySummary,
	)
	return err
}
