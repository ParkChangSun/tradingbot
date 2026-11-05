package main

import (
	"context"
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

// These are operator-owned preview limits. The AI cannot edit them or size orders.
// Long-only, cash-funded, whole-share planning and the open-order guard are fixed.
type PlannerLimits struct {
	MaxPositionWeight  decimal.Decimal `json:"max_position_weight"`
	MaxInvestedWeight  decimal.Decimal `json:"max_invested_weight"`
	MaxOrderWeight     decimal.Decimal `json:"max_order_weight"`
	QuoteMaxAgeSeconds int64           `json:"quote_max_age_seconds"`
}

func defaultPlannerLimits() PlannerLimits {
	return PlannerLimits{MaxPositionWeight: decimal.RequireFromString("0.10"), MaxInvestedWeight: decimal.RequireFromString("0.80"), MaxOrderWeight: decimal.RequireFromString("0.02"), QuoteMaxAgeSeconds: 60}
}

func (l PlannerLimits) validate() error {
	one := decimal.NewFromInt(1)
	for _, v := range []decimal.Decimal{l.MaxPositionWeight, l.MaxInvestedWeight, l.MaxOrderWeight} {
		if !v.IsPositive() || v.GreaterThan(one) {
			return fmt.Errorf("planner weights must be greater than zero and at most one")
		}
	}
	if l.MaxOrderWeight.GreaterThan(l.MaxPositionWeight) || l.MaxPositionWeight.GreaterThan(l.MaxInvestedWeight) {
		return fmt.Errorf("planner weights must satisfy order <= position <= invested")
	}
	if l.QuoteMaxAgeSeconds <= 0 || l.QuoteMaxAgeSeconds > int64((1<<63-1)/time.Second) {
		return fmt.Errorf("planner quote age must be a positive duration in seconds")
	}
	return nil
}

type PlanCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // passed, blocked, not_applicable.
	Detail string `json:"detail"`
}

type ProposedOrder struct {
	Symbol                 string         `json:"symbol"`
	Side                   DecisionAction `json:"side"`
	Quantity               string         `json:"quantity"`
	ReferencePrice         string         `json:"reference_price"`
	ReferenceSide          string         `json:"reference_side"`
	EstimatedNotional      string         `json:"estimated_notional"`
	ProjectedPositionValue string         `json:"projected_position_value"`
	ProjectedInvestedValue string         `json:"projected_invested_value"`
	ProjectedCash          string         `json:"projected_cash"`
}

type OrderPlan struct {
	Status          string         `json:"status"`    // preview, blocked, no_trade, unavailable, not_requested.
	Execution       string         `json:"execution"` // Always disabled; preview is not execution approval.
	Limits          PlannerLimits  `json:"limits"`
	LongOnly        bool           `json:"long_only"`
	CashFunded      bool           `json:"cash_funded"`
	WholeShares     bool           `json:"whole_shares"`
	BlockOpenOrders bool           `json:"block_open_orders"`
	ReferenceTime   *time.Time     `json:"reference_time,omitempty"`
	MarketOpen      *bool          `json:"market_open,omitempty"`
	Quote           *LatestQuote   `json:"quote,omitempty"`
	Checks          []PlanCheck    `json:"checks"`
	Proposed        *ProposedOrder `json:"proposed_order,omitempty"`
}

func emptyOrderPlan(limits PlannerLimits) OrderPlan {
	return OrderPlan{Status: "blocked", Execution: "disabled", Limits: limits, LongOnly: true, CashFunded: true, WholeShares: true, BlockOpenOrders: true, Checks: []PlanCheck{}}
}

func (p *OrderPlan) check(name string, passed bool, detail string) bool {
	status := "passed"
	if !passed {
		status = "blocked"
	}
	p.Checks = append(p.Checks, PlanCheck{Name: name, Status: status, Detail: detail})
	return passed
}

