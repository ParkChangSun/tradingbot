# Trading Bot

Small Go CLI for exploring a US stock trading bot.

## Run

Copy the example environment file and add your Alpaca Paper Trading credentials:

```bash
cp .env.example .env
```

Then start the bot:

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

The API base URL defaults to Alpaca Paper Trading
(`https://paper-api.alpaca.markets`). To override it, set
`APCA_API_BASE_URL`.

## Test

```bash
go test ./...
```
