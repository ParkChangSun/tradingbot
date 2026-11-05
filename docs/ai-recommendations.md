# AI recommendation cycle

## Objective and boundary

Prioritize limiting losses while seeking opportunities for returns. This is an
objective, not a guarantee against losses. The AI may recommend no trade. The
initial integration is a read-only experiment, not a validated trading strategy.

The flow is account/market snapshot plus previous recommendations, followed by
one AI recommendation, program validation, and a journal/CLI report. The model
cannot submit orders, change configuration, raise its budget, choose tools, or
select another model. [Program-owned preview limits](order-planner.md) are now
configured for a dry-run planner; they do not enable execution. `valid` describes
the recommendation contract, not trade eligibility.

The broker continues to use Alpaca Paper Trading only. Before any future order
submission, a separate program-owned gate must refresh account information,
positions, open orders, and appropriate prices; calculate quantities; account for
pending exposure; and check projected orders against selected risk limits.

## Input and output

The AI receives the existing 20 oldest-first daily bars for `TRADING_SYMBOL`,
their feed/adjustment, and the broker reference time. The existing New York
cutoff excludes today's bar. The history window is an initial integration input,
not evidence that it suits every holding period. News, live quotes, histories
for other holdings, and exchange-calendar freshness checks are not added.

Portfolio context includes all positions (side, quantity, average entry price,
market value, and signed portfolio weight), account equity, cash, buying power,
and open orders. Weights are decimal market value divided by positive account
equity, rounded to 12 fractional places. Missing or invalid required numeric
values, nonpositive equity, and non-USD accounts fail analysis explicitly. A
full 500-order page also fails AI analysis because the current adapter reads
only one page and cannot prove completeness. Account valuations and historical
prices may refer to different times; no live execution price is inferred.

Only the configured symbol receives a recommendation. For it and every current
holding, the context includes the latest validated recommendation for that Paper
account/symbol, or explicit null when none exists. Thus existing positions that
predate this feature have no invented prior thesis. Other holdings' previous
recommendations are context, not permission to trade them.

The structured recommendation contains:

- `action`: `buy`, `sell`, or `hold` (including no trade).
- `symbol` and a concise `rationale`.
- `expected_holding_sessions`: a positive integer, or null only for hold with no
  position in the configured symbol.
- Nonempty `reassessment_conditions` and a `next_review_date` in YYYY-MM-DD,
  strictly after the reference New York date.
- `previous_recommendation_id`: the exact supplied ID, or null for a first
  recommendation.
- `change_explanation`: required if action, rationale, holding period,
  reassessment conditions, or review date differs from the previous recommendation.

The program validates required fields, types, allowed action, configured symbol,
holding-period applicability, review date, previous ID, and change explanation.
Unknown/duplicate fields, malformed responses, refusals, and incomplete responses
are failures, not silent hold recommendations. It cannot prove that a rationale
is correct or that an explanation is persuasive.

Review dates and conditions are advisory. There is no scheduler, continuous
condition evaluation, automatic stop, or holding-period enforcement. The AI does
not size orders; the independent planner calculates an indicative whole-share
quantity from refreshed account state and a fresh quote after the recommendation.
Open orders do not stop AI analysis, but block planning; a trading-blocked account
still stops the daily cycle before policy evaluation.

## Model and API spending

