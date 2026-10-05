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
