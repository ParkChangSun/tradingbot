package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Broker exposes only the read operations needed to build a daily snapshot.
// Order submission is intentionally absent. Preview constraints do not constitute
// a complete execution or risk policy.
type Broker interface {
	Clock(context.Context) (MarketClock, error)
	Account(context.Context) (AccountSnapshot, error)
	Positions(context.Context) ([]Position, error)
	OpenOrders(context.Context) ([]OpenOrder, error)
}

type MarketClock struct {
	Timestamp time.Time
	IsOpen    bool
	NextOpen  time.Time
	NextClose time.Time
}

type AccountSnapshot struct {
	ID             string
	Status         string
	Currency       string
	BuyingPower    string
	Equity         string
	Cash           string
	TradingBlocked bool
}

type Position struct {
	AssetID           string
	Symbol            string
	AssetClass        string
	Quantity          string
	Side              string
	MarketValue       string
	AverageEntryPrice string
	PortfolioWeight   string // Signed market value / account equity; calculated for AI context.
}

type OpenOrder struct {
	ID            string
	ClientOrderID string
	Symbol        string
	AssetClass    string
	Side          string
	Quantity      string
	Status        string
	SubmittedAt   time.Time
}

type DecisionAction string

const (
	DecisionHold DecisionAction = "hold"
	DecisionBuy  DecisionAction = "buy"
	DecisionSell DecisionAction = "sell"
)

// Decision describes directional intent only. The program's planner owns preview
// quantities; executable order type and time-in-force remain undecided.
type Decision struct {
	Action                   DecisionAction `json:"action"`
	Symbol                   string         `json:"symbol"`
	Rationale                string         `json:"rationale"`
	ExpectedHoldingSessions  *int           `json:"expected_holding_sessions"`
	ReassessmentConditions   []string       `json:"reassessment_conditions"`
	NextReviewDate           string         `json:"next_review_date"`
	PreviousRecommendationID *string        `json:"previous_recommendation_id"`
	ChangeExplanation        string         `json:"change_explanation"`
	RecordID                 string         `json:"-"`
	Usage                    *TokenUsage    `json:"-"`
}

type DecisionPolicy interface {
	Decide(context.Context, DailySnapshot) (Decision, error)
}

// HoldPolicy is the default when AI recommendations are not enabled. It always
// holds; neither policy has access to order submission.
type HoldPolicy struct{}

func (HoldPolicy) Decide(context.Context, DailySnapshot) (Decision, error) {
	return Decision{
		Action:    DecisionHold,
		Rationale: "AI recommendations are not enabled; order submission is not implemented",
	}, nil
}

type DailySnapshot struct {
	Clock      MarketClock
	Account    AccountSnapshot
	Positions  []Position
	OpenOrders []OpenOrder
	History    DailyHistory
}

type CycleOutcome string

const (
	OutcomeTradingBlocked CycleOutcome = "trading_blocked"
	OutcomeNoAction       CycleOutcome = "no_action"
	OutcomeDecisionReady  CycleOutcome = "decision_ready"
)

type CycleResult struct {
	Outcome  CycleOutcome
	Snapshot DailySnapshot
	Decision Decision
	DryRun   bool
}

type DailyCycle struct {
	broker     Broker
	policy     DecisionPolicy
	marketData MarketData
	symbol     string
}

func NewDailyCycle(broker Broker, policy DecisionPolicy, marketData MarketData, symbol string) (*DailyCycle, error) {
	if broker == nil {
		return nil, fmt.Errorf("broker is required")
	}
	if policy == nil {
		return nil, fmt.Errorf("decision policy is required")
	}

	if marketData == nil {
		return nil, fmt.Errorf("market data is required")
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	return &DailyCycle{broker: broker, policy: policy, marketData: marketData, symbol: symbol}, nil
}

func (c *DailyCycle) Run(ctx context.Context) (CycleResult, error) {
	clock, err := c.broker.Clock(ctx)
	if err != nil {
		return CycleResult{}, fmt.Errorf("get market clock: %w", err)
	}

	result := CycleResult{
		Snapshot: DailySnapshot{
			Clock: clock,
		},
		DryRun: true,
	}

	account, err := c.broker.Account(ctx)
	if err != nil {
		return CycleResult{}, fmt.Errorf("get account snapshot: %w", err)
	}
	result.Snapshot.Account = account

	positions, err := c.broker.Positions(ctx)
	if err != nil {
		return CycleResult{}, fmt.Errorf("get positions snapshot: %w", err)
	}
	result.Snapshot.Positions = positions

	openOrders, err := c.broker.OpenOrders(ctx)
	if err != nil {
		return CycleResult{}, fmt.Errorf("get open orders snapshot: %w", err)
	}
	result.Snapshot.OpenOrders = openOrders

	if account.TradingBlocked {
		result.Outcome = OutcomeTradingBlocked
		return result, nil
	}

	// Open orders remain visible to read-only policies. Execution is disabled;
	// a future execution gate must independently handle pending exposure.

	req, err := newHistoryRequest(c.symbol, clock.Timestamp)
	if err != nil {
		return CycleResult{}, err
	}
	bars, err := c.marketData.DailyBars(ctx, req)
	if err != nil {
		return CycleResult{}, fmt.Errorf("get daily history: %w", err)
	}
	bars, err = validateDailyBars(req, bars)
	if err != nil {
		return CycleResult{}, fmt.Errorf("validate daily history: %w", err)
	}
	result.Snapshot.History = DailyHistory{Symbol: c.symbol, Feed: dailyHistoryFeed, Adjustment: dailyHistoryAdjustment, Bars: bars}

	decision, err := c.policy.Decide(ctx, result.Snapshot)
	if err != nil {
		return CycleResult{}, fmt.Errorf("make trading decision: %w", err)
	}
	if err := validateDecision(decision); err != nil {
		return CycleResult{}, fmt.Errorf("invalid trading decision: %w", err)
	}
	result.Decision = decision

	if decision.Action == DecisionHold {
		result.Outcome = OutcomeNoAction
		return result, nil
	}

	// A buy or sell decision is observable but never submitted. Broker has no
	// write method, so this safety boundary cannot be bypassed accidentally.
	result.Outcome = OutcomeDecisionReady
	return result, nil
}

func validateDecision(decision Decision) error {
	switch decision.Action {
	case DecisionHold:
		return nil
	case DecisionBuy, DecisionSell:
		if strings.TrimSpace(decision.Symbol) == "" {
			return fmt.Errorf("symbol is required for %s", decision.Action)
		}
		return nil
	default:
		return fmt.Errorf("unsupported action %q", decision.Action)
	}
}
