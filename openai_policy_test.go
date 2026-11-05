package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func aiSnapshot(t *testing.T) DailySnapshot {
	t.Helper()
	req, err := newHistoryRequest("AAPL", testClockTime)
	if err != nil {
		t.Fatal(err)
	}
	bars, err := validateDailyBars(req, historyFixture(req))
	if err != nil {
		t.Fatal(err)
	}
	return DailySnapshot{
		Clock:      MarketClock{Timestamp: testClockTime},
		Account:    AccountSnapshot{ID: "paper-account", Status: "ACTIVE", Currency: "USD", Equity: "10000", Cash: "9000", BuyingPower: "18000"},
		Positions:  []Position{{Symbol: "AAPL", Side: "long", Quantity: "2.5", AverageEntryPrice: "180.25", MarketValue: "500"}, {Symbol: "MSFT", Side: "long", Quantity: "1", AverageEntryPrice: "450", MarketValue: "500"}},
		OpenOrders: []OpenOrder{{ID: "pending-1", Symbol: "AAPL", Side: "buy", Quantity: "1", Status: "accepted"}},
		History:    DailyHistory{Symbol: "AAPL", Feed: "iex", Adjustment: "split", Bars: bars},
	}
}

func aiDecision() Decision {
	holding := 5
	return Decision{Action: DecisionHold, Symbol: "AAPL", Rationale: "Wait for a suitable opportunity; keep exposure unchanged.", ExpectedHoldingSessions: &holding,
		ReassessmentConditions: []string{"Reassess if the next daily close falls below 95."}, NextReviewDate: "2026-11-02"}
}

