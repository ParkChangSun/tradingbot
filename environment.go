package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

const defaultEnvFile = ".env"

func loadEnvironmentFile(filename string) error {
	if err := godotenv.Load(filename); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("load environment variables from %q: %w", filename, err)
	}

	return nil
}
