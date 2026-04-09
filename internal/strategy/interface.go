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

// News는 종목 관련 뉴스 기사를 나타냅니다.
type News struct {
	Time     string
	Headline string
	Summary  string
}

// Snapshot은 종목의 현재 시장 상태를 담습니다.
// Alpaca GetSnapshot API에서 수집합니다.
type Snapshot struct {
	LastPrice   float64 // 최근 체결가
	BidPrice    float64 // 매수 호가 (사려는 가격)
	AskPrice    float64 // 매도 호가 (팔려는 가격)
	Spread      float64 // 호가 스프레드
	VWAP        float64 // 오늘 거래량 가중 평균가
	PrevClose   float64 // 전일 종가
	ChangeRatio float64 // 전일 대비 변화율 (%)
}

// MarketContext는 전체 시장 분위기를 나타냅니다.
// SPY(S&P500), VXX(변동성) 등을 기반으로 합니다.
type MarketContext struct {
	SPYChangeRatio float64 // SPY 전일 대비 변화율 (%)
	VXXChangeRatio float64 // VXX 전일 대비 변화율 (%)
}

// Indicators는 캔들 데이터로부터 미리 계산된 기술적 지표입니다.
type Indicators struct {
	RSI           float64 // RSI(14)
	MACD          float64 // MACD 라인
	MACDSignal    float64 // MACD 시그널 라인
	MACDHist      float64 // MACD 히스토그램 (+면 강세, -면 약세)
	BollingerUpper float64 // 볼린저 밴드 상단
	BollingerMid  float64 // 볼린저 밴드 중간(SMA20)
	BollingerLower float64 // 볼린저 밴드 하단
}

// SignalResult는 구체적인 매매 신호를 정의합니다.
type SignalResult int

const (
	SignalNone SignalResult = iota
	SignalBuy
	SignalSell
)

func (s SignalResult) String() string {
	switch s {
	case SignalBuy:
		return "BUY"
	case SignalSell:
		return "SELL"
	default:
		return "HOLD"
	}
}

// Signal은 매매 결정과 그에 대한 근거를 포함합니다.
type Signal struct {
	Result   SignalResult
	Reason   string
	Strength int // 0~100 사이의 판단 확신도
}

// Strategy는 모든 매매 전략이 구현해야 하는 인터페이스입니다.
type Strategy interface {
	Name() string
	Calculate(symbol string, candles []Candle, news []News, snap Snapshot, market MarketContext) Signal
}

