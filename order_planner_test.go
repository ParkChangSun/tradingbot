package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func planningFixture() (DailySnapshot, LatestQuote) {
	return DailySnapshot{Clock: MarketClock{Timestamp: testClockTime, IsOpen: true}, Account: AccountSnapshot{ID: "paper-account", Status: "ACTIVE", Currency: "USD", Equity: "10000", Cash: "10000", BuyingPower: "40000"}},
		LatestQuote{Symbol: "AAPL", Feed: "iex", Timestamp: testClockTime.Add(-time.Second), BidPrice: "9", AskPrice: "10"}
}

func planFor(action DecisionAction, s DailySnapshot, q LatestQuote) OrderPlan {
	return planOrder(Decision{Action: action, Symbol: "AAPL"}, "AAPL", "paper-account", s, q, defaultPlannerLimits())
}

func assertBlocked(t *testing.T, p OrderPlan, name string) {
	t.Helper()
	if p.Status != "blocked" || p.Proposed != nil || p.Execution != "disabled" {
		t.Fatalf("unexpected plan status=%s proposal=%v", p.Status, p.Proposed)
	}
	for _, check := range p.Checks {
		if check.Name == name && check.Status == "blocked" {
			return
		}
	}
	t.Fatalf("missing blocked check %s: %v", name, p.Checks)
}

func TestBuyPreviewUsesSmallestProgramAllowance(t *testing.T) {
	for _, tt := range []struct {
		name          string
		change        func(*DailySnapshot, *LatestQuote)
		qty, notional string
	}{
		{"order cap", func(*DailySnapshot, *LatestQuote) {}, "20", "200"},
		{"position headroom", func(s *DailySnapshot, _ *LatestQuote) {
			s.Positions = []Position{{Symbol: "AAPL", Side: "long", Quantity: "95", MarketValue: "950"}}
		}, "5", "50"},
		{"portfolio headroom", func(s *DailySnapshot, _ *LatestQuote) {
			s.Positions = []Position{{Symbol: "MSFT", Side: "long", Quantity: "10", MarketValue: "7950"}}
		}, "5", "50"},
		{"cash reserve", func(s *DailySnapshot, _ *LatestQuote) { s.Account.Cash = "2055" }, "5", "50"},
		{"buying power", func(s *DailySnapshot, _ *LatestQuote) { s.Account.BuyingPower = "35" }, "3", "30"},
		{"no rounded-up division", func(_ *DailySnapshot, q *LatestQuote) { q.AskPrice = "10.000000000000000000001" }, "19", "190.000000000000000000019"},
		{"fresh quote raises existing exposure", func(s *DailySnapshot, _ *LatestQuote) {
			s.Positions = []Position{{Symbol: "AAPL", Side: "long", Quantity: "95", MarketValue: "900"}}
		}, "5", "50"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, q := planningFixture()
			tt.change(&s, &q)
			p := planFor(DecisionBuy, s, q)
			if p.Status != "preview" || p.Proposed == nil || p.Proposed.Quantity != tt.qty || p.Proposed.EstimatedNotional != tt.notional || p.Proposed.ReferenceSide != "ask" || p.Execution != "disabled" {
				t.Fatalf("unexpected plan: %+v", p)
			}
			for _, check := range p.Checks {
				if check.Status == "blocked" {
					t.Fatalf("preview contains failed check: %v", check)
				}
			}
			proposed := p.Proposed
			if decimal.RequireFromString(proposed.ProjectedPositionValue).GreaterThan(decimal.NewFromInt(1000)) || decimal.RequireFromString(proposed.ProjectedInvestedValue).GreaterThan(decimal.NewFromInt(8000)) || decimal.RequireFromString(proposed.ProjectedCash).LessThan(decimal.NewFromInt(2000)) {
				t.Fatal("buy exceeds accepted limits")
			}
		})
	}
}

