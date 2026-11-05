package main

import (
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Setenv(tradingSymbolEnv, "aapl")
	t.Setenv(apiKeyEnv, "test-key")
	t.Setenv(apiSecretEnv, "test-secret")

	config, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	if config.TradingSymbol != "AAPL" {
		t.Errorf("symbol = %q", config.TradingSymbol)
	}
	if config.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want %q", config.APIKey, "test-key")
	}
	if config.APISecret != "test-secret" {
		t.Errorf("APISecret = %q, want %q", config.APISecret, "test-secret")
	}
}

func TestLoadConfigReportsAllMissingRequiredValues(t *testing.T) {
	t.Setenv(tradingSymbolEnv, " ")
	t.Setenv(apiKeyEnv, "")
	t.Setenv(apiSecretEnv, "   ")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig returned nil error")
	}

	for _, want := range []string{
		"invalid configuration",
		apiKeyEnv + " is required",
		apiSecretEnv + " is required",
		tradingSymbolEnv + " is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestLoadConfigTrimsValues(t *testing.T) {
	t.Setenv(tradingSymbolEnv, " brk.b ")
	t.Setenv(apiKeyEnv, "  test-key  ")
	t.Setenv(apiSecretEnv, "  test-secret  ")

	config, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	if config.APIKey != "test-key" {
		t.Errorf("APIKey = %q, want %q", config.APIKey, "test-key")
	}
	if config.APISecret != "test-secret" {
		t.Errorf("APISecret = %q, want %q", config.APISecret, "test-secret")
	}
}

func TestLoadConfigNormalizesSymbol(t *testing.T) {
	t.Setenv(apiKeyEnv, "key")
	t.Setenv(apiSecretEnv, "secret")
	t.Setenv(tradingSymbolEnv, " brk.b ")
	config, err := loadConfig()
	if err != nil || config.TradingSymbol != "BRK.B" {
		t.Fatalf("symbol=%q error=%v", config.TradingSymbol, err)
	}
}

func TestAIConfigDefaultsAndExplicitOptIn(t *testing.T) {
	t.Setenv(apiKeyEnv, "fake-paper-key")
	t.Setenv(apiSecretEnv, "fake-paper-secret")
	t.Setenv(tradingSymbolEnv, "AAPL")
	t.Setenv("DECISION_POLICY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("OPENAI_CYCLE_BUDGET_USD", "")
	c, err := loadConfig()
	if err != nil || c.DecisionPolicy != "hold" || c.OpenAIModel != approvedModel || c.CycleBudgetNanodollars != 50000000 || c.RecommendationDirectory != ".tradingbot-recommendations" {
		t.Fatalf("defaults error=%v", err)
	}
	t.Setenv("DECISION_POLICY", "openai")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "OPENAI_API_KEY") {
		t.Fatal("missing AI key accepted")
	}
	t.Setenv("OPENAI_API_KEY", " fake-openai-key ")
	t.Setenv("OPENAI_CYCLE_BUDGET_USD", "0.04")
	c, err = loadConfig()
	if err != nil || c.OpenAIKey != "fake-openai-key" || c.CycleBudgetNanodollars != 40000000 {
		t.Fatalf("AI config error=%v", err)
	}
	t.Setenv("OPENAI_MODEL", "unreviewed")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "pricing") {
		t.Fatal("unknown model accepted")
	}
	t.Setenv("OPENAI_MODEL", approvedModel)
	t.Setenv("OPENAI_CYCLE_BUDGET_USD", "bad-budget")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "OPENAI_CYCLE_BUDGET_USD") {
		t.Fatal("invalid budget accepted")
	}
	t.Setenv("DECISION_POLICY", "invalid-policy")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "DECISION_POLICY") {
		t.Fatal("unknown policy accepted")
	}
}

func TestCycleBudgetParsing(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  int64
	}{
		{"0.05", 50000000}, {"0.000000001", 1}, {"1", 1000000000}, {"9223372036.854775807", 9223372036854775807},
	} {
		got, err := parseCycleBudget(tt.value)
		if err != nil || got != tt.want {
			t.Fatalf("value=%s got=%d error=%v", tt.value, got, err)
		}
	}
	for _, value := range []string{"0", "-0.01", "NaN", "Infinity", "0.0000000001", "9223372036.854775808"} {
		if _, err := parseCycleBudget(value); err == nil {
			t.Fatalf("invalid budget %s accepted", value)
		}
	}
}

func TestPlannerConfigDefaultsOverridesAndInvalidLimits(t *testing.T) {
	t.Setenv(apiKeyEnv, "fake-paper-key")
	t.Setenv(apiSecretEnv, "fake-paper-secret")
	t.Setenv(tradingSymbolEnv, "AAPL")
	t.Setenv("DECISION_POLICY", "hold")
	for _, name := range []string{"PLANNER_MAX_POSITION_WEIGHT", "PLANNER_MAX_INVESTED_WEIGHT", "PLANNER_MAX_ORDER_WEIGHT", "PLANNER_QUOTE_MAX_AGE_SECONDS"} {
		t.Setenv(name, "")
	}
	c, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if c.PlannerLimits.MaxPositionWeight.String() != "0.1" || c.PlannerLimits.MaxInvestedWeight.String() != "0.8" || c.PlannerLimits.MaxOrderWeight.String() != "0.02" || c.PlannerLimits.QuoteMaxAgeSeconds != 60 {
		t.Fatal("accepted defaults not configured")
	}
	for _, tt := range []struct{ name, value string }{
		{"PLANNER_MAX_POSITION_WEIGHT", "0"}, {"PLANNER_MAX_POSITION_WEIGHT", "-0.1"},
		{"PLANNER_MAX_POSITION_WEIGHT", "bad"}, {"PLANNER_MAX_POSITION_WEIGHT", "0.01"},
		{"PLANNER_MAX_ORDER_WEIGHT", "0.11"}, {"PLANNER_MAX_INVESTED_WEIGHT", "1.1"},
		{"PLANNER_MAX_INVESTED_WEIGHT", "0.05"}, {"PLANNER_QUOTE_MAX_AGE_SECONDS", "0"},
		{"PLANNER_QUOTE_MAX_AGE_SECONDS", "-1"}, {"PLANNER_QUOTE_MAX_AGE_SECONDS", "bad"},
		{"PLANNER_QUOTE_MAX_AGE_SECONDS", "9223372036854775807"},
	} {
		t.Run(tt.name+"="+tt.value, func(t *testing.T) {
			t.Setenv(tt.name, tt.value)
			if _, err := loadConfig(); err == nil {
				t.Fatal("invalid planner limits accepted")
			}
		})
	}
	t.Setenv("PLANNER_MAX_POSITION_WEIGHT", "0.05")
	t.Setenv("PLANNER_MAX_INVESTED_WEIGHT", "0.60")
	t.Setenv("PLANNER_MAX_ORDER_WEIGHT", "0.01")
	t.Setenv("PLANNER_QUOTE_MAX_AGE_SECONDS", "30")
	c, err = loadConfig()
	if err != nil || c.PlannerLimits.MaxOrderWeight.String() != "0.01" || c.PlannerLimits.QuoteMaxAgeSeconds != 30 {
		t.Fatalf("operator configuration error=%v", err)
	}
}
