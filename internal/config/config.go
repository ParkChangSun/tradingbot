package config

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	AlpacaBaseURL   string
	AlpacaAPIKey    string
	AlpacaAPISecret string
	TelegramToken   string
	TelegramChatID  int64
	EnableTelegram  bool
	DryRun          bool
	Strategy        string
	Symbols         []string
	IntervalSeconds int
}

func LoadConfig() (*Config, error) {
	// .env 파일이 있으면 로드 (없어도 환경변수에서 읽음)
	_ = godotenv.Load()

	enableTg, _ := strconv.ParseBool(os.Getenv("ENABLE_TELEGRAM"))
	chatID, _ := strconv.ParseInt(os.Getenv("TELEGRAM_CHAT_ID"), 10, 64)
	dryRun, _ := strconv.ParseBool(os.Getenv("DRY_RUN"))
	interval, _ := strconv.Atoi(os.Getenv("CHECK_INTERVAL_SECONDS"))
	if interval == 0 {
		interval = 60
	}

	rawSymbols := strings.Split(os.Getenv("SYMBOL_LIST"), ",")
	var symbols []string
	for _, s := range rawSymbols {
		if t := strings.TrimSpace(s); t != "" {
			symbols = append(symbols, t)
		}
	}
	if len(symbols) == 0 {
		symbols = []string{"AAPL", "MSFT", "NVDA"}
	}

	baseURL := os.Getenv("ALPACA_BASE_URL")
	if baseURL == "" {
		baseURL = "https://paper-api.alpaca.markets"
	}

	return &Config{
		AlpacaBaseURL:   baseURL,
		AlpacaAPIKey:    os.Getenv("ALPACA_API_KEY"),
		AlpacaAPISecret: os.Getenv("ALPACA_API_SECRET"),
		TelegramToken:   os.Getenv("TELEGRAM_TOKEN"),
		TelegramChatID:  chatID,
		EnableTelegram:  enableTg,
		DryRun:          dryRun,
		Strategy:        os.Getenv("STRATEGY"),
		Symbols:         symbols,
		IntervalSeconds: interval,
	}, nil
}