func TestSellPreviewCannotShortAndMayReduceOverLimitExposure(t *testing.T) {
	for _, tt := range []struct {
		name, held string
		others     []Position
		qty        string
		blocked    bool
	}{
		{"order cap", "100", nil, "22", false},
		{"only owned whole shares", "2.75", nil, "2", false},
		{"full whole-share exit", "2", nil, "2", false},
		{"fractional remainder", "0.75", nil, "", true},
		{"reduce oversized portfolio", "200", []Position{{Symbol: "MSFT", Side: "long", Quantity: "20", MarketValue: "8000"}}, "22", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, q := planningFixture()
			s.Account.Cash = "0"
			s.Account.BuyingPower = "0"
			s.Positions = append(tt.others, Position{Symbol: "AAPL", Side: "long", Quantity: tt.held, MarketValue: decimal.RequireFromString(tt.held).Mul(decimal.NewFromInt(10)).String()})
			p := planFor(DecisionSell, s, q)
			if tt.blocked {
				assertBlocked(t, p, "whole_share_quantity")
				return
			}
			if p.Status != "preview" || p.Proposed == nil || p.Proposed.Quantity != tt.qty || p.Proposed.ReferenceSide != "bid" || p.Proposed.ReferencePrice != "9" {
				t.Fatalf("unexpected sell: %+v", p)
			}
			if decimal.RequireFromString(p.Proposed.Quantity).GreaterThan(decimal.RequireFromString(tt.held)) || decimal.RequireFromString(p.Proposed.EstimatedNotional).GreaterThan(decimal.NewFromInt(200)) {
				t.Fatal("sell exceeds owned shares/order cap")
			}
			remainingValue := decimal.RequireFromString(tt.held).Sub(decimal.RequireFromString(tt.qty)).Mul(decimal.NewFromInt(9))
			if p.Proposed.ProjectedPositionValue != remainingValue.String() {
				t.Fatal("sell projection retains phantom position value")
			}
		})
	}
	s, q := planningFixture()
	assertBlocked(t, planFor(DecisionSell, s, q), "owned_position")
}

