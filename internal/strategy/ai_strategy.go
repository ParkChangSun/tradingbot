package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"

	"google.golang.org/genai"
)

type AIStrategy struct {
	Client    *genai.Client
	Model     string
	Timeframe string
}

type aiResponse struct {
	Signal   string `json:"signal"`
	Reason   string `json:"reason"`
	Strength int    `json:"strength"`
}

func (s *AIStrategy) Name() string {
	return fmt.Sprintf("AI Strategy (%s)", s.Model)
}

func (s *AIStrategy) Calculate(symbol string, candles []Candle, news []News, snap Snapshot, market MarketContext) Signal {
	if len(candles) == 0 {
		return Signal{Result: SignalNone, Reason: "데이터 부족"}
	}

	// 1. 기술적 지표 계산 (캔들 데이터에서 직접 계산)
	indicators := computeIndicators(candles)

	// 2. 프롬프트 구성
	prompt := s.buildPrompt(symbol, candles, news, snap, market, indicators)

	// 3. Gemini API 호출
	ctx := context.Background()
	resp, err := s.Client.Models.GenerateContent(ctx, s.Model, genai.Text(prompt), nil)
	if err != nil {
		log.Printf("AI 분석 실패: %v", err)
		return Signal{Result: SignalNone, Reason: "AI 분석 에러"}
	}

	if len(resp.Candidates) == 0 || len(resp.Candidates[0].Content.Parts) == 0 {
		return Signal{Result: SignalNone, Reason: "AI 응답 없음"}
	}

	// 4. 응답 파싱 (JSON 추출)
	rawText := resp.Candidates[0].Content.Parts[0].Text
	return s.parseResponse(rawText)
}

// computeIndicators는 캔들 데이터로부터 RSI, MACD, 볼린저 밴드를 계산합니다.
func computeIndicators(candles []Candle) Indicators {
	closes := make([]float64, len(candles))
	for i, c := range candles {
		closes[i] = c.Close
	}

	ind := Indicators{}
	if len(closes) >= 14 {
		ind.RSI = computeRSI(closes, 14)
	}
	if len(closes) >= 26 {
		macd, signal := computeMACD(closes, 12, 26, 9)
		ind.MACD = macd
		ind.MACDSignal = signal
		ind.MACDHist = macd - signal
	}
	if len(closes) >= 20 {
		upper, mid, lower := computeBollingerBands(closes, 20, 2.0)
		ind.BollingerUpper = upper
		ind.BollingerMid = mid
		ind.BollingerLower = lower
	}
	return ind
}

func computeEMA(closes []float64, period int) []float64 {
	ema := make([]float64, len(closes))
	k := 2.0 / float64(period+1)
	ema[0] = closes[0]
	for i := 1; i < len(closes); i++ {
		ema[i] = closes[i]*k + ema[i-1]*(1-k)
	}
	return ema
}