func aiResponseBody(t *testing.T, d Decision) string {
	t.Helper()
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	response := map[string]any{"model": approvedModel, "service_tier": "default", "status": "completed", "usage": map[string]int{"input_tokens": 1000, "output_tokens": 500},
		"output": []any{map[string]any{"type": "message", "content": []any{map[string]string{"type": "output_text", "text": string(data)}}}}}
	b, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func mustAIPolicy(t *testing.T, journal RecommendationJournal, transport http.RoundTripper) *OpenAIPolicy {
	t.Helper()
	p, err := NewOpenAIPolicy("fake-openai-key", approvedModel, defaultCycleBudgetNanodollars, &http.Client{Transport: transport}, journal)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewAIPolicyRejectsUnreviewedConfiguration(t *testing.T) {
	for _, tt := range []struct {
		key, model string
		budget     int64
		journal    RecommendationJournal
	}{
		{"", approvedModel, 50000000, &testJournal{}},
		{"fake-key", "unknown", 50000000, &testJournal{}},
		{"fake-key", approvedModel, 0, &testJournal{}},
		{"fake-key", approvedModel, -1, &testJournal{}},
		{"fake-key", approvedModel, 50000000, nil},
	} {
		if _, err := NewOpenAIPolicy(tt.key, tt.model, tt.budget, nil, tt.journal); err == nil {
			t.Fatal("invalid configuration accepted")
		}
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	p, err := NewOpenAIPolicy("fake-key", approvedModel, 50000000, client, &testJournal{})
	if err != nil || p.client.Timeout != 60*time.Second || client.Timeout != 5*time.Minute {
		t.Fatal("timeout bound missing or caller client mutated")
	}
}

func TestAIRecommendationCyclePersistsAndReusesContext(t *testing.T) {
	snapshot := aiSnapshot(t)
	journal := FileRecommendationJournal{Directory: filepath.Join(t.TempDir(), "journal")}
	// Seed another holding's history; the model must see it without recommending MSFT.
	msft := snapshot
	msft.History.Symbol = "MSFT"
	seed := journalRecord(t, msft, aiDecision())
	seed.Recommendation.Symbol = "MSFT"
	if err := journal.Save(seed); err != nil {
		t.Fatal(err)
	}
	var previousID string
	calls := 0
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		if req.URL.String() != openAIResponsesURL || req.Method != http.MethodPost || req.Header.Get("Authorization") != "Bearer fake-openai-key" {
			t.Fatal("incorrect OpenAI endpoint or authentication")
		}
		data, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte("fake-openai-key")) {
			t.Fatal("credential leaked into model input")
		}
		var request struct {
			Model           string                           `json:"model"`
			MaxOutputTokens int64                            `json:"max_output_tokens"`
			Store           bool                             `json:"store"`
			ServiceTier     string                           `json:"service_tier"`
			Truncation      string                           `json:"truncation"`
			Tools           []any                            `json:"tools"`
			ToolChoice      string                           `json:"tool_choice"`
			Reasoning       map[string]string                `json:"reasoning"`
			Input           []struct{ Role, Content string } `json:"input"`
			Text            struct {
				Format struct {
					Type   string
					Strict bool
					Schema map[string]any
				}
			} `json:"text"`
		}
		if err := json.Unmarshal(data, &request); err != nil {
			t.Fatal(err)
		}
		if request.Model != approvedModel || request.MaxOutputTokens != 6000 || request.Store || request.ServiceTier != "default" || request.Truncation != "disabled" || len(request.Tools) != 0 || request.ToolChoice != "none" || request.Reasoning["effort"] != "low" {
			t.Fatal("request omitted program limits")
		}
		if len(request.Input) != 2 || request.Input[0].Role != "developer" || request.Input[0].Content != recommendationInstructions || request.Text.Format.Type != "json_schema" || !request.Text.Format.Strict || request.Text.Format.Schema["additionalProperties"] != false {
			t.Fatal("missing instructions/schema")
		}
		var input RecommendationContext
		if err := json.Unmarshal([]byte(request.Input[1].Content), &input); err != nil {
			t.Fatal(err)
		}
		p := input.Snapshot.Positions[0]
		if input.Symbol != "AAPL" || p.Quantity != "2.5" || p.AverageEntryPrice != "180.25" || p.PortfolioWeight != "0.05" || input.Snapshot.Account.Cash != "9000" || input.Snapshot.Account.BuyingPower != "18000" || len(input.Snapshot.OpenOrders) != 1 || len(input.Snapshot.History.Bars) != 20 || input.Snapshot.History.Feed != "iex" || input.Snapshot.History.Adjustment != "split" || !input.Snapshot.Clock.Timestamp.Equal(testClockTime) {
			t.Fatal("required market/portfolio context missing")
		}
		if len(input.Previous) != 2 || input.Previous["MSFT"] == nil || input.Previous["MSFT"].Decision.Rationale == "" || input.Previous["MSFT"].Decision.ExpectedHoldingSessions == nil || len(input.Previous["MSFT"].Decision.ReassessmentConditions) == 0 {
			t.Fatal("held-symbol history missing")
		}
		if (calls == 1 && input.Previous["AAPL"] != nil) || (calls == 2 && (input.Previous["AAPL"] == nil || input.Previous["AAPL"].ID != previousID)) {
			t.Fatal("wrong prior recommendation after restart")
		}
		entries, err := os.ReadDir(journal.accountDirectory(snapshot.Account.ID))
		if err != nil {
			t.Fatal(err)
		}
		reserved := 0
		for _, e := range entries {
			r, err := readRecommendationRecord(filepath.Join(journal.accountDirectory(snapshot.Account.ID), e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if r.Status == "reserved" {
				reserved++
				if r.ReservedNanodollars > r.BudgetNanodollars || r.InputTokenUpperBound > 12000 || r.Usage != nil {
					t.Fatal("reservation not persisted before request")
				}
			}
		}
		if reserved != 1 {
			t.Fatal("missing reservation")
		}
		d := aiDecision()
		if calls == 2 {
			d.Action = DecisionSell
			d.PreviousRecommendationID = &previousID
			d.ChangeExplanation = "The new snapshot changes the balance of downside and opportunity."
		}
		return httpResponse(200, aiResponseBody(t, d)), nil
	})
	for i := 0; i < 2; i++ {
		// Reconstruct both policy and journal to simulate independent CLI runs.
		policy := mustAIPolicy(t, FileRecommendationJournal{Directory: journal.Directory}, transport)
		broker := &fakeBroker{clock: snapshot.Clock, account: snapshot.Account, positions: snapshot.Positions, openOrders: snapshot.OpenOrders}
		var out bytes.Buffer
		if err := run(context.Background(), &out, broker, policy, &fakeMarketData{}, "AAPL", defaultPlannerLimits()); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "execution=disabled risk_limits=preview_configured") || !strings.Contains(out.String(), `"execution":"disabled"`) || !strings.Contains(out.String(), `"rationale":`) || !strings.Contains(out.String(), `"cost_upper_nanodollars":`) {
			t.Fatal("missing inspectable recommendation/report")
		}
		latest, err := journal.Latest(snapshot.Account.ID, []string{"AAPL"})
		if err != nil {
			t.Fatal(err)
		}
		if latest["AAPL"] == nil || latest["AAPL"].ID == previousID {
			t.Fatal("recommendation not persisted")
		}
		previousID = latest["AAPL"].ID
	}
	if calls != 2 || snapshot.Positions[0].PortfolioWeight != "" {
		t.Fatal("unexpected calls or mutation of caller portfolio")
	}
}

func TestAIBudgetBoundariesAndNoCallOnRejectedInput(t *testing.T) {
	maxBytes := maxAIInputTokens - aiFramingTokenAllowance
	upper, cost, err := reserveAICost(maxBytes, 36000000)
	if err != nil || upper != 12000 || cost != 36000000 {
		t.Fatal("incorrect maximum reservation")
	}
	for _, pair := range [][2]int64{{maxBytes, 35999999}, {maxBytes + 1, 50000000}, {math.MaxInt64, math.MaxInt64}, {-1, 50000000}} {
		if _, _, err := reserveAICost(pair[0], pair[1]); !errors.Is(err, ErrAIBudget) {
			t.Fatal("invalid bound accepted")
		}
	}
	for _, kind := range []string{"budget", "oversized context"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			j := &testJournal{}
			p := mustAIPolicy(t, j, roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected call") }))
			s := aiSnapshot(t)
			if kind == "budget" {
				p.budgetNanodollars = 1
			} else {
				s.OpenOrders[0].ID = strings.Repeat("界", 12000)
			}
			_, err := p.Decide(context.Background(), s)
			if !errors.Is(err, ErrAIBudget) || calls != 0 || len(j.saved) != 0 {
				t.Fatalf("error=%v calls=%d records=%d", err, calls, len(j.saved))
			}
		})
	}
}

