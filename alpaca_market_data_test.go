package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
)

func barsResponse(bars []DailyBar, next *string) string {
	wire := make([]map[string]any, len(bars))
	for i, b := range bars {
		wire[i] = map[string]any{"t": b.Timestamp, "o": b.Open, "h": b.High, "l": b.Low, "c": b.Close, "v": b.Volume}
	}
	body, err := json.Marshal(map[string]any{"bars": map[string]any{"AAPL": wire}, "next_page_token": next})
	if err != nil {
		panic(err)
	}
	return string(body)
}

type trackedBody struct {
	io.Reader
	closed bool
}

func (b *trackedBody) Close() error { b.closed = true; return nil }

func TestMarketDataRetrievesPaginatedHistory(t *testing.T) {
	t.Setenv("APCA_API_DATA_URL", "https://unrelated.test")
	t.Setenv("APCA_API_OAUTH", "unrelated-token")
	req, err := newHistoryRequest("AAPL", testClockTime)
	if err != nil {
		t.Fatal(err)
	}
	fixtures := historyFixture(req)
	calls := 0
	var bodies []*trackedBody
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != "GET" || r.URL.Host != "data.alpaca.markets" || r.URL.Path != "/v2/stocks/bars" {
			t.Errorf("request=%s %s", r.Method, r.URL)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get("APCA-API-KEY-ID") != "key" || r.Header.Get("APCA-API-SECRET-KEY") != "secret" {
			t.Error("explicit key authentication not preserved")
		}
		q := r.URL.Query()
		for key, want := range map[string]string{"symbols": "AAPL", "timeframe": "1Day", "feed": "iex", "adjustment": "split", "sort": "desc", "currency": "USD", "start": req.Start.Format(time.RFC3339Nano), "end": req.Cutoff.Add(-time.Nanosecond).Format(time.RFC3339Nano)} {
			if q.Get(key) != want {
				t.Errorf("%s=%q want %q", key, q.Get(key), want)
			}
		}
		var body string
		if calls == 1 {
			if q.Get("limit") != "20" || q.Get("page_token") != "" {
				t.Errorf("first query=%s", q.Encode())
			}
			next := "page2"
			body = barsResponse(fixtures[:7], &next)
		} else {
			if calls != 2 || q.Get("limit") != "13" || q.Get("page_token") != "page2" {
				t.Errorf("second query=%s calls=%d", q.Encode(), calls)
			}
			body = barsResponse(fixtures[7:], nil)
		}
		tracked := &trackedBody{Reader: strings.NewReader(body)}
		bodies = append(bodies, tracked)
		response := httpResponse(200, "")
		response.Body = tracked
		return response, nil
	})}
	data, err := NewAlpacaMarketData("key", "secret", client)
	if err != nil {
		t.Fatal(err)
	}
	bars, err := data.DailyBars(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(bars) != len(fixtures) {
		t.Fatalf("calls=%d bars=%v", calls, bars)
	}
	for i, bar := range bars {
		want := fixtures[i]
		if !bar.Timestamp.Equal(want.Timestamp) || bar.Open != want.Open || bar.High != want.High || bar.Low != want.Low || bar.Close != want.Close || bar.Volume != want.Volume {
			t.Fatalf("bar %d=%v want %v", i, bar, want)
		}
	}
	for _, body := range bodies {
		if !body.closed {
			t.Error("response body not closed")
		}
	}
	if client.Timeout != 0 {
		t.Error("caller HTTP client mutated")
	}
}

