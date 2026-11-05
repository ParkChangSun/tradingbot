package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
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
			body = `[{"asset_id":"asset-1","symbol":"AAPL","asset_class":"us_equity","qty":"2","side":"long","market_value":"450.00","avg_entry_price":"200.50"}]`
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
		BuyingPower: "25000.5", Equity: "20000.25", Cash: "5000.25",
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
		Quantity: "2", Side: "long", MarketValue: "450", AverageEntryPrice: "200.5",
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
		"/v2/orders?direction=asc&limit=500&nested=false&status=open",
	}
	if !reflect.DeepEqual(requestedPaths, wantPaths) {
		t.Errorf("request paths = %v, want %v", requestedPaths, wantPaths)
	}
}

func TestAlpacaBrokerReportsAPIErrorWithoutCredentials(t *testing.T) {
	const secret = "must-not-appear"
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := httpResponse(http.StatusForbidden, `{"code":40310000,"message":"forbidden"}`)
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
	var apiError *alpaca.APIError
	if !errors.As(err, &apiError) || apiError.StatusCode != 403 || apiError.Code != 40310000 {
		t.Fatalf("error = %v, want typed SDK API error", err)
	}
	for _, want := range []string{"request-123", "forbidden"} {
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
	if err == nil || !strings.Contains(err.Error(), "Alpaca request failed") {
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

func TestAlpacaBrokerPreservesPaperAndKeyAuthentication(t *testing.T) {
	t.Setenv("APCA_API_BASE_URL", "https://api.alpaca.markets")
	t.Setenv("APCA_API_OAUTH", "unrelated-token")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "paper-api.alpaca.markets" || r.Header.Get("Authorization") != "" || r.Header.Get("APCA-API-KEY-ID") != "key" || r.Header.Get("APCA-API-SECRET-KEY") != "secret" {
			t.Error("paper host or explicit authentication changed")
		}
		return httpResponse(200, `{}`), nil
	})}
	broker, err := NewAlpacaBroker("key", "secret", client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Account(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAlpacaBrokerDoesNotRetryRateLimits(t *testing.T) {
	attempts := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts++
		return httpResponse(429, `{"code":42910000,"message":"rate limit exceeded"}`), nil
	})}
	broker, err := NewAlpacaBroker("key", "secret", client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = broker.Account(context.Background())
	if err == nil || attempts != 1 {
		t.Fatalf("error = %v, attempts = %d; want error and one attempt", err, attempts)
	}
}

func TestAlpacaBrokerCancelsInFlightRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		cancel()
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	broker, err := NewAlpacaBroker("key", "secret", client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = broker.Account(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
}

func TestAlpacaBrokerPreservesHTTPTimeout(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Millisecond, Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	broker, err := NewAlpacaBroker("key", "secret", client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = broker.Account(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want timeout", err)
	}
}

func TestAlpacaBrokerHandlesNullDecimals(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/v2/positions" {
			return httpResponse(200, `[{"qty":"0.125","market_value":null}]`), nil
		}
		return httpResponse(200, `[{"qty":null,"notional":"100"}]`), nil
	})}
	broker, err := NewAlpacaBroker("key", "secret", client)
	if err != nil {
		t.Fatal(err)
	}
	positions, err := broker.Positions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(positions) != 1 || positions[0].Quantity != "0.125" || positions[0].MarketValue != "" {
		t.Fatalf("positions = %#v", positions)
	}
	orders, err := broker.OpenOrders(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(orders) != 1 || orders[0].Quantity != "" {
		t.Fatalf("orders = %#v", orders)
	}
}

func TestAlpacaBrokerLimitsResponseSize(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return httpResponse(200, `{"id":"`+strings.Repeat("x", maxAlpacaResponseSize)+`"}`), nil
	})}
	broker, err := NewAlpacaBroker("key", "secret", client)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := broker.Account(context.Background()); err == nil {
		t.Fatal("oversized response succeeded")
	}
}

func TestAlpacaBrokerPreservesCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	broker, err := NewAlpacaBroker("key", "secret", client)
	if err != nil {
		t.Fatal(err)
	}
	_, err = broker.Account(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want caller deadline", err)
	}
}
