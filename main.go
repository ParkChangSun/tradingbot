package main

import (
	"fmt"
	"io"
	"os"
)

const startedMessage = "trading bot started"

func main() {
	if err := run(os.Stdout); err != nil {
		os.Exit(1)
	}
}

func run(w io.Writer) error {
	_, err := fmt.Fprintln(w, startedMessage)
	return err
}
