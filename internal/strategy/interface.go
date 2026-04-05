package strategy

// Candle은 하나의 OHLCV 봉 데이터를 나타냅니다.
// 거래소에 관계없이 모든 전략이 공통으로 사용합니다.
type Candle struct {
	Time   string
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// Signal은 매매 결정을 나타냅니다.
type Signal int

const (
	SignalNone Signal = iota
	SignalBuy
	SignalSell
)

func (s Signal) String() string {
	switch s {
	case SignalBuy:
		return "BUY"
	case SignalSell:
		return "SELL"
	default:
		return "HOLD"
	}
}

// Strategy는 모든 매매 전략이 구현해야 하는 인터페이스입니다.
type Strategy interface {
	Name() string
	Calculate(candles []Candle) Signal
}
