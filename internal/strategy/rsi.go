package strategy

import (
	"github.com/cinar/indicator"
)

type RSIStrategy struct {
	Period    int
	Overbought float64
	Oversold   float64
}

func (s *RSIStrategy) Name() string {
	return "RSI Strategy"
}

func (s *RSIStrategy) Calculate(candles []Candle) Signal {
	if len(candles) < s.Period+1 {
		return SignalNone
	}

	prices := make([]float64, len(candles))
	for i, c := range candles {
		prices[i] = c.Close
	}

	_, rsi := indicator.RsiPeriod(s.Period, prices)
	currRSI := rsi[len(rsi)-1]
	prevRSI := rsi[len(rsi)-2]

	// 과매도 이탈(매수 신호: 30 미만에서 위로 탈출)
	if prevRSI < s.Oversold && currRSI >= s.Oversold {
		return SignalBuy
	}

	// 과매수 이탈(매도 신호: 70 초과에서 아래로 탈출)
	if prevRSI > s.Overbought && currRSI <= s.Overbought {
		return SignalSell
	}

	return SignalNone
}
