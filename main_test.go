package main

import (
	"bytes"
	"testing"
)

func TestRunPrintsStartedMessage(t *testing.T) {
	var output bytes.Buffer

	if err := run(&output); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	const want = "trading bot started\n"
	if got := output.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
