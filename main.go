package main

import (
	"context"
	"encoding/json"
	"errors"
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

	var policy DecisionPolicy = HoldPolicy{}
	if config.DecisionPolicy == "openai" {
		policy, err = NewOpenAIPolicy(config.OpenAIKey, config.OpenAIModel, config.CycleBudgetNanodollars,
			&http.Client{Timeout: 60 * time.Second}, FileRecommendationJournal{Directory: config.RecommendationDirectory})
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if err := run(context.Background(), os.Stdout, broker, policy, marketData, config.TradingSymbol, config.PlannerLimits); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, w io.Writer, broker Broker, policy DecisionPolicy, marketData MarketData, symbol string, limits PlannerLimits) error {
	if err := limits.validate(); err != nil {
		return err
	}
	cycle, err := NewDailyCycle(broker, policy, marketData, symbol)
	if err != nil {
		return err
	}
	result, err := cycle.Run(ctx)
	if err != nil {
		return err
	}
	plan, planErr := refreshOrderPlan(ctx, broker, marketData, result.Decision, cycle.symbol, result.Snapshot.Account.ID, limits)

	historySummary := "history=skipped"
	if history := result.Snapshot.History; len(history.Bars) > 0 {
		historySummary = fmt.Sprintf("history=ready symbol=%s bars=%d latest_bar=%s feed=%s adjustment=%s", history.Symbol, len(history.Bars), history.Bars[len(history.Bars)-1].Timestamp.Format(time.DateOnly), history.Feed, history.Adjustment)
	}
	_, err = fmt.Fprintf(
		w,
		"daily cycle outcome=%s dry_run=%t execution=disabled risk_limits=preview_configured plan=%s positions=%d open_orders=%d market_open=%t %s\n",
		result.Outcome,
		result.DryRun,
		plan.Status,
		len(result.Snapshot.Positions),
		len(result.Snapshot.OpenOrders),
		result.Snapshot.Clock.IsOpen, historySummary,
	)
	if err != nil {
		return errors.Join(err, planErr)
	}
	if result.Decision.RecordID != "" {
		err = json.NewEncoder(w).Encode(struct {
			RecordID       string           `json:"record_id"`
			Recommendation Decision         `json:"recommendation"`
			Validation     ValidationReport `json:"validation"`
			Usage          *TokenUsage      `json:"usage"`
			Plan           OrderPlan        `json:"order_plan"`
		}{result.Decision.RecordID, result.Decision, executionDisabled("valid", result.Snapshot), result.Decision.Usage, plan})
	} else {
		err = json.NewEncoder(w).Encode(struct {
			Plan OrderPlan `json:"order_plan"`
		}{plan})
	}
	return errors.Join(err, planErr)
}
