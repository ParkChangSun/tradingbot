package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewDailyCycleValidatesDependencies(t *testing.T) {
	broker := &fakeBroker{}
	policy := &fakePolicy{}

	tests := []struct {
		name   string
		broker Broker
		policy DecisionPolicy
		want   string
	}{
		{name: "missing broker", policy: policy, want: "broker is required"},
		{name: "missing policy", broker: broker, want: "decision policy is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDailyCycle(tt.broker, tt.policy)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewDailyCycle error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestDailyCycleStopsWhenMarketIsClosed(t *testing.T) {
	now := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	broker := &fakeBroker{clock: MarketClock{Timestamp: now, IsOpen: false}}
	policy := &fakePolicy{}
	cycle := mustDailyCycle(t, broker, policy)

	result, err := cycle.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Outcome != OutcomeMarketClosed {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeMarketClosed)
	}
	if !result.DryRun {
		t.Error("DryRun = false, want true")
	}
	if !reflect.DeepEqual(broker.calls, []string{"clock"}) {
		t.Errorf("broker calls = %v, want [clock]", broker.calls)
	}
	if policy.calls != 0 {
		t.Errorf("policy calls = %d, want 0", policy.calls)
	}
}

func TestDailyCycleBuildsSnapshotAndHolds(t *testing.T) {
	broker := &fakeBroker{
		clock:     MarketClock{IsOpen: true},
		account:   AccountSnapshot{ID: "account-1", BuyingPower: "10000"},
		positions: []Position{{Symbol: "AAPL", Quantity: "2"}},
	}
	policy := &fakePolicy{decision: Decision{Action: DecisionHold, Rationale: "wait"}}
	cycle := mustDailyCycle(t, broker, policy)

	result, err := cycle.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Outcome != OutcomeNoAction {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeNoAction)
	}
	if !reflect.DeepEqual(broker.calls, []string{"clock", "account", "positions", "open_orders"}) {
		t.Errorf("broker calls = %v", broker.calls)
	}
	if policy.calls != 1 {
		t.Fatalf("policy calls = %d, want 1", policy.calls)
	}
	if !reflect.DeepEqual(policy.snapshot, result.Snapshot) {
		t.Errorf("policy snapshot = %#v, want %#v", policy.snapshot, result.Snapshot)
	}
}

func TestDailyCycleStopsWhenAccountIsTradingBlocked(t *testing.T) {
	broker := &fakeBroker{
		clock:   MarketClock{IsOpen: true},
		account: AccountSnapshot{TradingBlocked: true},
	}
	policy := &fakePolicy{}
	cycle := mustDailyCycle(t, broker, policy)

	result, err := cycle.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Outcome != OutcomeTradingBlocked {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeTradingBlocked)
	}
	if policy.calls != 0 {
		t.Errorf("policy calls = %d, want 0", policy.calls)
	}
}

func TestDailyCycleStopsWhenOpenOrdersExist(t *testing.T) {
	broker := &fakeBroker{
		clock:      MarketClock{IsOpen: true},
		openOrders: []OpenOrder{{ID: "order-1", Symbol: "AAPL"}},
	}
	policy := &fakePolicy{decision: Decision{Action: DecisionBuy, Symbol: "AAPL"}}
	cycle := mustDailyCycle(t, broker, policy)

	result, err := cycle.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Outcome != OutcomeOpenOrdersPresent {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeOpenOrdersPresent)
	}
	if policy.calls != 0 {
		t.Errorf("policy calls = %d, want 0", policy.calls)
	}
}

func TestDailyCycleReportsTradeIntentWithoutSubmitting(t *testing.T) {
	broker := &fakeBroker{clock: MarketClock{IsOpen: true}}
	policy := &fakePolicy{decision: Decision{
		Action:    DecisionBuy,
		Symbol:    "AAPL",
		Rationale: "test signal",
	}}
	cycle := mustDailyCycle(t, broker, policy)

	result, err := cycle.Run(context.Background())
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if result.Outcome != OutcomeDecisionReady {
		t.Errorf("Outcome = %q, want %q", result.Outcome, OutcomeDecisionReady)
	}
	if !result.DryRun {
		t.Error("DryRun = false, want true")
	}
	if !reflect.DeepEqual(result.Decision, policy.decision) {
		t.Errorf("Decision = %#v, want %#v", result.Decision, policy.decision)
	}
}

