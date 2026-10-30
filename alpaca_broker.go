package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/shopspring/decimal"
)

const (
	alpacaPaperAPIBaseURL = "https://paper-api.alpaca.markets"
	maxAlpacaResponseSize = 4 << 20
)

type AlpacaBroker struct {
	baseURL   string
	apiKey    string
	apiSecret string
	client    *http.Client
}

func NewAlpacaBroker(apiKey, apiSecret string, client *http.Client) (*AlpacaBroker, error) {
	return newAlpacaBroker(alpacaPaperAPIBaseURL, apiKey, apiSecret, client)
}

func newAlpacaBroker(baseURL, apiKey, apiSecret string, client *http.Client) (*AlpacaBroker, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Host == "" || parsedURL.User != nil {
		return nil, fmt.Errorf("invalid Alpaca API base URL")
	}
	apiKey = strings.TrimSpace(apiKey)
	apiSecret = strings.TrimSpace(apiSecret)
	if apiKey == "" {
		return nil, fmt.Errorf("Alpaca API key is required")
	}
	if apiSecret == "" {
		return nil, fmt.Errorf("Alpaca API secret is required")
	}
	if client == nil {
		return nil, fmt.Errorf("HTTP client is required")
	}

	return &AlpacaBroker{
		baseURL:   strings.TrimRight(parsedURL.String(), "/"),
		apiKey:    apiKey,
		apiSecret: apiSecret,
		client:    client,
	}, nil
}

func (b *AlpacaBroker) Clock(ctx context.Context) (MarketClock, error) {
	response, err := callAlpaca(b, ctx, func(client *alpaca.Client) (*alpaca.Clock, error) { return client.GetClock() })
	if err != nil {
		return MarketClock{}, err
	}

	return MarketClock{
		Timestamp: response.Timestamp,
		IsOpen:    response.IsOpen,
		NextOpen:  response.NextOpen,
		NextClose: response.NextClose,
	}, nil
}

func (b *AlpacaBroker) Account(ctx context.Context) (AccountSnapshot, error) {
	response, err := callAlpaca(b, ctx, func(client *alpaca.Client) (*alpaca.Account, error) { return client.GetAccount() })
	if err != nil {
		return AccountSnapshot{}, err
	}

	return AccountSnapshot{
		ID:             response.ID,
		Status:         response.Status,
		Currency:       response.Currency,
		BuyingPower:    response.BuyingPower.String(),
		Equity:         response.Equity.String(),
		Cash:           response.Cash.String(),
		TradingBlocked: response.TradingBlocked,
	}, nil
}

func (b *AlpacaBroker) Positions(ctx context.Context) ([]Position, error) {
	response, err := callAlpaca(b, ctx, func(client *alpaca.Client) ([]alpaca.Position, error) { return client.GetPositions() })
	if err != nil {
		return nil, err
	}

	positions := make([]Position, len(response))
	for i, position := range response {
		positions[i] = Position{
			AssetID:     position.AssetID,
			Symbol:      position.Symbol,
			AssetClass:  string(position.AssetClass),
			Quantity:    position.Qty.String(),
			Side:        position.Side,
			MarketValue: optionalDecimal(position.MarketValue),
		}
	}
	return positions, nil
}

func (b *AlpacaBroker) OpenOrders(ctx context.Context) ([]OpenOrder, error) {
	response, err := callAlpaca(b, ctx, func(client *alpaca.Client) ([]alpaca.Order, error) {
		return client.GetOrders(alpaca.GetOrdersRequest{Status: "open", Limit: 500, Direction: "asc"})
	})
	if err != nil {
		return nil, err
	}

	orders := make([]OpenOrder, len(response))
	for i, order := range response {
		orders[i] = OpenOrder{
			ID:            order.ID,
			ClientOrderID: order.ClientOrderID,
			Symbol:        order.Symbol,
			AssetClass:    string(order.AssetClass),
			Side:          string(order.Side),
			Quantity:      optionalDecimal(order.Qty),
			Status:        order.Status,
			SubmittedAt:   order.SubmittedAt,
		}
	}
	return orders, nil
}

// callAlpaca uses a per-call client so context and response metadata never leak
// between concurrent broker calls. The caller's HTTP client is copied, not mutated.
func callAlpaca[T any](b *AlpacaBroker, ctx context.Context, call func(*alpaca.Client) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	transport := b.client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	var requestID string
	client := *b.client
	client.Transport = alpacaTransport{ctx: ctx, base: transport, apiKey: b.apiKey, apiSecret: b.apiSecret, requestID: &requestID}
	sdk := alpaca.NewClient(alpaca.ClientOpts{
		APIKey: b.apiKey, APISecret: b.apiSecret, BaseURL: b.baseURL,
		HTTPClient: &client,
		// Zero selects the SDK default retries; a negative value disables retries.
		RetryLimit: -1,
	})
	result, err := call(sdk)
	if err != nil {
		if ctx.Err() != nil {
			err = errors.Join(err, ctx.Err())
		}
		if requestID != "" {
			return zero, fmt.Errorf("Alpaca request failed (request ID %s): %w", requestID, err)
		}
		return zero, fmt.Errorf("Alpaca request failed: %w", err)
	}
	return result, nil
}

type alpacaTransport struct {
	ctx               context.Context
	base              http.RoundTripper
	apiKey, apiSecret string
	requestID         *string
}

func (t alpacaTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// Keep the HTTP client's timeout context while adding caller cancellation.
	ctx, cancel := context.WithCancel(request.Context())
	stop := context.AfterFunc(t.ctx, cancel)
	cleanup := func() { stop(); cancel() }
	request = request.Clone(ctx)
	// The SDK can load OAuth from the environment. Preserve this application's
	// explicit API-key authentication even when that unrelated variable is set.
	request.Header.Del("Authorization")
	request.Header.Set("APCA-API-KEY-ID", t.apiKey)
	request.Header.Set("APCA-API-SECRET-KEY", t.apiSecret)
	request.Header.Set("Accept", "application/json")
	response, err := t.base.RoundTrip(request)
	if err != nil {
		cleanup()
		return nil, err
	}
	*t.requestID = response.Header.Get("X-Request-ID")
	response.Body = &limitedAlpacaBody{Reader: io.LimitReader(response.Body, maxAlpacaResponseSize), Closer: response.Body, cleanup: cleanup}
	return response, nil
}

type limitedAlpacaBody struct {
	io.Reader
	io.Closer
	cleanup func()
}

func (b *limitedAlpacaBody) Close() error {
	defer b.cleanup()
	return b.Closer.Close()
}

func optionalDecimal(value *decimal.Decimal) string {
	if value == nil {
		return ""
	}
	return value.String()
}
