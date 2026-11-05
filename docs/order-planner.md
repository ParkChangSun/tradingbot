# Dry-run order planner

The planner converts the AI's directional recommendation into a proposed
whole-share quantity, using operator-owned constraints. It never submits orders.
`preview` is an indicative calculation, not execution approval, an order type,
a limit price, or a guarantee about fills or losses. Alpaca remains Paper-only.

## Selected initial policy

| Setting | Default | Operator variable |
| --- | --- | --- |
| Maximum position weight | 10% of current equity | `PLANNER_MAX_POSITION_WEIGHT=0.10` |
| Maximum total invested exposure | 80% of current equity | `PLANNER_MAX_INVESTED_WEIGHT=0.80` |
| Maximum estimated order value per cycle | 2% of current equity | `PLANNER_MAX_ORDER_WEIGHT=0.02` |
| Maximum quote age | 60 seconds | `PLANNER_QUOTE_MAX_AGE_SECONDS=60` |
| Trading permissions | Long-only, cash-funded | Fixed; no shorting or borrowing for buys |
| Quantities | Whole shares, rounded down | Fixed; below one share is blocked |
| Open orders | Any order blocks planning | Fixed, regardless of symbol |

Weights are decimal fractions, not percentage integers. Configuration requires
`0 < order <= position <= invested <= 1` and a positive quote-age duration.
The cash reserve equals `1 - maximum invested weight` (20% by default).
Only the operator can change configuration. Model output cannot change sizing,
limits, quote freshness, API budget, or execution permissions.

The quote-age default is a technical freshness requirement, not a spread or
slippage limit. No loss breakers, automatic exits, daily turnover cap, maximum
holding period, or recommendation expiry rule are introduced.

## Refreshed data and price source

After a buy/sell recommendation is returned, the program fetches account,
positions, and open orders again. A changed account identity, inactive/non-USD
account, trading block, or any open order blocks planning before a quote request.
Other required decimal fields and position quantities/values must be usable;
short portfolios and duplicate position symbols are unsupported and blocked.

For an otherwise eligible account, it fetches a latest IEX quote with explicit
USD currency through the existing pinned SDK, then reads the broker clock to
measure quote age at completion. Quote requests reuse explicit authentication,
the 15-second HTTP timeout, caller cancellation, bounded decoded bodies, and
disabled SDK retries. There is no feed/endpoint fallback or new subscription.

The [Alpaca latest-quotes endpoint](https://docs.alpaca.markets/us/reference/stocklatestquotes-1)
provides bid/ask prices. The planner uses the ask for buys and bid for sells;
these are IEX prices, not consolidated NBBO prices or executable order prices.
It rejects missing, future-dated, stale, nonpositive, crossed, or wrong-symbol/
wrong-feed quotes. Exactly the configured maximum age is accepted. It never
uses daily-bar prices for sizing. Closed-market analysis remains allowed, but
an old latest quote will block planning; no trading-hours policy is enabled.

The reads are sequential and not an atomic snapshot. Other holdings retain their
refreshed broker market values; the planner does not retrieve quotes for every
holding or assume that equity/position marks and the quote have identical times.
Hold/no-trade recommendations need no refresh or quote because no order is sized.

## Sizing

All sizing uses decimal arithmetic. The quote SDK exposes floats; the adapter
converts their shortest decimal representations to strings before planner
arithmetic. Daily historical analytical floats are never used for quantities.

Let `E` be refreshed positive equity and `P` the ask for a buy. Existing target
exposure is the larger of its broker market value and owned quantity times `P`.
Total invested exposure sums all long holdings, with that conservative adjustment
for the configured symbol. Thus a higher fresh ask cannot silently understate
the target's existing exposure.

The buy allowance in USD is the smallest of:

- `E * maximum order weight`;
- `E * maximum position weight - existing target exposure`;
- `E * maximum invested weight - total invested exposure`;
- `cash - E * (1 - maximum invested weight)`;
- Broker-reported buying power.

Each allowance must be positive. Quantity is exact integer truncation of
`allowance / ask`. Zero shares blocks planning. This preserves the cash reserve
and prevents margin buying even if reported buying power exceeds cash.

For a sell, quantity is the smaller of the exact whole-share truncations of
`E * maximum order weight / bid` and current owned shares. Selling an unowned
symbol is blocked. Fractional holdings can leave a fractional remainder.
Estimated proceeds are quantity times bid; remaining target shares are valued
at that bid for the projection, so a full exit leaves zero position value.

Position and portfolio exposure caps block increases. Risk-reducing sells are
allowed to leave an already oversized portfolio above those caps, rather than
preventing incremental reductions. The 2% order cap still applies. Existing
negative cash does not authorize buying; selling a long position may reduce it.
This is not an automatic liquidation rule and never overrides a hold.

Every invocation is a new cycle: the order cap is not a daily turnover limit.
Estimated notional/projections omit fees, slippage, market impact, and price moves.

## Reports and remaining execution work

The CLI keeps the AI recommendation/validation report separate from `order_plan`:

- `preview`: a proposed quantity/reference price/notional is available.
- `blocked`: a check failed, or fewer than one whole share can be planned.
- `no_trade`: the policy held; no order or further data reads were needed.
- `unavailable`: data retrieval failed; the CLI prints the report and also returns
  an error rather than silently treating the failure as hold.
- `not_requested`: no recommendation was produced, such as a trading-blocked cycle.

Checks identify what passed or blocked the preview. Checks stop at definitive
blockers; omitted checks have not passed. Every report includes effective limits
and `execution=disabled`. The summary uses `risk_limits=preview_configured` to
distinguish configured preview constraints from a complete execution policy.
The summary's account counts describe the original recommendation snapshot;
the order-plan time/quote/projections describe the later refresh.

Recommendation journals remain unchanged in purpose: they store AI context and
recommendations, not order plans, submissions, fills, or portfolio performance.
Older records remain usable as prior recommendations. Plan reports are CLI output.

Order submission requires a separate implementation and decisions on tradable
assets, order type/time-in-force, market hours, price/spread controls, validity,
loss breakers/exits, and pending/partial orders. It must revalidate current state,
handle duplicate submissions and uncertain outcomes, and enforce Paper-only
access. None of that is implied by a successful dry-run preview.