func TestDailyCyclePropagatesBrokerErrorsWithContext(t *testing.T) {
	tests := []struct {
		name     string
		failCall string
		want     string
	}{
		{name: "clock", failCall: "clock", want: "get market clock"},
		{name: "account", failCall: "account", want: "get account snapshot"},
		{name: "positions", failCall: "positions", want: "get positions snapshot"},
		{name: "open orders", failCall: "open_orders", want: "get open orders snapshot"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			broker := &fakeBroker{
				clock:    MarketClock{IsOpen: true},
				failCall: tt.failCall,
				failure:  errors.New("broker unavailable"),
			}
			cycle := mustDailyCycle(t, broker, &fakePolicy{})

			_, err := cycle.Run(context.Background())
			if err == nil {
				t.Fatal("Run returned nil error")
			}
			if !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), "broker unavailable") {
				t.Errorf("error = %q, want operation and cause", err)
			}
		})
	}
}

func TestDailyCycleReportsPolicyAndValidationErrors(t *testing.T) {
	tests := []struct {
		name   string
		policy *fakePolicy
		want   string
	}{
		{
			name:   "policy failure",
			policy: &fakePolicy{failure: errors.New("model unavailable")},
			want:   "make trading decision: model unavailable",
		},
		{
			name:   "missing symbol",
			policy: &fakePolicy{decision: Decision{Action: DecisionSell}},
			want:   "invalid trading decision: symbol is required for sell",
		},
		{
			name:   "unknown action",
			policy: &fakePolicy{decision: Decision{Action: "wait"}},
			want:   `invalid trading decision: unsupported action "wait"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cycle := mustDailyCycle(t, &fakeBroker{clock: MarketClock{IsOpen: true}}, tt.policy)
			_, err := cycle.Run(context.Background())
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestHoldPolicyAlwaysHolds(t *testing.T) {
	decision, err := (HoldPolicy{}).Decide(context.Background(), DailySnapshot{})
	if err != nil {
		t.Fatalf("Decide returned error: %v", err)
	}
	if decision.Action != DecisionHold {
		t.Errorf("Action = %q, want %q", decision.Action, DecisionHold)
	}
}

type fakeBroker struct {
	clock      MarketClock
	account    AccountSnapshot
	positions  []Position
	openOrders []OpenOrder
	failCall   string
	failure    error
	calls      []string
}

func (b *fakeBroker) Clock(context.Context) (MarketClock, error) {
	b.calls = append(b.calls, "clock")
	if b.failCall == "clock" {
		return MarketClock{}, b.failure
	}
	return b.clock, nil
}

func (b *fakeBroker) Account(context.Context) (AccountSnapshot, error) {
	b.calls = append(b.calls, "account")
	if b.failCall == "account" {
		return AccountSnapshot{}, b.failure
	}
	return b.account, nil
}

func (b *fakeBroker) Positions(context.Context) ([]Position, error) {
	b.calls = append(b.calls, "positions")
	if b.failCall == "positions" {
		return nil, b.failure
	}
	return b.positions, nil
}

func (b *fakeBroker) OpenOrders(context.Context) ([]OpenOrder, error) {
	b.calls = append(b.calls, "open_orders")
	if b.failCall == "open_orders" {
		return nil, b.failure
	}
	return b.openOrders, nil
}

type fakePolicy struct {
	decision Decision
	failure  error
	calls    int
	snapshot DailySnapshot
}

func (p *fakePolicy) Decide(_ context.Context, snapshot DailySnapshot) (Decision, error) {
	p.calls++
	p.snapshot = snapshot
	return p.decision, p.failure
}

func mustDailyCycle(t *testing.T, broker Broker, policy DecisionPolicy) *DailyCycle {
	t.Helper()
	cycle, err := NewDailyCycle(broker, policy)
	if err != nil {
		t.Fatalf("NewDailyCycle returned error: %v", err)
	}
	return cycle
}
