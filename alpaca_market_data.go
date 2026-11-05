package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/shopspring/decimal"
)

const alpacaDataAPIBaseURL = "https://data.alpaca.markets"

// AlpacaMarketData reuses the broker's connection configuration and transport;
// all requests here go through the SDK's separate read-only market-data client.
type AlpacaMarketData struct{ connection *AlpacaBroker }

func NewAlpacaMarketData(key, secret string, client *http.Client) (*AlpacaMarketData, error) {
	return newAlpacaMarketData(alpacaDataAPIBaseURL, key, secret, client)
}

func newAlpacaMarketData(baseURL, key, secret string, client *http.Client) (*AlpacaMarketData, error) {
	connection, err := newAlpacaBroker(baseURL, key, secret, client)
	if err != nil {
		return nil, err
	}
	return &AlpacaMarketData{connection: connection}, nil
}

func (m *AlpacaMarketData) DailyBars(ctx context.Context, req HistoryRequest) ([]DailyBar, error) {
	if err := validateHistoryRequest(req); err != nil {
		return nil, err
	}
	bars, err := callMarketData(m, ctx, func(sdk *marketdata.Client) ([]marketdata.Bar, error) {
		return sdk.GetBars(req.Symbol, marketdata.GetBarsRequest{
			TimeFrame: marketdata.OneDay, Start: req.Start, End: req.Cutoff.Add(-time.Nanosecond), TotalLimit: req.Count,
			Feed: dailyHistoryFeed, Adjustment: marketdata.AdjustmentSplit, Sort: marketdata.SortDesc, Currency: "USD",
		})
	})
	if err != nil {
		return nil, fmt.Errorf("get daily bars for %s: %w", req.Symbol, err)
	}
	result := make([]DailyBar, len(bars))
	for i, bar := range bars {
		result[i] = DailyBar{Timestamp: bar.Timestamp, Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, Volume: bar.Volume}
	}
	return result, nil
}

func (m *AlpacaMarketData) LatestQuote(ctx context.Context, symbol string) (LatestQuote, error) {
	symbol = strings.ToUpper(strings.TrimSpace(symbol))
	if symbol == "" {
		return LatestQuote{}, fmt.Errorf("quote symbol is required")
	}
	quote, err := callMarketData(m, ctx, func(sdk *marketdata.Client) (*marketdata.Quote, error) {
		return sdk.GetLatestQuote(symbol, marketdata.GetLatestQuoteRequest{Feed: dailyHistoryFeed, Currency: "USD"})
	})
	if err != nil {
		return LatestQuote{}, fmt.Errorf("get latest quote for %s: %w", symbol, err)
	}
	if quote == nil {
		return LatestQuote{}, fmt.Errorf("latest quote is missing for %s", symbol)
	}
	if math.IsNaN(quote.BidPrice) || math.IsInf(quote.BidPrice, 0) || math.IsNaN(quote.AskPrice) || math.IsInf(quote.AskPrice, 0) {
		return LatestQuote{}, fmt.Errorf("quote contains non-finite prices")
	}
	return LatestQuote{Symbol: symbol, Feed: dailyHistoryFeed, Timestamp: quote.Timestamp, BidPrice: decimal.NewFromFloat(quote.BidPrice).String(), AskPrice: decimal.NewFromFloat(quote.AskPrice).String()}, nil
}

// Share the existing explicit authentication, bounded transport and cancellation
// behavior across history and quote requests. No feed or endpoint fallback.
func callMarketData[T any](m *AlpacaMarketData, ctx context.Context, call func(*marketdata.Client) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	b := m.connection
	transport := b.client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	var requestID string
	client := *b.client
	client.Transport = alpacaTransport{ctx: ctx, base: transport, apiKey: b.apiKey, apiSecret: b.apiSecret, requestID: &requestID}
	sdk := marketdata.NewClient(marketdata.ClientOpts{
		APIKey: b.apiKey, APISecret: b.apiSecret, BaseURL: b.baseURL,
		HTTPClient: &client, RetryLimit: -1,
	})
	result, err := call(sdk)
	if err != nil {
		if ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		if requestID != "" {
			return zero, fmt.Errorf("market data request (request ID %s): %w", requestID, err)
		}
		return zero, err
	}
	return result, nil
}
