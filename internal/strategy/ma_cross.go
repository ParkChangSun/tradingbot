package strategy

import (
	"github.com/cinar/indicator"
)

type MACrossStrategy struct {
	ShortPeriod int
	LongPeriod  int
}

func (s *MACrossStrategy) Name() string {
	return "Moving Average Crossover"
}

func (s *MACrossStrategy) Calculate(candles []Candle) Signal {
	if len(candles) < s.LongPeriod+1 {
		return SignalNone
	}

	prices := make([]float64, len(candles))
	for i, c := range candles {
		prices[i] = c.Close
	}

	shortMA := indicator.Sma(s.ShortPeriod, prices)
	longMA := indicator.Sma(s.LongPeriod, prices)

	// 현재 골든크로스(매수) 또는 데드크로스(매도) 확인
	// 최근 두 시점의 데이터를 비교
	currIdx := len(shortMA) - 1
	prevIdx := len(shortMA) - 2

	if shortMA[prevIdx] <= longMA[prevIdx] && shortMA[currIdx] > longMA[currIdx] {
		return SignalBuy
	}

	if shortMA[prevIdx] >= longMA[prevIdx] && shortMA[currIdx] < longMA[currIdx] {
		return SignalSell
	}

	return SignalNone
}
