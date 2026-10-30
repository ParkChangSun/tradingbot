# Trading Bot

Small Go CLI for exploring a US stock trading bot.

## Run

Copy the example environment file, add your Alpaca Paper Trading credentials,
and replace `your-stock-symbol` with the US stock symbol you want to inspect:

```bash
cp .env.example .env
```

Then run one daily cycle:

```bash
go run .
```

The bot loads `.env` from the current working directory when the file exists.
Variables already set in the process environment take precedence over values in
`.env`.

You can continue to set the required variables directly instead:

```bash
export APCA_API_KEY_ID="your-paper-api-key"
export APCA_API_SECRET_KEY="your-paper-api-secret"
export TRADING_SYMBOL="your-stock-symbol"
go run .
```

The bot always uses the Alpaca Paper Trading API at
`https://paper-api.alpaca.markets`. Credentials and the single symbol are configurable.
`TRADING_SYMBOL` is required, trimmed, and normalized to uppercase.

The Alpaca adapter uses the official `alpaca-trade-api-go/v3` SDK, pinned to
v3.11.0. It explicitly selects the paper URL and API-key authentication rather
than inheriting SDK base-URL or OAuth environment settings. Each request retains
the caller's cancellation, the CLI's 15-second HTTP timeout, and a 4 MiB decoded
response limit. Response bodies are closed after bounded buffering, including
gzip responses. Automatic SDK retries are disabled, so a rate-limit response fails
the cycle immediately. SDK API errors remain available through `errors.As`,
with response request IDs included in error messages when present.

SDK decimal values are converted to normalized strings in snapshots (for
example, `"450.00"` becomes `"450"`). Nullable position market values and order
quantities remain empty strings when unavailable. Open-order retrieval remains
limited to one page of up to 500 orders.

## Daily cycle

Each invocation runs a single cycle in both open and closed market hours. It
reads the US market clock, account, positions, and open orders. A trading-blocked
account or any existing open order stops the cycle before history retrieval and
policy evaluation. Otherwise it retrieves and validates history, then passes the
enriched snapshot to the decision policy. `market_open` remains visible in the
snapshot and CLI output; closed hours do not stop read-only analysis.

The current production policy always holds. The broker boundary exposes no
order-submission method, so buy and sell orders cannot be sent until strategy,
position sizing, and risk rules are deliberately added. Any existing open order
also stops policy evaluation as a conservative duplicate-order safeguard.

Scheduling, live order execution, notifications, and strategy selection are
outside the current scope.

## Daily price history

The market-data adapter uses the same pinned SDK, with an explicit read-only
`https://data.alpaca.markets` endpoint and API-key authentication. Trading requests
continue to use Paper Trading. SDK URL and OAuth environment overrides are not
inherited. Data access depends on the account's market-data permissions; an
access failure is reported without switching feeds or endpoints.

For one configured symbol, each eligible cycle requests the latest **20 daily
bars** within the previous **120 calendar days**, using the **IEX** feed,
**split adjustment**, and USD prices. These are fixed initial data conventions,
not a selected strategy. IEX data represents one exchange rather than a
consolidated market feed. The SDK fetches additional pages when necessary.

The broker clock supplies the reference time. The exclusive cutoff is midnight
at the start of its current date in `America/New_York`; the inclusive API end is
one nanosecond before that cutoff. Date arithmetic handles daylight-saving
changes, and timezone data is embedded for minimal container environments.
Today's bar is excluded even after market close. History is still requested on
weekends and holidays, with the same date cutoff.

Validation requires exactly 20 bars, timestamps within the requested interval,
unique New York dates, newest-first API ordering, positive finite OHLC prices,
and consistent OHLC ranges. Zero volume is accepted. Validated bars are exposed
to the policy oldest-first, with timestamps in the market timezone. Prices use
`float64` for analysis and are not monetary amounts for order sizing.

Missing or malformed history fails the cycle before policy evaluation. Bars are
not synthesized or silently repaired. Validation does **not** prove that every
expected exchange session is present or that the latest expected session is
available; exchange-calendar freshness checks are a later prerequisite for a
strategy that depends on them. Split-adjusted historical values can also change
after corporate actions; this milestone does not archive reproducible datasets.

Example successful output (illustrative):

```text
daily cycle outcome=no_action dry_run=true positions=0 open_orders=0 market_open=false history=ready symbol=EXAMPLE bars=20 latest_bar=2026-10-29 feed=iex adjustment=split
```

Guarded cycles instead report `history=skipped`. No orders can be submitted in
either case. Databases, backtesting, scheduling, strategy logic, and execution
are outside this milestone.

## Test

```bash
go vet ./...
go test ./...
```

Automated tests use fake credentials and transports; no Alpaca connection is
required. A manual `go run .` with local Paper Trading credentials can verify
account and data access without submitting orders. Never commit the local `.env`.
