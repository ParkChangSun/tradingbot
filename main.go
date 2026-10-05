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

	if err := run(context.Background(), os.Stdout, broker, HoldPolicy{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, w io.Writer, broker Broker, policy DecisionPolicy) error {
	cycle, err := NewDailyCycle(broker, policy)
	if err != nil {
		return err
	}

	result, err := cycle.Run(ctx)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(
		w,
		"daily cycle outcome=%s dry_run=%t positions=%d open_orders=%d\n",
		result.Outcome,
		result.DryRun,
		len(result.Snapshot.Positions),
		len(result.Snapshot.OpenOrders),
	)
	return err
}