func TestMarketDataErrors(t *testing.T) {
	req, _ := newHistoryRequest("AAPL", testClockTime)
	for _, tt := range []struct {
		name   string
		status int
		body   string
	}{
		{"forbidden", 403, `{"code":40310000,"message":"forbidden"}`},
		{"rate limit", 429, `{"code":42910000,"message":"rate limit"}`},
		{"server", 500, `{"message":"server error"}`},
		{"malformed", 200, `{"broken"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			data, err := NewAlpacaMarketData("key", "must-not-appear", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				response := httpResponse(tt.status, tt.body)
				response.Header.Set("X-Request-ID", "request-data")
				return response, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			_, err = data.DailyBars(context.Background(), req)
			if err == nil || calls != 1 || !strings.Contains(err.Error(), "request-data") || !strings.Contains(err.Error(), "AAPL") || strings.Contains(err.Error(), "must-not-appear") {
				t.Fatalf("calls=%d error=%v", calls, err)
			}
			if tt.status >= 300 {
				var apiError *alpaca.APIError
				if !errors.As(err, &apiError) || apiError.StatusCode != tt.status {
					t.Fatalf("typed API error missing: %v", err)
				}
			}
		})
	}
}

func TestMarketDataCancellationAndTimeout(t *testing.T) {
	req, _ := newHistoryRequest("AAPL", testClockTime)
	for _, mode := range []string{"before request", "in flight", "caller deadline", "HTTP timeout", "between pages"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "caller deadline" {
				var deadlineCancel context.CancelFunc
				ctx, deadlineCancel = context.WithTimeout(ctx, 10*time.Millisecond)
				defer deadlineCancel()
			}
			if mode == "before request" {
				cancel()
			}
			calls := 0
			client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if mode == "between pages" && calls == 1 {
					next := "page2"
					cancel()
					return httpResponse(200, barsResponse(historyFixture(req)[:1], &next)), nil
				}
				if mode == "in flight" {
					cancel()
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			if mode == "HTTP timeout" {
				client.Timeout = 10 * time.Millisecond
			}
			data, err := NewAlpacaMarketData("key", "secret", client)
			if err != nil {
				t.Fatal(err)
			}
			_, err = data.DailyBars(ctx, req)
			want := context.Canceled
			if mode == "caller deadline" || mode == "HTTP timeout" {
				want = context.DeadlineExceeded
			}
			if !errors.Is(err, want) {
				t.Fatalf("error=%v want %v", err, want)
			}
			if mode == "before request" && calls != 0 {
				t.Fatalf("canceled request sent: %d", calls)
			}
			if mode == "between pages" && calls != 1 {
				t.Fatalf("request sent after cancellation: %d", calls)
			}
		})
	}
}

func TestMarketDataBoundedDecodedResponses(t *testing.T) {
	req, _ := newHistoryRequest("AAPL", testClockTime)
	for _, tt := range []struct {
		name       string
		body       string
		compressed bool
		wantError  bool
	}{
		{"gzip valid", barsResponse(historyFixture(req), nil), true, false},
		{"oversized", strings.Repeat(" ", maxAlpacaResponseSize) + "{}", false, true},
		{"gzip oversized", strings.Repeat(" ", maxAlpacaResponseSize) + "{}", true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wire := []byte(tt.body)
			if tt.compressed {
				var buffer bytes.Buffer
				writer := gzip.NewWriter(&buffer)
				if _, err := writer.Write(wire); err != nil {
					t.Fatal(err)
				}
				if err := writer.Close(); err != nil {
					t.Fatal(err)
				}
				wire = buffer.Bytes()
			}
			body := &trackedBody{Reader: bytes.NewReader(wire)}
			data, err := NewAlpacaMarketData("key", "secret", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				response := httpResponse(200, "")
				response.Body = body
				if tt.compressed {
					response.Header.Set("Content-Encoding", "gzip")
				}
				return response, nil
			})})
			if err != nil {
				t.Fatal(err)
			}
			bars, err := data.DailyBars(context.Background(), req)
			if tt.wantError {
				if err == nil || !strings.Contains(err.Error(), "exceeds") {
					t.Fatalf("error=%v", err)
				}
			} else if err != nil || len(bars) != 20 {
				t.Fatalf("bars=%d error=%v", len(bars), err)
			}
			if !body.closed {
				t.Fatal("response not closed")
			}
		})
	}
}

func TestMarketDataRejectsInvalidConfigurationAndRequests(t *testing.T) {
	for _, tt := range []struct {
		url, key, secret string
		client           *http.Client
	}{
		{"://bad", "key", "secret", &http.Client{}}, {"https://data.test", "", "secret", &http.Client{}},
		{"https://data.test", "key", "", &http.Client{}}, {"https://data.test", "key", "secret", nil},
	} {
		if _, err := newAlpacaMarketData(tt.url, tt.key, tt.secret, tt.client); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	calls := 0
	data, err := NewAlpacaMarketData("key", "secret", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("unexpected request") })})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := newHistoryRequest("AAPL", testClockTime)
	for _, mutate := range []func(*HistoryRequest){
		func(r *HistoryRequest) { r.Symbol = " " }, func(r *HistoryRequest) { r.Count = 0 }, func(r *HistoryRequest) { r.Count = 21 }, func(r *HistoryRequest) { r.Start = r.Cutoff },
	} {
		bad := req
		mutate(&bad)
		if _, err := data.DailyBars(context.Background(), bad); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	if calls != 0 {
		t.Fatalf("invalid request sent %d calls", calls)
	}
}

type failingBody struct {
	readErr, closeErr error
	closed            bool
}

func (b *failingBody) Read([]byte) (int, error) { return 0, b.readErr }
func (b *failingBody) Close() error             { b.closed = true; return b.closeErr }

func TestMarketDataReportsBodyFailures(t *testing.T) {
	req, err := newHistoryRequest("AAPL", testClockTime)
	if err != nil {
		t.Fatal(err)
	}
	readFailure := errors.New("read failed")
	closeFailure := errors.New("close failed")
	body := &failingBody{readErr: readFailure, closeErr: closeFailure}
	data, err := NewAlpacaMarketData("key", "secret", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		response := httpResponse(200, "")
		response.Body = body
		return response, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	_, err = data.DailyBars(context.Background(), req)
	if !errors.Is(err, readFailure) || !errors.Is(err, closeFailure) || !body.closed {
		t.Fatalf("error=%v closed=%t", err, body.closed)
	}
}