func TestPlannerBlocksInvalidOrInsufficientState(t *testing.T) {
	for _, tt := range []struct {
		name, check string
		change      func(*DailySnapshot, *LatestQuote)
	}{
		{"account changed", "account", func(s *DailySnapshot, _ *LatestQuote) { s.Account.ID = "another-account" }},
		{"blocked account", "account", func(s *DailySnapshot, _ *LatestQuote) { s.Account.TradingBlocked = true }},
		{"inactive account", "account", func(s *DailySnapshot, _ *LatestQuote) { s.Account.Status = "INACTIVE" }},
		{"non-USD", "account", func(s *DailySnapshot, _ *LatestQuote) { s.Account.Currency = "EUR" }},
		{"open order", "open_orders", func(s *DailySnapshot, _ *LatestQuote) { s.OpenOrders = []OpenOrder{{Symbol: "MSFT"}} }},
		{"zero equity", "positive_equity", func(s *DailySnapshot, _ *LatestQuote) { s.Account.Equity = "0" }},
		{"missing equity", "equity", func(s *DailySnapshot, _ *LatestQuote) { s.Account.Equity = "" }},
		{"missing cash", "cash", func(s *DailySnapshot, _ *LatestQuote) { s.Account.Cash = "" }},
		{"missing buying power", "buying_power", func(s *DailySnapshot, _ *LatestQuote) { s.Account.BuyingPower = "bad" }},
		{"cash reserve exhausted", "cash_after_reserve", func(s *DailySnapshot, _ *LatestQuote) { s.Account.Cash = "2000" }},
		{"negative cash", "cash_after_reserve", func(s *DailySnapshot, _ *LatestQuote) { s.Account.Cash = "-1" }},
		{"zero buying power", "buying_power_available", func(s *DailySnapshot, _ *LatestQuote) { s.Account.BuyingPower = "0" }},
		{"position cap reached", "position_headroom", func(s *DailySnapshot, _ *LatestQuote) {
			s.Positions = []Position{{Symbol: "AAPL", Side: "long", Quantity: "100", MarketValue: "1000"}}
		}},
		{"portfolio cap reached", "portfolio_headroom", func(s *DailySnapshot, _ *LatestQuote) {
			s.Positions = []Position{{Symbol: "MSFT", Side: "long", Quantity: "1", MarketValue: "8000"}}
		}},
		{"below one affordable share", "whole_share_quantity", func(_ *DailySnapshot, q *LatestQuote) { q.AskPrice = "201" }},
		{"short portfolio", "long_positions", func(s *DailySnapshot, _ *LatestQuote) {
			s.Positions = []Position{{Symbol: "MSFT", Side: "short", Quantity: "-1", MarketValue: "-100"}}
		}},
		{"duplicate positions", "long_positions", func(s *DailySnapshot, _ *LatestQuote) {
			p := Position{Symbol: "AAPL", Side: "long", Quantity: "1", MarketValue: "10"}
			s.Positions = []Position{p, p}
		}},
		{"missing position quantity", "position_quantity", func(s *DailySnapshot, _ *LatestQuote) {
			s.Positions = []Position{{Symbol: "AAPL", Side: "long", MarketValue: "10"}}
		}},
		{"missing market value", "position_value", func(s *DailySnapshot, _ *LatestQuote) {
			s.Positions = []Position{{Symbol: "AAPL", Side: "long", Quantity: "1"}}
		}},
		{"negative market value", "position_values", func(s *DailySnapshot, _ *LatestQuote) {
			s.Positions = []Position{{Symbol: "AAPL", Side: "long", Quantity: "1", MarketValue: "-1"}}
		}},
		{"wrong quote symbol", "quote_identity", func(_ *DailySnapshot, q *LatestQuote) { q.Symbol = "MSFT" }},
		{"wrong quote feed", "quote_identity", func(_ *DailySnapshot, q *LatestQuote) { q.Feed = "sip" }},
		{"stale quote", "quote_age", func(_ *DailySnapshot, q *LatestQuote) {
			q.Timestamp = testClockTime.Add(-60*time.Second - time.Nanosecond)
		}},
		{"future quote", "quote_age", func(_ *DailySnapshot, q *LatestQuote) { q.Timestamp = testClockTime.Add(time.Nanosecond) }},
		{"missing quote time", "quote_age", func(_ *DailySnapshot, q *LatestQuote) { q.Timestamp = time.Time{} }},
		{"missing reference clock", "quote_age", func(s *DailySnapshot, _ *LatestQuote) { s.Clock.Timestamp = time.Time{} }},
		{"zero bid", "quote_prices", func(_ *DailySnapshot, q *LatestQuote) { q.BidPrice = "0" }},
		{"negative ask", "quote_prices", func(_ *DailySnapshot, q *LatestQuote) { q.AskPrice = "-1" }},
		{"invalid ask", "ask_price", func(_ *DailySnapshot, q *LatestQuote) { q.AskPrice = "NaN" }},
		{"crossed quote", "quote_prices", func(_ *DailySnapshot, q *LatestQuote) { q.BidPrice = "11" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, q := planningFixture()
			tt.change(&s, &q)
			assertBlocked(t, planFor(DecisionBuy, s, q), tt.check)
		})
	}
	s, q := planningFixture()
	q.Timestamp = testClockTime.Add(-60 * time.Second)
	if p := planFor(DecisionBuy, s, q); p.Status != "preview" {
		t.Fatal("quote at age boundary rejected")
	}
	assertBlocked(t, planOrder(Decision{Action: DecisionBuy, Symbol: "MSFT"}, "AAPL", "paper-account", s, q, defaultPlannerLimits()), "recommendation")
}

type policyFunc func(context.Context, DailySnapshot) (Decision, error)

func (f policyFunc) Decide(ctx context.Context, s DailySnapshot) (Decision, error) { return f(ctx, s) }

func outputPlan(t *testing.T, output string) OrderPlan {
	t.Helper()
	parts := strings.SplitN(output, "\n", 2)
	if len(parts) != 2 {
		t.Fatal("missing structured report")
	}
	var report struct {
		Plan OrderPlan `json:"order_plan"`
	}
	if err := json.Unmarshal([]byte(parts[1]), &report); err != nil {
		t.Fatal(err)
	}
	return report.Plan
}

