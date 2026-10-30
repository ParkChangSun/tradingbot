package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	apiKeyEnv        = "APCA_API_KEY_ID"
	apiSecretEnv     = "APCA_API_SECRET_KEY"
	tradingSymbolEnv = "TRADING_SYMBOL"
)

type Config struct {
	APIKey        string
	APISecret     string
	TradingSymbol string
}

func loadConfig() (Config, error) {
	config := Config{
		APIKey:        strings.TrimSpace(os.Getenv(apiKeyEnv)),
		APISecret:     strings.TrimSpace(os.Getenv(apiSecretEnv)),
		TradingSymbol: strings.ToUpper(strings.TrimSpace(os.Getenv(tradingSymbolEnv))),
	}

	var validationErrors []error
	if config.APIKey == "" {
		validationErrors = append(validationErrors, fmt.Errorf("%s is required", apiKeyEnv))
	}
	if config.APISecret == "" {
		validationErrors = append(validationErrors, fmt.Errorf("%s is required", apiSecretEnv))
	}

	if config.TradingSymbol == "" {
		validationErrors = append(validationErrors, fmt.Errorf("%s is required", tradingSymbolEnv))
	}

	if err := errors.Join(validationErrors...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}
