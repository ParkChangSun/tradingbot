package main

import (
	"fmt"
	"io"
	"os"
)

const startedMessage = "trading bot started"

func main() {
	if err := loadEnvironmentFile(defaultEnvFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if _, err := loadConfig(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(w io.Writer) error {
	_, err := fmt.Fprintln(w, startedMessage)
	return err
}