func TestCyclePlannerRefreshesAfterRecommendation(t *testing.T) {
	for _, kind := range []string{"new open order", "cash changed", "position changed", "new clock"} {
		t.Run(kind, func(t *testing.T) {
			s, q := planningFixture()
			b := &fakeBroker{account: s.Account, clock: s.Clock}
			data := &fakeMarketData{quote: q}
			policy := policyFunc(func(_ context.Context, original DailySnapshot) (Decision, error) {
				if len(b.calls) != 4 || original.Account.Cash != "10000" {
					t.Fatal("unexpected recommendation snapshot")
				}
				switch kind {
				case "new open order":
					b.openOrders = []OpenOrder{{ID: "new", Symbol: "MSFT"}}
				case "cash changed":
					b.account.Cash = "2000"
				case "position changed":
					b.positions = []Position{{Symbol: "AAPL", Side: "long", Quantity: "100", MarketValue: "1000"}}
				case "new clock":
					b.clock.Timestamp = testClockTime.Add(2 * time.Second)
					data.quote.Timestamp = testClockTime.Add(time.Second)
				}
				return Decision{Action: DecisionBuy, Symbol: "AAPL", Rationale: "test recommendation"}, nil
			})
			var out bytes.Buffer
			if err := run(context.Background(), &out, b, policy, data, "AAPL", defaultPlannerLimits()); err != nil {
				t.Fatal(err)
			}
			p := outputPlan(t, out.String())
			if kind == "new clock" {
				if p.Status != "preview" || p.ReferenceTime == nil || !p.ReferenceTime.Equal(b.clock.Timestamp) {
					t.Fatal("fresh clock not used")
				}
			} else {
				if p.Status != "blocked" || p.Proposed != nil {
					t.Fatal("planned against old account state")
				}
			}
			want := []string{"clock", "account", "positions", "open_orders", "account", "positions", "open_orders"}
			if kind != "new open order" {
				want = append(want, "clock")
			}
			if !reflect.DeepEqual(b.calls, want) {
				t.Fatalf("calls=%v want=%v", b.calls, want)
			}
			if kind == "new open order" && data.quoteCalls != 0 {
				t.Fatal("unnecessary quote for blocked orders")
			}
		})
	}
}

func TestPlannerRefreshFailuresAreReportedAlongsideRecommendation(t *testing.T) {
	for _, operation := range []string{"account", "positions", "open_orders", "clock", "quote"} {
		t.Run(operation, func(t *testing.T) {
			s, q := planningFixture()
			b := &fakeBroker{account: s.Account, clock: s.Clock}
			data := &fakeMarketData{quote: q}
			failure := errors.New("temporary data failure")
			policy := policyFunc(func(context.Context, DailySnapshot) (Decision, error) {
				if operation == "quote" {
					data.quoteFailure = failure
				} else {
					b.failCall = operation
					b.failure = failure
				}
				return Decision{Action: DecisionBuy, Symbol: "AAPL", RecordID: "test-record"}, nil
			})
			var out bytes.Buffer
			err := run(context.Background(), &out, b, policy, data, "AAPL", defaultPlannerLimits())
			if !errors.Is(err, failure) || !strings.Contains(out.String(), `"record_id":"test-record"`) {
				t.Fatalf("error=%v output=%s", err, out.String())
			}
			if p := outputPlan(t, out.String()); p.Status != "unavailable" || p.Proposed != nil || p.Execution != "disabled" {
				t.Fatal("failed refresh produced a preview")
			}
		})
	}
}

func TestHoldPlannerNeedsNoQuoteOrRefresh(t *testing.T) {
	b := &fakeBroker{}
	data := &fakeMarketData{}
	var out bytes.Buffer
	if err := run(context.Background(), &out, b, HoldPolicy{}, data, "AAPL", defaultPlannerLimits()); err != nil {
		t.Fatal(err)
	}
	if len(b.calls) != 4 || data.quoteCalls != 0 || outputPlan(t, out.String()).Status != "no_trade" {
		t.Fatal("hold unexpectedly requested sizing data")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, err := refreshOrderPlan(ctx, b, data, Decision{Action: DecisionBuy, Symbol: "AAPL"}, "AAPL", "paper-account", defaultPlannerLimits())
	if !errors.Is(err, context.Canceled) || p.Status != "unavailable" || len(b.calls) != 4 || data.quoteCalls != 0 {
		t.Fatal("canceled planner called APIs")
	}
}
