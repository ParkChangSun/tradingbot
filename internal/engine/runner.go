package engine

import (
	"fmt"
	"time"

	"github.com/park/tradingbot/internal/config"
	"github.com/park/tradingbot/internal/exchange/alpaca"
	"github.com/park/tradingbot/internal/notifier"
	"github.com/park/tradingbot/internal/strategy"
)

type Runner struct {
	Config   *config.Config
	Client   *alpaca.AlpacaClient
	Notifier *notifier.TelegramNotifier
	Strategy strategy.Strategy
}

func NewRunner(cfg *config.Config, client *alpaca.AlpacaClient, n *notifier.TelegramNotifier, s strategy.Strategy) *Runner {
	return &Runner{
		Config:   cfg,
		Client:   client,
		Notifier: n,
		Strategy: s,
	}
}

func (r *Runner) Start() {
	r.Notifier.Notify(fmt.Sprintf("🚀 Alpaca 트레이딩 봇 시작! (전략: %s, 모드: %v)", r.Strategy.Name(), r.Config.DryRun))

	ticker := time.NewTicker(time.Duration(r.Config.IntervalSeconds) * time.Second)
	for range ticker.C {
		r.process()
	}
}

func (r *Runner) process() {
	// 장이 열려있는지 확인
	isOpen, err := r.Client.IsMarketOpen()
	if err != nil {
		fmt.Printf("시장 상태 조회 실패: %v\n", err)
		return
	}
	if !isOpen {
		fmt.Println("시장이 닫혀있습니다. 데이터 조회 및 주문을 건너뜁니다.")
		// 테스트를 위해 주석 처리하거나, DryRun 시에는 무시할 수도 있습니다.
		// 실제 투자를 위해선 return으로 스킵해야 합니다.
		// return 
	}

	for _, symbol := range r.Config.Symbols {
		// 1. 데이터 수집 (일봉 100개)
		candles, err := r.Client.GetDailyCandles(symbol, 100)
		if err != nil {
			fmt.Printf("[%s] 데이터 수집 실패: %v\n", symbol, err)
			continue
		}

		// 2. 전략 계산
		sig := r.Strategy.Calculate(candles)
		if sig == strategy.SignalNone {
			continue
		}

		// 3. 매매 결정 및 실행
		r.handleSignal(symbol, sig, isOpen)
	}
}

func (r *Runner) handleSignal(symbol string, sig strategy.Signal, isOpen bool) {
	msg := fmt.Sprintf("📢 [%s] 시그널 발생: %s", symbol, sig.String())
	fmt.Println(msg)

	if r.Config.DryRun {
		r.Notifier.Notify(msg + " (모의 실행)")
		return
	}

	if !isOpen {
		fmt.Printf("[%s] 시장이 닫혀있어 주문을 실행하지 않습니다.\n", symbol)
		return
	}

	// 실제 매매 (1주)
	isBuy := (sig == strategy.SignalBuy)
	resp, err := r.Client.Order(symbol, 1.0, isBuy)
	if err != nil {
		r.Notifier.Notify(fmt.Sprintf("❌ [%s] 주문 실패: %v", symbol, err))
	} else {
		r.Notifier.Notify(fmt.Sprintf("✅ [%s] 주문 완료: %s", symbol, resp))
	}
}