The initial model is pinned to `gpt-5.4-mini-2026-03-17` with low reasoning effort
and strict structured output through the Responses API. It is an inexpensive
integration baseline; its decision quality needs separate evaluation. The
[official model documentation](https://developers.openai.com/api/docs/models/gpt-5.4-mini)
lists standard prices of $0.75/million input tokens and $4.50/million output
tokens. The reviewed price schedule is encoded with this model; unknown model
configuration is rejected rather than assigned a guessed price.

The default per-cycle budget is $0.05. An operator may change
`OPENAI_CYCLE_BUDGET_USD` to a positive dollar amount representable in nanodollars.
This cannot be changed through model output. Input/output ceilings remain fixed
at 12,000/6,000 tokens; increasing the dollar budget does not expand them.

Before generation, the program counts every UTF-8 byte of the entire serialized
request, including instructions, schema, all required context, and previous
recommendations, plus a 1,024-token fixed framing allowance. This deliberately
overcounts text under byte-level tokenization rather than using a characters/4
estimate. The allowance applies to this fixed two-message text/schema request;
changing the model or request format requires reviewing the bound and prices.
Oversized input fails without dropping context or making a generation call.

The maximum charge is reserved with integer nanodollar arithmetic using uncached
input rates and the full output ceiling. At the ceilings this is $0.036, below
the default $0.05 budget. `max_output_tokens=6000` includes reasoning and visible
output; see the [official reasoning guide](https://developers.openai.com/api/docs/guides/reasoning).
An exhausted output allowance is reported as incomplete, with no repair call.

Each eligible cycle makes at most one generation attempt. The adapter uses a
fixed public endpoint and standard service tier, disables tools, remote response
storage, automatic truncation, and redirects, and has no retry or fallback loop.
The OpenAI HTTP timeout is capped at 60 seconds; Alpaca retains its existing
15-second timeout. SDK environment endpoint overrides are not used for OpenAI.

Usage, when available, records actual input/output counts and an uncached-rate
cost upper bound (cached input may cost less). Missing usage remains unknown,
not zero. Timeouts, cancellations, and failed requests may already be billed;
the reserved amount is not automatically refunded or retried. Reported model,
tier, and usage outside the reviewed limits invalidate the result.

This bounds program-authorized usage under the reviewed pricing schedule, not
the provider invoice if pricing changes. New invocations are new cycles with new
budgets; there is no daily/monthly spending cap or automatic repeat scheduling.

## Local recommendation history

Records live in ignored `.tradingbot-recommendations/`, under hashed account
directories, with random record IDs. Directories use 0700 and records 0600.
Each recommendation record stores the input context, previous recommendation summaries, model
and prompt versions, recommendation (when decoded), validation report, reserved
budget, and known usage or failure. Account/portfolio records are sensitive;
credentials are never stored in them. Do not commit or publish these files.
The separate refreshed order-plan report is printed to the CLI, not stored in
this recommendation journal. Existing `loss-aware-v1` records remain readable;
their historical "limits unset" messages are preserved. New recommendations use
`loss-aware-v2`, which describes program-owned preview sizing.

Before the network request, an atomic journal write records `reserved` status
with unknown usage. Afterward, another atomic write marks it `validated`,
`invalid`, or `failed`. Files are synced and renamed within the account directory.
A crash can leave a reserved record: treat it as potentially billed, not executed.
Temporary `.record-*` files are never interpreted as recommendations.

Only validated records are reused. Failures do not replace the previous valid
recommendation. Missing history is distinct from corrupt/unreadable history;
corruption blocks analysis. Writes that fail block the request or successful
return, depending on when the failure happens. Records contain recommendations,
not orders, fills, or proof that the portfolio followed the AI's advice.

Run cycles serially for coherent prior-recommendation context. There is no
cross-process cycle lock, retention policy, journal migration, or database.

## Unresolved execution limits

The initial preview choices are long-only/cash-funded, 10% maximum position
weight, 80% maximum invested exposure, 2% maximum order value per cycle, whole
shares rounded down, and a block on any open order. See the planner document
for formulas and operator configuration. These are testing limits, not evidence
of optimal risk management or a guarantee against loss.

Other execution decisions remain unresolved. The following table retains the
earlier illustrative alternatives for future review; only the initial preview
choices above have been selected, and order submission remains disabled:

| Decision | Options | Tradeoff |
| --- | --- | --- |
| Shorting/leverage | Cash-funded long-only, or permit margin/shorting | Broader permissions add exposure and checks |
| Maximum position weight | 5%, 10%, or 20% of equity | Lower concentration versus less deployment |
| Total invested exposure | 50%, 80%, or 100%; reserve the rest as cash | Less deployment versus more flexibility |
| Maximum order size | 1%, 2%, or 5% of equity | Smaller mistakes versus slower repositioning |
| Daily turnover | 5%, 10%, or 20%, optionally an order-count cap | Less churn versus slower changes |
| Loss circuit breakers | 1%/2% daily loss; 5%/10% drawdown | Earlier pauses versus more interruptions |
| Position exits | Review-only, fixed 3%/5%/10% threshold, or volatility-based | No automatic control versus simple/adaptive rules |
| Planned loss per trade | 0.25%, 0.5%, or 1% of equity | Requires explicit exit and sizing rules; actual loss can exceed plan |
| Validity/holding bounds | Same-session or next-session validity; optional maximum holding period | Fresher context versus more reassessment |
| Execution conditions | Regular hours or broader sessions; price-deviation/spread limits | Simpler execution versus availability |

Also decide eligible assets, fractional quantities, order type/time-in-force,
handling of pending/partial orders, and whether oversized proposals are rejected
or explicitly resized. Loss breakers require a defined equity baseline,
realized/unrealized treatment, deposits handling, restart persistence, and reset
authority. Pausing new exposure is separate from liquidation; neither is enabled.

These decisions are prerequisites to an order-submission milestone. A later
evaluation milestone must assess recommendation behavior and trading performance;
the integration tests make no claim of profitability or loss prevention.