type testJournal struct {
	previous   map[string]*PreviousRecommendation
	saved      []RecommendationRecord
	failSaveAt int
	loadError  error
}

func (j *testJournal) Latest(_ string, symbols []string) (map[string]*PreviousRecommendation, error) {
	if j.loadError != nil {
		return nil, j.loadError
	}
	m := make(map[string]*PreviousRecommendation)
	for _, s := range symbols {
		m[s] = j.previous[s]
	}
	return m, nil
}
func (j *testJournal) Save(r RecommendationRecord) error {
	if j.failSaveAt == len(j.saved)+1 {
		return errors.New("journal write failed")
	}
	j.saved = append(j.saved, r)
	return nil
}

func TestAIFailureModesAreRecordedWithoutRetries(t *testing.T) {
	base := aiResponseBody(t, aiDecision())
	for _, tt := range []struct {
		name   string
		status int
		body   string
		want   error
		usage  bool
	}{
		{"rate limit", 429, `{"message":"fake-openai-key"}`, ErrAIAPI, false},
		{"server error", 500, `{}`, ErrAIAPI, false},
		{"malformed JSON", 200, `{`, ErrAIResponse, false},
		{"incomplete", 200, strings.Replace(base, `"status":"completed"`, `"status":"incomplete"`, 1), ErrAIIncomplete, true},
		{"refusal", 200, strings.Replace(base, `"type":"output_text"`, `"type":"refusal"`, 1), ErrAIRefusal, true},
		{"wrong model", 200, strings.Replace(base, approvedModel, "unknown-model", 1), ErrAIResponse, true},
		{"wrong tier", 200, strings.Replace(base, `"service_tier":"default"`, `"service_tier":"priority"`, 1), ErrAIResponse, true},
		{"missing usage", 200, strings.Replace(base, `"usage":`, `"ignored_usage":`, 1), ErrAIResponse, false},
		{"over output limit", 200, strings.Replace(base, `"output_tokens":500`, `"output_tokens":6001`, 1), ErrAIResponse, false},
		{"oversized response", 200, strings.Repeat("x", maxAIResponseBytes+1), ErrAIResponse, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			j := &testJournal{}
			calls := 0
			var body *trackedBody
			p := mustAIPolicy(t, j, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				r := httpResponse(tt.status, tt.body)
				body = &trackedBody{Reader: r.Body}
				r.Body = body
				return r, nil
			}))
			_, err := p.Decide(context.Background(), aiSnapshot(t))
			if !errors.Is(err, tt.want) || calls != 1 || !body.closed || len(j.saved) != 2 || j.saved[1].Status != "failed" || (j.saved[1].Usage != nil) != tt.usage {
				t.Fatalf("error=%v calls=%d records=%d", err, calls, len(j.saved))
			}
			if strings.Contains(err.Error(), "fake-openai-key") || strings.Contains(j.saved[1].Failure, "fake-openai-key") {
				t.Fatal("upstream error leaked credential")
			}
		})
	}
}

