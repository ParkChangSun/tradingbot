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
