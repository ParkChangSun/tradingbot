package main

import (
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/shopspring/decimal"
)

const (
	apiKeyEnv        = "APCA_API_KEY_ID"
	apiSecretEnv     = "APCA_API_SECRET_KEY"
	tradingSymbolEnv = "TRADING_SYMBOL"
)

type Config struct {
	APIKey                  string
	APISecret               string
	TradingSymbol           string
	DecisionPolicy          string
	OpenAIKey               string
	OpenAIModel             string
	CycleBudgetNanodollars  int64
	RecommendationDirectory string
	PlannerLimits           PlannerLimits
}

func loadConfig() (Config, error) {
	config := Config{
		APIKey:                  strings.TrimSpace(os.Getenv(apiKeyEnv)),
		APISecret:               strings.TrimSpace(os.Getenv(apiSecretEnv)),
		TradingSymbol:           strings.ToUpper(strings.TrimSpace(os.Getenv(tradingSymbolEnv))),
		DecisionPolicy:          strings.TrimSpace(os.Getenv("DECISION_POLICY")),
		OpenAIKey:               strings.TrimSpace(os.Getenv("OPENAI_API_KEY")),
		OpenAIModel:             strings.TrimSpace(os.Getenv("OPENAI_MODEL")),
		CycleBudgetNanodollars:  defaultCycleBudgetNanodollars,
		RecommendationDirectory: ".tradingbot-recommendations",
		PlannerLimits:           defaultPlannerLimits(),
	}
	if config.DecisionPolicy == "" {
		config.DecisionPolicy = "hold"
	}
	if config.OpenAIModel == "" {
		config.OpenAIModel = approvedModel
	}

	var validationErrors []error
	for _, setting := range []struct {
		name   string
		target *decimal.Decimal
	}{
		{"PLANNER_MAX_POSITION_WEIGHT", &config.PlannerLimits.MaxPositionWeight},
		{"PLANNER_MAX_INVESTED_WEIGHT", &config.PlannerLimits.MaxInvestedWeight},
		{"PLANNER_MAX_ORDER_WEIGHT", &config.PlannerLimits.MaxOrderWeight},
	} {
		if value := strings.TrimSpace(os.Getenv(setting.name)); value != "" {
			v, err := decimal.NewFromString(value)
			if err != nil {
				validationErrors = append(validationErrors, fmt.Errorf("%s must be a decimal weight", setting.name))
			} else {
				*setting.target = v
			}
		}
	}
	if value := strings.TrimSpace(os.Getenv("PLANNER_QUOTE_MAX_AGE_SECONDS")); value != "" {
		v, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			validationErrors = append(validationErrors, fmt.Errorf("PLANNER_QUOTE_MAX_AGE_SECONDS must be integer seconds"))
		} else {
			config.PlannerLimits.QuoteMaxAgeSeconds = v
		}
	}
	if err := config.PlannerLimits.validate(); err != nil {
		validationErrors = append(validationErrors, err)
	}
	if config.DecisionPolicy != "hold" && config.DecisionPolicy != "openai" {
		validationErrors = append(validationErrors, fmt.Errorf("DECISION_POLICY must be hold or openai"))
	}
	if config.DecisionPolicy == "openai" {
		if config.OpenAIKey == "" {
			validationErrors = append(validationErrors, fmt.Errorf("OPENAI_API_KEY is required"))
		}
		if config.OpenAIModel != approvedModel {
			validationErrors = append(validationErrors, fmt.Errorf("OPENAI_MODEL requires reviewed model pricing"))
		}
		if value := strings.TrimSpace(os.Getenv("OPENAI_CYCLE_BUDGET_USD")); value != "" {
			budget, err := parseCycleBudget(value)
			if err != nil {
				validationErrors = append(validationErrors, err)
			} else {
				config.CycleBudgetNanodollars = budget
			}
		}
	}
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

func parseCycleBudget(value string) (int64, error) {
	d, err := decimal.NewFromString(value)
	if err != nil {
		return 0, fmt.Errorf("OPENAI_CYCLE_BUDGET_USD must be a positive exact dollar amount")
	}
	n := d.Mul(decimal.NewFromInt(1000000000))
	if !n.IsPositive() || !n.Equal(n.Truncate(0)) || n.GreaterThan(decimal.NewFromInt(math.MaxInt64)) {
		return 0, fmt.Errorf("OPENAI_CYCLE_BUDGET_USD is outside supported nanodollar precision/range")
	}
	return n.IntPart(), nil
}