func TestAIJournalFailuresPreventOrInvalidateResult(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(string(rune('0'+failAt)), func(t *testing.T) {
			j := &testJournal{failSaveAt: failAt}
			calls := 0
			p := mustAIPolicy(t, j, roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls++
				return httpResponse(200, aiResponseBody(t, aiDecision())), nil
			}))
			d, err := p.Decide(context.Background(), aiSnapshot(t))
			if err == nil || !strings.Contains(err.Error(), "journal") || d.RecordID != "" || calls != failAt-1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
		})
	}
	j := &testJournal{loadError: errors.New("corrupt journal")}
	p := mustAIPolicy(t, j, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("called with unreadable history")
		return nil, nil
	}))
	if _, err := p.Decide(context.Background(), aiSnapshot(t)); err == nil {
		t.Fatal("ignored history error")
	}
}

func TestAICancellationTimeoutAndRedirectNeverRetry(t *testing.T) {
	for _, kind := range []string{"canceled before call", "deadline", "timeout", "redirect"} {
		t.Run(kind, func(t *testing.T) {
			j := &testJournal{}
			calls := 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := mustAIPolicy(t, j, roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				if kind == "redirect" {
					r := httpResponse(307, "")
					r.Header.Set("Location", "https://elsewhere.test/v1/responses")
					return r, nil
				}
				if kind == "deadline" {
					<-req.Context().Done()
					return nil, req.Context().Err()
				}
				return nil, timeoutError{}
			}))
			if kind == "canceled before call" {
				cancel()
			}
			if kind == "deadline" {
				var c context.CancelFunc
				ctx, c = context.WithTimeout(ctx, 5*time.Millisecond)
				defer c()
			}
			_, err := p.Decide(ctx, aiSnapshot(t))
			if err == nil || calls > 1 {
				t.Fatalf("error=%v calls=%d", err, calls)
			}
			if kind == "canceled before call" {
				if calls != 0 || len(j.saved) != 0 || !errors.Is(err, context.Canceled) {
					t.Fatal("pre-canceled request called API")
				}
			} else if len(j.saved) != 2 || j.saved[1].Usage != nil {
				t.Fatal("unknown cost was not recorded")
			}
			if (kind == "deadline" || kind == "timeout") && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("timeout classification lost")
			}
		})
	}
}

type timeoutError struct{}

func (timeoutError) Error() string { return "timeout" }
func (timeoutError) Timeout() bool { return true }

func TestAIInvalidRecommendationDoesNotBecomeHold(t *testing.T) {
	j := &testJournal{}
	d := aiDecision()
	d.Symbol = "MSFT"
	p := mustAIPolicy(t, j, roundTripFunc(func(*http.Request) (*http.Response, error) { return httpResponse(200, aiResponseBody(t, d)), nil }))
	got, err := p.Decide(context.Background(), aiSnapshot(t))
	if err == nil || got.Action != "" || j.saved[1].Status != "invalid" || j.saved[1].Recommendation == nil || j.saved[1].Validation.Execution != "disabled" {
		t.Fatal("invalid decision was accepted or lost")
	}
}
