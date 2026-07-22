package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	apiKeyEnv     = "APCA_API_KEY_ID"
	apiSecretEnv  = "APCA_API_SECRET_KEY"
	apiBaseURLEnv = "APCA_API_BASE_URL"

	defaultAPIBaseURL = "https://paper-api.alpaca.markets"
)

type Config struct {
	APIKey     string
	APISecret  string
	APIBaseURL string
}

func loadConfig() (Config, error) {
	config := Config{
		APIKey:     strings.TrimSpace(os.Getenv(apiKeyEnv)),
		APISecret:  strings.TrimSpace(os.Getenv(apiSecretEnv)),
		APIBaseURL: strings.TrimSpace(os.Getenv(apiBaseURLEnv)),
	}

	if config.APIBaseURL == "" {
		config.APIBaseURL = defaultAPIBaseURL
	}

	var validationErrors []error
	if config.APIKey == "" {
		validationErrors = append(validationErrors, fmt.Errorf("%s is required", apiKeyEnv))
	}
	if config.APISecret == "" {
		validationErrors = append(validationErrors, fmt.Errorf("%s is required", apiSecretEnv))
	}

	if err := errors.Join(validationErrors...); err != nil {
		return Config{}, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}
