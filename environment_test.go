package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEnvironmentFileLoadsValues(t *testing.T) {
	const name = "TRADINGBOT_TEST_DOTENV_LOAD"
	unsetEnvForTest(t, name)

	filename := writeEnvFile(t, name+"=from-file\n")
	if err := loadEnvironmentFile(filename); err != nil {
		t.Fatalf("loadEnvironmentFile returned error: %v", err)
	}

	if got := os.Getenv(name); got != "from-file" {
		t.Errorf("%s = %q, want %q", name, got, "from-file")
	}
}

func TestLoadEnvironmentFilePreservesExistingValues(t *testing.T) {
	tests := []struct {
		name          string
		existingValue string
	}{
		{name: "non-empty", existingValue: "from-process"},
		{name: "empty", existingValue: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const name = "TRADINGBOT_TEST_DOTENV_PRECEDENCE"
			t.Setenv(name, tt.existingValue)

			filename := writeEnvFile(t, name+"=from-file\n")
			if err := loadEnvironmentFile(filename); err != nil {
				t.Fatalf("loadEnvironmentFile returned error: %v", err)
			}

			if got := os.Getenv(name); got != tt.existingValue {
				t.Errorf("%s = %q, want existing value %q", name, got, tt.existingValue)
			}
		})
	}
}

func TestLoadEnvironmentFileAllowsMissingFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), ".env")

	if err := loadEnvironmentFile(filename); err != nil {
		t.Fatalf("loadEnvironmentFile returned error for missing file: %v", err)
	}
}

func TestLoadEnvironmentFileReportsMalformedFile(t *testing.T) {
	filename := writeEnvFile(t, "BROKEN=\"unterminated\n")

	err := loadEnvironmentFile(filename)
	if err == nil {
		t.Fatal("loadEnvironmentFile returned nil error")
	}
	if !strings.Contains(err.Error(), "load environment variables") {
		t.Errorf("error %q does not contain context", err)
	}
}

func writeEnvFile(t *testing.T, contents string) string {
	t.Helper()

	filename := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(filename, []byte(contents), 0o600); err != nil {
		t.Fatalf("write environment file: %v", err)
	}

	return filename
}

func unsetEnvForTest(t *testing.T, name string) {
	t.Helper()

	value, exists := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("unset %s: %v", name, err)
	}

	t.Cleanup(func() {
		var err error
		if exists {
			err = os.Setenv(name, value)
		} else {
			err = os.Unsetenv(name)
		}
		if err != nil {
			t.Errorf("restore %s: %v", name, err)
		}
	})
}