// Refresh after the AI returns. Never size against its potentially older snapshot.
// Quote collection is followed by the broker clock to measure age at completion.
// The sequential reads are not an atomic account snapshot or an execution gate.
func refreshOrderPlan(ctx context.Context, broker Broker, quotes QuoteSource, d Decision, symbol, accountID string, limits PlannerLimits) (OrderPlan, error) {
	plan := emptyOrderPlan(limits)
	if err := limits.validate(); err != nil {
		return plan, err
	}
	if err := ctx.Err(); err != nil {
		plan.Status = "unavailable"
		plan.check("refresh", false, "planning was canceled")
		return plan, err
	}
	if d.Action == DecisionHold {
		plan.Status = "no_trade"
		plan.Checks = append(plan.Checks, PlanCheck{"recommendation", "not_applicable", "hold/no trade requires no order or quote"})
		return plan, nil
	}
	if d.Action != DecisionBuy && d.Action != DecisionSell {
		plan.Status = "not_requested"
		return plan, nil
	}
	if d.Symbol != symbol {
		plan.check("symbol", false, "recommendation must match configured symbol")
		return plan, nil
	}
	fail := func(operation string, err error) (OrderPlan, error) {
		plan.Status = "unavailable"
		plan.check("refresh", false, operation+" failed; no preview is available")
		return plan, fmt.Errorf("%s: %w", operation, err)
	}
	account, err := broker.Account(ctx)
	if err != nil {
		return fail("refresh account", err)
	}
	positions, err := broker.Positions(ctx)
	if err != nil {
		return fail("refresh positions", err)
	}
	orders, err := broker.OpenOrders(ctx)
	if err != nil {
		return fail("refresh open orders", err)
	}
	snapshot := DailySnapshot{Account: account, Positions: positions, OpenOrders: orders}
	// No quote is needed to report these definitive blockers.
	if len(orders) > 0 || account.TradingBlocked || account.ID != accountID || account.Status != "ACTIVE" || account.Currency != "USD" {
		return planOrder(d, symbol, accountID, snapshot, LatestQuote{}, limits), nil
	}
	if quotes == nil {
		return fail("get latest quote", fmt.Errorf("quote source is required"))
	}
	quote, err := quotes.LatestQuote(ctx, symbol)
	if err != nil {
		return fail("get latest quote", err)
	}
	clock, err := broker.Clock(ctx)
	if err != nil {
		return fail("refresh market clock", err)
	}
	snapshot.Clock = clock
	if err := ctx.Err(); err != nil {
		return fail("refresh planning snapshot", err)
	}
	return planOrder(d, symbol, accountID, snapshot, quote, limits), nil
}