func computeRSI(closes []float64, period int) float64 {
	if len(closes) < period+1 {
		return 50
	}
	var gains, losses float64
	for i := 1; i <= period; i++ {
		diff := closes[len(closes)-period-1+i] - closes[len(closes)-period-2+i]
		if diff > 0 {
			gains += diff
		} else {
			losses -= diff
		}
	}
	avgGain := gains / float64(period)
	avgLoss := losses / float64(period)
	if avgLoss == 0 {
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

func computeMACD(closes []float64, fast, slow, signal int) (float64, float64) {
	emaFast := computeEMA(closes, fast)
	emaSlow := computeEMA(closes, slow)

	macdLine := make([]float64, len(closes))
	for i := range closes {
		macdLine[i] = emaFast[i] - emaSlow[i]
	}
	signalLine := computeEMA(macdLine, signal)

	last := len(closes) - 1
	return macdLine[last], signalLine[last]
}

func computeBollingerBands(closes []float64, period int, multiplier float64) (upper, mid, lower float64) {
	n := len(closes)
	window := closes[n-period:]

	var sum float64
	for _, v := range window {
		sum += v
	}
	mid = sum / float64(period)

	var sqSum float64
	for _, v := range window {
		diff := v - mid
		sqSum += diff * diff
	}
	stdDev := math.Sqrt(sqSum / float64(period))

	upper = mid + multiplier*stdDev
	lower = mid - multiplier*stdDev
	return
}

func (s *AIStrategy) buildPrompt(symbol string, candles []Candle, news []News, snap Snapshot, market MarketContext, ind Indicators) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("너는 초단기/단기 차트를 분석하는 전문적인 퀀트 트레이더야. 다음 [%s] 종목의 %s 주기 차트와 종합 시장 데이터를 보고 매매 결정을 내려줘.\n\n", symbol, s.Timeframe))

	// 1. 현재 시세 상태 (Snapshot)
	sb.WriteString("### 현재 시세 상태:\n")
	sb.WriteString(fmt.Sprintf("- 최근 체결가: $%.2f\n", snap.LastPrice))
	sb.WriteString(fmt.Sprintf("- 매수호가: $%.2f / 매도호가: $%.2f (스프레드: $%.4f)\n", snap.BidPrice, snap.AskPrice, snap.Spread))
	sb.WriteString(fmt.Sprintf("- VWAP: $%.2f (현재가 대비 %+.2f%%)\n", snap.VWAP, (snap.LastPrice-snap.VWAP)/snap.VWAP*100))
	sb.WriteString(fmt.Sprintf("- 전일 종가: $%.2f → 현재 전일비 %+.2f%%\n", snap.PrevClose, snap.ChangeRatio))

	// 2. 기술적 지표
	sb.WriteString("\n### 기술적 지표 (서버 계산):\n")
	sb.WriteString(fmt.Sprintf("- RSI(14): %.1f", ind.RSI))
	if ind.RSI >= 70 {
		sb.WriteString(" ⚠️ 과매수 구간\n")
	} else if ind.RSI <= 30 {
		sb.WriteString(" ⚠️ 과매도 구간\n")
	} else {
		sb.WriteString("\n")
	}
	sb.WriteString(fmt.Sprintf("- MACD: %.4f / Signal: %.4f / Hist: %+.4f", ind.MACD, ind.MACDSignal, ind.MACDHist))
	if ind.MACDHist > 0 {
		sb.WriteString(" (상승 추세)\n")
	} else {
		sb.WriteString(" (하락 추세)\n")
	}
	sb.WriteString(fmt.Sprintf("- 볼린저 밴드: 상단 $%.2f / 중간 $%.2f / 하단 $%.2f\n", ind.BollingerUpper, ind.BollingerMid, ind.BollingerLower))

	// 3. 전체 시장 환경
	sb.WriteString("\n### 전체 시장 환경:\n")
	sb.WriteString(fmt.Sprintf("- SPY(S&P500): 전일비 %+.2f%%\n", market.SPYChangeRatio))
	sb.WriteString(fmt.Sprintf("- VXX(변동성): 전일비 %+.2f%%\n", market.VXXChangeRatio))

	// 4. 최근 캔들 요약 (최근 20개만)
	sb.WriteString(fmt.Sprintf("\n### 최근 캔들 (%s, 최신 20개):\n", s.Timeframe))
	startIdx := len(candles) - 20
	if startIdx < 0 {
		startIdx = 0
	}
	for _, c := range candles[startIdx:] {
		timeStr := c.Time
		if len(timeStr) >= 16 {
			timeStr = timeStr[:16]
		}
		sb.WriteString(fmt.Sprintf("- %s: 시가 %.2f, 고가 %.2f, 저가 %.2f, 종가 %.2f, 거래량 %.0f\n",
			timeStr, c.Open, c.High, c.Low, c.Close, c.Volume))
	}

	// 5. 뉴스
	sb.WriteString("\n### 최근 뉴스:\n")
	if len(news) == 0 {
		sb.WriteString("- 기록된 뉴스가 없습니다.\n")
	} else {
		for _, n := range news {
			timeStr := n.Time
			if len(timeStr) >= 10 {
				timeStr = timeStr[:10]
			}
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", timeStr, n.Headline))
		}
	}

	sb.WriteString("\n### 지시사항:\n")
	sb.WriteString("1. 위의 모든 데이터를 종합하여 즉각적인 매매 결정을 내려줘.\n")
	sb.WriteString("2. 출력은 반드시 다음 JSON 형식으로만 해줘. 다른 설명은 절대 하지 마.\n")
	sb.WriteString("3. 형식: { \"signal\": \"BUY\" 또는 \"SELL\" 또는 \"HOLD\", \"reason\": \"한국어로된 짧은 요약\", \"strength\": 0에서 100까지의 숫자 }\n")
	sb.WriteString("4. strength값은 해당 판단에 대한 확신도. 무조건 사야하면 90이상, 애매하면 30이하.\n")

	return sb.String()
}

func (s *AIStrategy) parseResponse(text string) Signal {
	cleanText := strings.TrimSpace(text)
	cleanText = strings.TrimPrefix(cleanText, "```json")
	cleanText = strings.TrimPrefix(cleanText, "```")
	cleanText = strings.TrimSuffix(cleanText, "```")
	cleanText = strings.TrimSpace(cleanText)

	var res aiResponse
	if err := json.Unmarshal([]byte(cleanText), &res); err != nil {
		log.Printf("AI 응답 파싱 실패: %v (Raw: %s)", err, text)
		return Signal{Result: SignalNone, Reason: "AI 응답 형식 오류"}
	}

	var result SignalResult
	switch strings.ToUpper(res.Signal) {
	case "BUY":
		result = SignalBuy
	case "SELL":
		result = SignalSell
	default:
		result = SignalNone
	}

	return Signal{Result: result, Reason: res.Reason, Strength: res.Strength}
}
