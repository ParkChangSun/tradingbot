# Trading Bot

Small Go CLI for exploring a US stock trading bot.

## Run

Set the required Alpaca credentials. The API base URL defaults to Alpaca Paper
Trading (`https://paper-api.alpaca.markets`).

```bash
export APCA_API_KEY_ID="your-paper-api-key"
export APCA_API_SECRET_KEY="your-paper-api-secret"
go run .
```

To override the API endpoint, set `APCA_API_BASE_URL`.

## Test

```bash
go test ./...
```
