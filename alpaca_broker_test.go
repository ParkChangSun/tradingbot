package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewAlpacaBrokerUsesPaperTradingURL(t *testing.T) {
	broker, err := NewAlpacaBroker("key", "secret", &http.Client{})
	if err != nil {
		t.Fatalf("NewAlpacaBroker returned error: %v", err)
	}
	if broker.baseURL != alpacaPaperAPIBaseURL {
		t.Errorf("baseURL = %q, want %q", broker.baseURL, alpacaPaperAPIBaseURL)
	}
}

func TestNewAlpacaBrokerValidatesConfiguration(t *testing.T) {
	client := &http.Client{}
	tests := []struct {
		name      string
		baseURL   string
		apiKey    string
		apiSecret string
		client    *http.Client
		want      string
	}{
		{name: "invalid URL", baseURL: "://bad", apiKey: "key", apiSecret: "secret", client: client, want: "invalid Alpaca API base URL"},
		{name: "unsupported URL scheme", baseURL: "ftp://example.test", apiKey: "key", apiSecret: "secret", client: client, want: "invalid Alpaca API base URL"},
		{name: "URL credentials", baseURL: "https://user:password@example.test", apiKey: "key", apiSecret: "secret", client: client, want: "invalid Alpaca API base URL"},
		{name: "missing key", baseURL: "https://example.test", apiSecret: "secret", client: client, want: "API key is required"},
		{name: "missing secret", baseURL: "https://example.test", apiKey: "key", client: client, want: "API secret is required"},
		{name: "missing client", baseURL: "https://example.test", apiKey: "key", apiSecret: "secret", want: "HTTP client is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newAlpacaBroker(tt.baseURL, tt.apiKey, tt.apiSecret, tt.client)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("NewAlpacaBroker error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestAlpacaBrokerRetrievesDailySnapshots(t *testing.T) {
	const (
		apiKey    = "test-key"
		apiSecret = "test-secret"
	)
	requestedPaths := make([]string, 0, 4)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("APCA-API-KEY-ID"); got != apiKey {
			t.Errorf("API key header = %q, want %q", got, apiKey)
		}
		if got := r.Header.Get("APCA-API-SECRET-KEY"); got != apiSecret {
			t.Errorf("API secret header = %q, want %q", got, apiSecret)
		}
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		requestedPaths = append(requestedPaths, r.URL.RequestURI())

		var body string
		switch r.URL.Path {
		case "/v2/clock":
			body = `{"timestamp":"2026-09-08T14:00:00Z","is_open":true,"next_open":"2026-09-09T13:30:00Z","next_close":"2026-09-08T20:00:00Z"}`
		case "/v2/account":
			body = `{"id":"account-1","status":"ACTIVE","currency":"USD","buying_power":"25000.50","equity":"20000.25","cash":"5000.25","trading_blocked":false}`
		case "/v2/positions":
			body = `[{"asset_id":"asset-1","symbol":"AAPL","asset_class":"us_equity","qty":"2","side":"long","market_value":"450.00"}]`
		case "/v2/orders":
			body = `[{"id":"order-1","client_order_id":"daily-1","symbol":"MSFT","asset_class":"us_equity","side":"buy","qty":"1","status":"accepted","submitted_at":"2026-09-08T14:01:02Z"}]`
		default:
			return httpResponse(http.StatusNotFound, `{"message":"not found"}`), nil
		}
		return httpResponse(http.StatusOK, body), nil
	})}

	broker, err := newAlpacaBroker("https://paper.test/", apiKey, apiSecret, client)
	if err != nil {
		t.Fatalf("NewAlpacaBroker returned error: %v", err)
	}
	ctx := context.Background()

	clock, err := broker.Clock(ctx)
	if err != nil {
		t.Fatalf("Clock returned error: %v", err)
	}
	if !clock.IsOpen || clock.Timestamp.Format(time.RFC3339) != "2026-09-08T14:00:00Z" {
		t.Errorf("Clock = %#v", clock)
	}

	account, err := broker.Account(ctx)
	if err != nil {
		t.Fatalf("Account returned error: %v", err)
	}
	wantAccount := AccountSnapshot{
		ID: "account-1", Status: "ACTIVE", Currency: "USD",
		BuyingPower: "25000.50", Equity: "20000.25", Cash: "5000.25",
	}
	if !reflect.DeepEqual(account, wantAccount) {
		t.Errorf("Account = %#v, want %#v", account, wantAccount)
	}

	positions, err := broker.Positions(ctx)
	if err != nil {
		t.Fatalf("Positions returned error: %v", err)
	}
	wantPositions := []Position{{
		AssetID: "asset-1", Symbol: "AAPL", AssetClass: "us_equity",
		Quantity: "2", Side: "long", MarketValue: "450.00",
	}}
	if !reflect.DeepEqual(positions, wantPositions) {
		t.Errorf("Positions = %#v, want %#v", positions, wantPositions)
	}

	orders, err := broker.OpenOrders(ctx)
	if err != nil {
		t.Fatalf("OpenOrders returned error: %v", err)
	}
	if len(orders) != 1 || orders[0].ClientOrderID != "daily-1" || orders[0].SubmittedAt.Format(time.RFC3339) != "2026-09-08T14:01:02Z" {
		t.Errorf("OpenOrders = %#v", orders)
	}

	wantPaths := []string{
		"/v2/clock",
		"/v2/account",
		"/v2/positions",
		"/v2/orders?status=open&limit=500&direction=asc",
	}
	if !reflect.DeepEqual(requestedPaths, wantPaths) {
		t.Errorf("request paths = %v, want %v", requestedPaths, wantPaths)
	}
}

func TestAlpacaBrokerReportsAPIErrorWithoutCredentials(t *testing.T) {
	const secret = "must-not-appear"
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := httpResponse(http.StatusForbidden, `{"message":"forbidden"}`)
		response.Header.Set("X-Request-ID", "request-123")
		return response, nil
	})}

	broker, err := newAlpacaBroker("https://paper.test", "key", secret, client)
	if err != nil {
		t.Fatalf("NewAlpacaBroker returned error: %v", err)
	}
	_, err = broker.Account(context.Background())
	if err == nil {
		t.Fatal("Account returned nil error")
	}
	for _, want := range []string{"403 Forbidden", "request-123", "forbidden"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("error exposed API secret: %q", err)
	}
}

func TestAlpacaBrokerReportsMalformedResponse(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return httpResponse(http.StatusOK, `{"broken"`), nil
	})}

	broker, err := newAlpacaBroker("https://paper.test", "key", "secret", client)
	if err != nil {
		t.Fatalf("NewAlpacaBroker returned error: %v", err)
	}
	_, err = broker.Clock(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode Alpaca response") {
		t.Fatalf("Clock error = %v, want decoding context", err)
	}
}

func TestAlpacaBrokerHonorsCanceledContext(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, r.Context().Err()
	})}

	broker, err := newAlpacaBroker("https://paper.test", "key", "secret", client)
	if err != nil {
		t.Fatalf("NewAlpacaBroker returned error: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = broker.Account(ctx)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Fatalf("Account error = %v, want context canceled", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func httpResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode)),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}
