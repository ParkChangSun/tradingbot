package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/park/tradingbot/internal/config"
	"github.com/park/tradingbot/internal/engine"
	"github.com/park/tradingbot/internal/exchange/alpaca"
	"github.com/park/tradingbot/internal/notifier"
	"github.com/park/tradingbot/internal/strategy"
)

func main() {
	// 1. 설정 로드
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("설정 로드 실패: %v", err)
	}

	// 2. 종목 확인
	if len(cfg.Symbols) == 0 {
		cfg.Symbols = []string{"AAPL", "MSFT", "NVDA"}
	}

	// 3. 거래소 클라이언트 초기화 (Alpaca)
	client := alpaca.NewAlpacaClient(cfg.AlpacaAPIKey, cfg.AlpacaAPISecret, cfg.AlpacaBaseURL)

	// 4. 알림 모듈 초기화
	var ntf *notifier.TelegramNotifier
	if cfg.EnableTelegram {
		var ntfErr error
		ntf, ntfErr = notifier.NewTelegramNotifier(cfg.TelegramToken, cfg.TelegramChatID)
		if ntfErr != nil {
			fmt.Printf("알림 모듈 초기화 실패 (로그로 대체): %v\n", ntfErr)
		}
	} else {
		ntf, _ = notifier.NewTelegramNotifier("", 0) // 설정으로 껐을 경우 로그로 대체
	}

	// 5. 전략 선택
	var selectedStrategy strategy.Strategy
	switch cfg.Strategy {
	case "RSI":
		selectedStrategy = &strategy.RSIStrategy{Period: 14, Overbought: 70, Oversold: 30}
	default:
		selectedStrategy = &strategy.MACrossStrategy{ShortPeriod: 5, LongPeriod: 20}
	}

	// 6. 엔진 실행
	runner := engine.NewRunner(cfg, client, ntf, selectedStrategy)
	
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		fmt.Println("\n봇을 종료합니다...")
		os.Exit(0)
	}()

	fmt.Printf("봇 구동 중... (전략: %s, 감시 종목: %v)\n", selectedStrategy.Name(), cfg.Symbols)
	runner.Start()
}
