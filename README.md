# Trading Bot

Small Go CLI for exploring a US stock trading bot.

## Run

Copy the example environment file and add your Alpaca Paper Trading credentials:

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
go run .
```

The bot always uses the Alpaca Paper Trading API at
`https://paper-api.alpaca.markets`. Only credentials are configurable.

The Alpaca adapter uses the official `alpaca-trade-api-go/v3` SDK, pinned to
v3.11.0. It explicitly selects the paper URL and API-key authentication rather
than inheriting SDK base-URL or OAuth environment settings. Each request retains
the caller's cancellation, the CLI's 15-second HTTP timeout, and a 4 MiB response
read limit. Automatic SDK retries are disabled, so a rate-limit response fails
the cycle immediately. SDK API errors remain available through `errors.As`,
with response request IDs included in error messages when present.

SDK decimal values are converted to normalized strings in snapshots (for
example, `"450.00"` becomes `"450"`). Nullable position market values and order
quantities remain empty strings when unavailable. Open-order retrieval remains
limited to one page of up to 500 orders.

## Daily cycle

Each invocation runs a single cycle. It reads the US market clock first and
stops when the market is closed. When the market is open, it reads the account,
positions, and open orders before evaluating a decision policy.

The current production policy always holds. The broker boundary exposes no
order-submission method, so buy and sell orders cannot be sent until strategy,
position sizing, and risk rules are deliberately added. Any existing open order
also stops policy evaluation as a conservative duplicate-order safeguard.

Scheduling, live order execution, notifications, and strategy selection are
outside the current scope.

## Test

```bash
go test ./...
```
