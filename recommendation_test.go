package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func journalRecord(t *testing.T, snapshot DailySnapshot, d Decision) RecommendationRecord {
	t.Helper()
	id, err := newRecordID()
	if err != nil {
		t.Fatal(err)
	}
	input, err := buildRecommendationContext(snapshot, map[string]*PreviousRecommendation{snapshot.History.Symbol: nil})
	if err != nil {
		t.Fatal(err)
	}
	return RecommendationRecord{Version: 1, ID: id, CreatedAt: time.Now().UTC(), AccountID: snapshot.Account.ID, Symbol: snapshot.History.Symbol, Model: approvedModel, PromptVersion: promptVersion, Input: input, Status: "validated", Recommendation: &d,
		Validation: executionDisabled("valid", snapshot), BudgetNanodollars: defaultCycleBudgetNanodollars, ReservedNanodollars: 36000000, InputTokenUpperBound: 12000, Usage: &TokenUsage{InputTokens: 1000, OutputTokens: 500, CostUpperNanodollars: 3000000}}
}

func TestRecommendationValidation(t *testing.T) {
	input, err := buildRecommendationContext(aiSnapshot(t), map[string]*PreviousRecommendation{"AAPL": nil, "MSFT": nil})
	if err != nil {
		t.Fatal(err)
	}
	for _, action := range []DecisionAction{DecisionBuy, DecisionSell, DecisionHold} {
		d := aiDecision()
		d.Action = action
		if err := validateRecommendation(d, input); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		name   string
		change func(*Decision)
		want   string
	}{
		{"wrong symbol", func(d *Decision) { d.Symbol = "MSFT" }, "symbol"},
		{"unknown action", func(d *Decision) { d.Action = "trade" }, "action"},
		{"empty rationale", func(d *Decision) { d.Rationale = " " }, "rationale"},
		{"missing holding period", func(d *Decision) { d.ExpectedHoldingSessions = nil }, "holding"},
		{"zero holding period", func(d *Decision) { n := 0; d.ExpectedHoldingSessions = &n }, "positive"},
		{"negative holding period", func(d *Decision) { n := -1; d.ExpectedHoldingSessions = &n }, "positive"},
		{"missing conditions", func(d *Decision) { d.ReassessmentConditions = nil }, "conditions"},
		{"blank condition", func(d *Decision) { d.ReassessmentConditions = []string{" "} }, "condition"},
		{"invalid review date", func(d *Decision) { d.NextReviewDate = "tomorrow" }, "YYYY-MM-DD"},
		{"same review date", func(d *Decision) { d.NextReviewDate = "2026-10-30" }, "follow"},
		{"past review date", func(d *Decision) { d.NextReviewDate = "2026-10-29" }, "follow"},
		{"invented prior reference", func(d *Decision) { id := "invented"; d.PreviousRecommendationID = &id }, "unexpected previous"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := aiDecision()
			tt.change(&d)
			if err := validateRecommendation(d, input); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	// Staying out without a position has no expected holding period.
	input.Snapshot.Positions = nil
	d := aiDecision()
	d.ExpectedHoldingSessions = nil
	if err := validateRecommendation(d, input); err != nil {
		t.Fatal(err)
	}
	d.Action = DecisionBuy
	if err := validateRecommendation(d, input); err == nil {
		t.Fatal("trade with null horizon accepted")
	}
	// Compare dates in New York, even if UTC is already tomorrow.
	input.Snapshot.Clock.Timestamp = time.Date(2026, 10, 31, 2, 0, 0, 0, time.UTC)
	d = aiDecision()
	d.NextReviewDate = "2026-10-31"
	if err := validateRecommendation(d, input); err != nil {
		t.Fatal(err)
	}
}

func TestChangedRecommendationRequiresExplanationAndExactReference(t *testing.T) {
	input, err := buildRecommendationContext(aiSnapshot(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	input.Previous = map[string]*PreviousRecommendation{"AAPL": {ID: "prior", Decision: aiDecision()}}
	for _, tt := range []struct {
		name   string
		change func(*Decision)
	}{
		{"action", func(d *Decision) { d.Action = DecisionSell }},
		{"rationale", func(d *Decision) { d.Rationale = "Different assessment of downside risk." }},
		{"holding", func(d *Decision) { n := 10; d.ExpectedHoldingSessions = &n }},
		{"conditions", func(d *Decision) { d.ReassessmentConditions = []string{"Review if daily close exceeds 110."} }},
		{"review date", func(d *Decision) { d.NextReviewDate = "2026-11-03" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d := aiDecision()
			id := "prior"
			d.PreviousRecommendationID = &id
			if err := validateRecommendation(d, input); err != nil {
				t.Fatal(err)
			}
			tt.change(&d)
			if err := validateRecommendation(d, input); err == nil || !strings.Contains(err.Error(), "explanation") {
				t.Fatal("change without explanation accepted")
			}
			d.ChangeExplanation = "Updated market and portfolio context changed the recommendation."
			if err := validateRecommendation(d, input); err != nil {
				t.Fatal(err)
			}
			d.PreviousRecommendationID = nil
			if err := validateRecommendation(d, input); err == nil {
				t.Fatal("missing prior ID accepted")
			}
			wrong := "wrong"
			d.PreviousRecommendationID = &wrong
			if err := validateRecommendation(d, input); err == nil {
				t.Fatal("wrong prior ID accepted")
			}
		})
	}
}

func TestRecommendationDecoderRequiresExactSchema(t *testing.T) {
	b, err := json.Marshal(aiDecision())
	if err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(map[string]any){
		func(m map[string]any) { delete(m, "change_explanation") },
		func(m map[string]any) { delete(m, "previous_recommendation_id") },
		func(m map[string]any) { m["max_output_tokens"] = 90000 },
		func(m map[string]any) { m["expected_holding_sessions"] = 1.5 },
		func(m map[string]any) { m["reassessment_conditions"] = nil },
	} {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		mutation(m)
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := decodeRecommendation(data); err == nil {
			t.Fatal("invalid schema accepted")
		}
	}
	if _, err := decodeRecommendation(append(b, []byte(` {}`)...)); err == nil {
		t.Fatal("trailing JSON accepted")
	}
	if _, err := decodeRecommendation([]byte(strings.Replace(string(b), `"action":"hold"`, `"action":"buy","action":"hold"`, 1))); err == nil {
		t.Fatal("duplicate key accepted")
	}
}

func TestPortfolioContextRejectsMissingValuesAndUsesDecimalWeights(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func(*DailySnapshot)
	}{
		{"account", func(s *DailySnapshot) { s.Account.ID = "" }},
		{"currency", func(s *DailySnapshot) { s.Account.Currency = "EUR" }},
		{"equity missing", func(s *DailySnapshot) { s.Account.Equity = "" }},
		{"equity zero", func(s *DailySnapshot) { s.Account.Equity = "0" }},
		{"equity negative", func(s *DailySnapshot) { s.Account.Equity = "-1" }},
		{"cash", func(s *DailySnapshot) { s.Account.Cash = "" }},
		{"buying power", func(s *DailySnapshot) { s.Account.BuyingPower = "bad" }},
		{"market value", func(s *DailySnapshot) { s.Positions[0].MarketValue = "" }},
		{"entry price", func(s *DailySnapshot) { s.Positions[0].AverageEntryPrice = "0" }},
		{"quantity", func(s *DailySnapshot) { s.Positions[0].Quantity = "" }},
		{"full open-order page", func(s *DailySnapshot) { s.OpenOrders = make([]OpenOrder, 500) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := aiSnapshot(t)
			tt.change(&s)
			calls := 0
			p := mustAIPolicy(t, &testJournal{}, roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, nil }))
			if _, err := p.Decide(context.Background(), s); err == nil || calls != 0 {
				t.Fatal("invalid context sent to API")
			}
		})
	}
	s := aiSnapshot(t)
	s.Account.Equity = "0.3"
	s.Positions[0].MarketValue = "0.1"
	s.Positions[1].MarketValue = "-0.1"
	s.Positions[1].Side = "short"
	input, err := buildRecommendationContext(s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if input.Snapshot.Positions[0].PortfolioWeight != "0.333333333333" || input.Snapshot.Positions[1].PortfolioWeight != "-0.333333333333" {
		t.Fatal("incorrect signed decimal weights")
	}
}

func TestFileJournalHistoryIsolationCorruptionAndPermissions(t *testing.T) {
	j := FileRecommendationJournal{Directory: filepath.Join(t.TempDir(), "journal")}
	s := aiSnapshot(t)
	r := journalRecord(t, s, aiDecision())
	latest, err := j.Latest(s.Account.ID, []string{"AAPL", "MSFT"})
	if err != nil || len(latest) != 2 || latest["AAPL"] != nil {
		t.Fatal("missing history is not explicit")
	}
	if err := j.Save(r); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(j.accountDirectory(r.AccountID), r.ID+".json")
	for _, p := range []struct {
		path string
		mode os.FileMode
	}{{j.Directory, 0700}, {j.accountDirectory(r.AccountID), 0700}, {file, 0600}} {
		info, err := os.Stat(p.path)
		if err != nil || info.Mode().Perm() != p.mode {
			t.Fatalf("permissions: %s %v", p.path, err)
		}
	}
	other, err := j.Latest("other-account", []string{"AAPL"})
	if err != nil || other["AAPL"] != nil {
		t.Fatal("account history leaked")
	}
	// A later failed/reserved attempt must not replace the previous valid recommendation.
	for _, status := range []string{"failed", "reserved", "invalid"} {
		next := r
		next.ID, err = newRecordID()
		if err != nil {
			t.Fatal(err)
		}
		next.CreatedAt = next.CreatedAt.Add(time.Hour)
		next.Status = status
		next.Recommendation = nil
		if err := j.Save(next); err != nil {
			t.Fatal(err)
		}
	}
	latest, err = j.Latest(s.Account.ID, []string{"AAPL", "MSFT"})
	if err != nil || latest["AAPL"] == nil || latest["AAPL"].ID != r.ID || latest["MSFT"] != nil {
		t.Fatal("wrong latest valid recommendation")
	}
	if err := os.WriteFile(file, []byte(`{"version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Latest(s.Account.ID, []string{"AAPL"}); err == nil {
		t.Fatal("corrupt history treated as missing")
	}
	if err := os.WriteFile(file, []byte(`{`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := j.Latest(s.Account.ID, []string{"AAPL"}); err == nil {
		t.Fatal("malformed JSON treated as missing")
	}
}

func TestFileJournalWriteErrorsAreReported(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "file")
	if err := os.WriteFile(path, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	r := journalRecord(t, aiSnapshot(t), aiDecision())
	if err := (FileRecommendationJournal{Directory: path}).Save(r); err == nil {
		t.Fatal("journal storage failure ignored")
	}
}

func TestPrePlannerJournalRemainsReadable(t *testing.T) {
	s := aiSnapshot(t)
	j := FileRecommendationJournal{Directory: filepath.Join(t.TempDir(), "journal")}
	r := journalRecord(t, s, aiDecision())
	r.PromptVersion = "loss-aware-v1"
	r.Input.Execution = "disabled: risk limits and order sizing are unset"
	r.Validation.Reasons = []string{"risk limits and order sizing are unset; order submission is not implemented"}
	if err := j.Save(r); err != nil {
		t.Fatal(err)
	}
	previous, err := j.Latest(s.Account.ID, []string{"AAPL"})
	if err != nil || previous["AAPL"] == nil || previous["AAPL"].ID != r.ID || previous["AAPL"].PromptVersion != "loss-aware-v1" {
		t.Fatalf("legacy journal unavailable: %v", err)
	}
}
