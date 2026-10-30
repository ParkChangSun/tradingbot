package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
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
	if err := ctx.Err(); err != nil {
		return nil, err
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
	bars, err := sdk.GetBars(req.Symbol, marketdata.GetBarsRequest{
		TimeFrame: marketdata.OneDay, Start: req.Start, End: req.Cutoff.Add(-time.Nanosecond), TotalLimit: req.Count,
		Feed: dailyHistoryFeed, Adjustment: marketdata.AdjustmentSplit, Sort: marketdata.SortDesc, Currency: "USD",
	})
	if err != nil {
		if ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		if requestID != "" {
			return nil, fmt.Errorf("get daily bars for %s (request ID %s): %w", req.Symbol, requestID, err)
		}
		return nil, fmt.Errorf("get daily bars for %s: %w", req.Symbol, err)
	}
	result := make([]DailyBar, len(bars))
	for i, bar := range bars {
		result[i] = DailyBar{Timestamp: bar.Timestamp, Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, Volume: bar.Volume}
	}
	return result, nil
}
