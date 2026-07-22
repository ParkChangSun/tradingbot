package main

import (
	"strings"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	t.Setenv(apiSecretEnv, "test-secret")
	t.Setenv(apiBaseURLEnv, "")

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
	if config.APIBaseURL != defaultAPIBaseURL {
		t.Errorf("APIBaseURL = %q, want %q", config.APIBaseURL, defaultAPIBaseURL)
	}
}

func TestLoadConfigUsesConfiguredBaseURL(t *testing.T) {
	t.Setenv(apiKeyEnv, "test-key")
	t.Setenv(apiSecretEnv, "test-secret")
	t.Setenv(apiBaseURLEnv, "https://example.test")

	config, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	if config.APIBaseURL != "https://example.test" {
		t.Errorf("APIBaseURL = %q, want %q", config.APIBaseURL, "https://example.test")
	}
}

func TestLoadConfigReportsAllMissingRequiredValues(t *testing.T) {
	t.Setenv(apiKeyEnv, "")
	t.Setenv(apiSecretEnv, "   ")
	t.Setenv(apiBaseURLEnv, "")

	_, err := loadConfig()
	if err == nil {
		t.Fatal("loadConfig returned nil error")
	}

	for _, want := range []string{
		"invalid configuration",
		apiKeyEnv + " is required",
		apiSecretEnv + " is required",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not contain %q", err, want)
		}
	}
}

func TestLoadConfigTrimsValues(t *testing.T) {
	t.Setenv(apiKeyEnv, "  test-key  ")
	t.Setenv(apiSecretEnv, "  test-secret  ")
	t.Setenv(apiBaseURLEnv, "  https://example.test  ")

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
	if config.APIBaseURL != "https://example.test" {
		t.Errorf("APIBaseURL = %q, want %q", config.APIBaseURL, "https://example.test")
	}
}