// planOrder produces only an indicative quantity, never an order type, executable
// price, time-in-force, or permission to submit. All arithmetic is decimal.
func planOrder(d Decision, symbol, accountID string, snapshot DailySnapshot, quote LatestQuote, limits PlannerLimits) OrderPlan {
	plan := emptyOrderPlan(limits)
	if !snapshot.Clock.Timestamp.IsZero() {
		plan.ReferenceTime = &snapshot.Clock.Timestamp
		plan.MarketOpen = &snapshot.Clock.IsOpen
	}
	if !plan.check("limits", limits.validate() == nil, "operator weights must satisfy 0 < order <= position <= invested <= 1; quote age must be positive") {
		return plan
	}
	if d.Action == DecisionHold {
		plan.Status = "no_trade"
		plan.Checks = append(plan.Checks, PlanCheck{"recommendation", "not_applicable", "hold/no trade requires no order"})
		return plan
	}
	if !plan.check("recommendation", (d.Action == DecisionBuy || d.Action == DecisionSell) && d.Symbol == symbol && symbol != "", "buy/sell must match the configured symbol") {
		return plan
	}
	a := snapshot.Account
	if !plan.check("account", a.ID != "" && a.ID == accountID && a.Currency == "USD" && a.Status == "ACTIVE" && !a.TradingBlocked, "refreshed account must match the recommendation's active, unblocked USD account") {
		return plan
	}
	if !plan.check("open_orders", len(snapshot.OpenOrders) == 0, "any existing open order blocks planning") {
		return plan
	}
	parse := func(name, value string) (decimal.Decimal, bool) {
		v, err := decimal.NewFromString(value)
		ok := plan.check(name, err == nil, "required account/position value must be a valid decimal")
		return v, ok
	}
	equity, ok := parse("equity", a.Equity)
	if !ok {
		return plan
	}
	if !plan.check("positive_equity", equity.IsPositive(), "equity must be positive") {
		return plan
	}
	cash, ok := parse("cash", a.Cash)
	if !ok {
		return plan
	}
	buyingPower, ok := parse("buying_power", a.BuyingPower)
	if !ok {
		return plan
	}
	held := decimal.Zero
	targetValue := decimal.Zero
	invested := decimal.Zero
	seen := map[string]bool{}
	for _, p := range snapshot.Positions {
		if !plan.check("long_positions", p.Symbol != "" && !seen[p.Symbol] && p.Side == "long", "positions must have unique symbols and be long; short portfolios are unsupported") {
			return plan
		}
		seen[p.Symbol] = true
		qty, ok := parse("position_quantity", p.Quantity)
		if !ok {
			return plan
		}
		value, ok := parse("position_value", p.MarketValue)
		if !ok {
			return plan
		}
		if !plan.check("position_values", qty.IsPositive() && !value.IsNegative(), "long quantities must be positive and market values nonnegative") {
			return plan
		}
		invested = invested.Add(value)
		if p.Symbol == symbol {
			held = qty
			targetValue = value
		}
	}
	plan.Quote = &quote
	if !plan.check("quote_identity", quote.Symbol == symbol && quote.Feed == dailyHistoryFeed, "quote must match configured symbol and IEX feed") {
		return plan
	}
	if !plan.check("quote_age", !snapshot.Clock.Timestamp.IsZero() && !quote.Timestamp.IsZero() && !quote.Timestamp.After(snapshot.Clock.Timestamp) && snapshot.Clock.Timestamp.Sub(quote.Timestamp) <= time.Duration(limits.QuoteMaxAgeSeconds)*time.Second, "quote must not be future-dated or older than configured maximum age") {
		return plan
	}
	bid, ok := parse("bid_price", quote.BidPrice)
	if !ok {
		return plan
	}
	ask, ok := parse("ask_price", quote.AskPrice)
	if !ok {
		return plan
	}
	if !plan.check("quote_prices", bid.IsPositive() && ask.IsPositive() && !bid.GreaterThan(ask), "bid and ask must be positive and not crossed") {
		return plan
	}
	price := ask
	referenceSide := "ask"
	if d.Action == DecisionSell {
		price = bid
		referenceSide = "bid"
	}
	// Do not understate existing target exposure when quote and broker mark differ.
	markedValue := decimal.Max(targetValue, held.Mul(price))
	invested = invested.Add(markedValue.Sub(targetValue))
	targetValue = markedValue
	orderCap := equity.Mul(limits.MaxOrderWeight)
	capacity := orderCap
	if d.Action == DecisionBuy {
		reserve := equity.Mul(decimal.NewFromInt(1).Sub(limits.MaxInvestedWeight))
		allowances := []struct {
			name   string
			amount decimal.Decimal
		}{
			{"order_cap", orderCap},
			{"position_headroom", equity.Mul(limits.MaxPositionWeight).Sub(targetValue)},
			{"portfolio_headroom", equity.Mul(limits.MaxInvestedWeight).Sub(invested)},
			{"cash_after_reserve", cash.Sub(reserve)},
			{"buying_power_available", buyingPower},
		}
		passed := true
		for _, allowance := range allowances {
			if !plan.check(allowance.name, allowance.amount.IsPositive(), "available USD: "+allowance.amount.String()) {
				passed = false
			}
			capacity = decimal.Min(capacity, allowance.amount)
		}
		if !passed {
			return plan
		}
	} else {
		if !plan.check("owned_position", held.IsPositive(), "sell can only reduce an existing long position") {
			return plan
		}
		plan.Checks = append(plan.Checks, PlanCheck{"exposure_caps", "not_applicable", "risk-reducing sells may leave existing exposure above caps; no increase is permitted"})
	}
	// QuoRem at precision zero truncates exactly. Div().Floor() could round a
	// just-below-integer quotient upward at the decimal library's division precision.
	quantity, _ := capacity.QuoRem(price, 0)
	if d.Action == DecisionSell {
		quantity = decimal.Min(quantity, held.Truncate(0))
	}
	if !plan.check("whole_share_quantity", quantity.IsPositive(), "rounded-down quantity must be at least one owned/affordable whole share") {
		return plan
	}
	notional := quantity.Mul(price)
	if !plan.check("order_size", notional.LessThanOrEqual(orderCap), "estimated notional must not exceed the per-cycle order cap") {
		return plan
	}
	projectedTarget := targetValue.Add(notional)
	projectedInvested := invested.Add(notional)
	projectedCash := cash.Sub(notional)
	if d.Action == DecisionSell {
		if !plan.check("no_short_sale", quantity.LessThanOrEqual(held), "sell quantity must not exceed current holdings") {
			return plan
		}
		// Remaining shares are valued at the fresh bid. Subtracting sale proceeds
		// from an older broker mark could leave phantom value after a full exit.
		projectedTarget = held.Sub(quantity).Mul(price)
		projectedInvested = invested.Sub(targetValue).Add(projectedTarget)
		projectedCash = cash.Add(notional)
	}
	plan.Status = "preview"
	plan.Proposed = &ProposedOrder{Symbol: symbol, Side: d.Action, Quantity: quantity.String(), ReferencePrice: price.String(), ReferenceSide: referenceSide, EstimatedNotional: notional.String(), ProjectedPositionValue: projectedTarget.String(), ProjectedInvestedValue: projectedInvested.String(), ProjectedCash: projectedCash.String()}
	return plan
}
