package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
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
	var response struct {
		Timestamp time.Time `json:"timestamp"`
		IsOpen    bool      `json:"is_open"`
		NextOpen  time.Time `json:"next_open"`
		NextClose time.Time `json:"next_close"`
	}
	if err := b.get(ctx, "/v2/clock", &response); err != nil {
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
	var response struct {
		ID             string `json:"id"`
		Status         string `json:"status"`
		Currency       string `json:"currency"`
		BuyingPower    string `json:"buying_power"`
		Equity         string `json:"equity"`
		Cash           string `json:"cash"`
		TradingBlocked bool   `json:"trading_blocked"`
	}
	if err := b.get(ctx, "/v2/account", &response); err != nil {
		return AccountSnapshot{}, err
	}

	return AccountSnapshot{
		ID:             response.ID,
		Status:         response.Status,
		Currency:       response.Currency,
		BuyingPower:    response.BuyingPower,
		Equity:         response.Equity,
		Cash:           response.Cash,
		TradingBlocked: response.TradingBlocked,
	}, nil
}

func (b *AlpacaBroker) Positions(ctx context.Context) ([]Position, error) {
	var response []struct {
		AssetID     string `json:"asset_id"`
		Symbol      string `json:"symbol"`
		AssetClass  string `json:"asset_class"`
		Quantity    string `json:"qty"`
		Side        string `json:"side"`
		MarketValue string `json:"market_value"`
	}
	if err := b.get(ctx, "/v2/positions", &response); err != nil {
		return nil, err
	}

	positions := make([]Position, len(response))
	for i, position := range response {
		positions[i] = Position{
			AssetID:     position.AssetID,
			Symbol:      position.Symbol,
			AssetClass:  position.AssetClass,
			Quantity:    position.Quantity,
			Side:        position.Side,
			MarketValue: position.MarketValue,
		}
	}
	return positions, nil
}

func (b *AlpacaBroker) OpenOrders(ctx context.Context) ([]OpenOrder, error) {
	var response []struct {
		ID            string    `json:"id"`
		ClientOrderID string    `json:"client_order_id"`
		Symbol        string    `json:"symbol"`
		AssetClass    string    `json:"asset_class"`
		Side          string    `json:"side"`
		Quantity      string    `json:"qty"`
		Status        string    `json:"status"`
		SubmittedAt   time.Time `json:"submitted_at"`
	}
	if err := b.get(ctx, "/v2/orders?status=open&limit=500&direction=asc", &response); err != nil {
		return nil, err
	}

	orders := make([]OpenOrder, len(response))
	for i, order := range response {
		orders[i] = OpenOrder{
			ID:            order.ID,
			ClientOrderID: order.ClientOrderID,
			Symbol:        order.Symbol,
			AssetClass:    order.AssetClass,
			Side:          order.Side,
			Quantity:      order.Quantity,
			Status:        order.Status,
			SubmittedAt:   order.SubmittedAt,
		}
	}
	return orders, nil
}

func (b *AlpacaBroker) get(ctx context.Context, endpoint string, target any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, b.baseURL+endpoint, nil)
	if err != nil {
		return fmt.Errorf("create Alpaca request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("APCA-API-KEY-ID", b.apiKey)
	request.Header.Set("APCA-API-SECRET-KEY", b.apiSecret)

	response, err := b.client.Do(request)
	if err != nil {
		return fmt.Errorf("send Alpaca request: %w", err)
	}
	defer response.Body.Close()

	limitedBody := io.LimitReader(response.Body, maxAlpacaResponseSize)
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		var apiError struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(limitedBody).Decode(&apiError)
		if apiError.Message == "" {
			apiError.Message = "request failed"
		}
		if requestID := response.Header.Get("X-Request-ID"); requestID != "" {
			return fmt.Errorf("Alpaca API returned %s (request ID %s): %s", response.Status, requestID, apiError.Message)
		}
		return fmt.Errorf("Alpaca API returned %s: %s", response.Status, apiError.Message)
	}

	if err := json.NewDecoder(limitedBody).Decode(target); err != nil {
		return fmt.Errorf("decode Alpaca response: %w", err)
	}
	return nil
}
