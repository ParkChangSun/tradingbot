package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
)

func TestAlpacaLatestQuoteUsesExplicitIEXReadOnlyEndpoint(t *testing.T) {
	t.Setenv("APCA_API_DATA_URL", "https://unrelated.test")
	t.Setenv("APCA_API_OAUTH", "unrelated-token")
	calls := 0
	var body *trackedBody
	client := &http.Client{Timeout: 15 * time.Second, Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.Method != "GET" || req.URL.Host != "data.alpaca.markets" || req.URL.Path != "/v2/stocks/quotes/latest" {
			t.Fatal("incorrect quote request")
		}
		q := req.URL.Query()
		if q.Get("symbols") != "AAPL" || q.Get("feed") != "iex" || q.Get("currency") != "USD" {
			t.Fatal("incorrect quote conventions")
		}
		if req.Header.Get("APCA-API-KEY-ID") != "fake-key" || req.Header.Get("APCA-API-SECRET-KEY") != "fake-secret" || req.Header.Get("Authorization") != "" {
			t.Fatal("authentication override")
		}
		body = &trackedBody{Reader: strings.NewReader(`{"quotes":{"AAPL":{"t":"2026-10-30T14:59:59Z","bp":9.125,"ap":9.25}}}`)}
		r := httpResponse(200, "")
		r.Body = body
		return r, nil
	})}
	data, err := NewAlpacaMarketData("fake-key", "fake-secret", client)
	if err != nil {
		t.Fatal(err)
	}
	quote, err := data.LatestQuote(context.Background(), " aapl ")
	if err != nil {
		t.Fatal(err)
	}
	if quote.Symbol != "AAPL" || quote.Feed != "iex" || quote.BidPrice != "9.125" || quote.AskPrice != "9.25" || !quote.Timestamp.Equal(testClockTime.Add(-time.Second)) || !body.closed || calls != 1 {
		t.Fatalf("quote=%+v error=%v calls=%d", quote, err, calls)
	}
	if client.Timeout != 15*time.Second {
		t.Fatal("caller client changed")
	}
}

func TestAlpacaLatestQuoteFailuresDoNotFallbackOrRetry(t *testing.T) {
	for _, tt := range []struct {
		name     string
		status   int
		body     string
		apiError bool
	}{
		{"missing symbol", 200, `{"quotes":{"MSFT":{"bp":9,"ap":10}}}`, false},
		{"null quote", 200, `{"quotes":{}}`, false},
		{"malformed response", 200, `{`, false},
		{"access denied", 403, `{"code":40310000,"message":"data unavailable"}`, true},
		{"rate limited", 429, `{"code":42910000,"message":"rate limited"}`, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			data, err := NewAlpacaMarketData("fake-key", "fake-secret", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				r := httpResponse(tt.status, tt.body)
				r.Header.Set("X-Request-ID", "quote-request")
				return r, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			_, err = data.LatestQuote(context.Background(), "AAPL")
			if err == nil || calls != 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if tt.apiError {
				var apiErr *alpaca.APIError
				if !errors.As(err, &apiErr) || !strings.Contains(err.Error(), "quote-request") {
					t.Fatalf("SDK error/request ID lost: %v", err)
				}
			}
		})
	}
	calls := 0
	data, err := NewAlpacaMarketData("fake-key", "fake-secret", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected call") })})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.LatestQuote(context.Background(), " "); err == nil {
		t.Fatal("empty symbol accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := data.LatestQuote(ctx, "AAPL"); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation lost")
	}
	if calls != 0 {
		t.Fatal("invalid or canceled quote request sent")
	}
}
