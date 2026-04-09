package alpaca

import (
	"time"

	"github.com/alpacahq/alpaca-trade-api-go/v3/alpaca"
	"github.com/alpacahq/alpaca-trade-api-go/v3/marketdata"
	"github.com/shopspring/decimal"
	
	"github.com/park/tradingbot/internal/strategy"
)

type AlpacaClient struct {
	TradeClient *alpaca.Client
	DataClient  *marketdata.Client
}

func NewAlpacaClient(apiKey, apiSecret, baseURL string) *AlpacaClient {
	tradeClient := alpaca.NewClient(alpaca.ClientOpts{
		APIKey:    apiKey,
		APISecret: apiSecret,
		BaseURL:   baseURL,
	})

	dataClient := marketdata.NewClient(marketdata.ClientOpts{
		APIKey:    apiKey,
		APISecret: apiSecret,
	})

	return &AlpacaClient{
		TradeClient: tradeClient,
		DataClient:  dataClient,
	}
}

func (c *AlpacaClient) IsMarketOpen() (bool, error) {
	clock, err := c.TradeClient.GetClock()
	if err != nil {
		return false, err
	}
	return clock.IsOpen, nil
}

func (c *AlpacaClient) GetCandles(symbol string, count int, timeframe string) ([]strategy.Candle, error) {
	var tf marketdata.TimeFrame
	switch timeframe {
	case "1Min":
		tf = marketdata.OneMin
	case "5Min":
		tf = marketdata.TimeFrame{N: 5, Unit: marketdata.Min}
	case "15Min":
		tf = marketdata.TimeFrame{N: 15, Unit: marketdata.Min}
	case "1Hour":
		tf = marketdata.OneHour
	case "1Day":
		tf = marketdata.OneDay
	default:
		tf = marketdata.OneMin
	}

	end := time.Now()
	var start time.Time
	if timeframe == "1Day" {
		start = end.AddDate(0, 0, -count*3)
	} else {
		start = end.Add(-time.Duration(count*10) * time.Hour) // 장 휴장시간 고려하여 넉넉히 과거부터 가져옴
	}

	req := marketdata.GetBarsRequest{
		TimeFrame: tf,
		Start:     start,
		End:       end,
		Feed:      marketdata.IEX, // 무료 플랜을 위한 설정
	}

	bars, err := c.DataClient.GetBars(symbol, req)
	if err != nil {
		return nil, err
	}

	var candles []strategy.Candle
	for _, b := range bars {
		candles = append(candles, strategy.Candle{
			Time:   b.Timestamp.String(),
			Open:   b.Open,
			High:   b.High,
			Low:    b.Low,
			Close:  b.Close,
			Volume: float64(b.Volume),
		})
	}

	if len(candles) > count {
		candles = candles[len(candles)-count:]
	}

	return candles, nil
}

func (c *AlpacaClient) Order(symbol string, qty float64, isBuy bool) (string, error) {
	side := alpaca.Sell
	if isBuy {
		side = alpaca.Buy
	}

	dQty := decimal.NewFromFloat(qty)

	req := alpaca.PlaceOrderRequest{
		Symbol:      symbol,
		Qty:         &dQty,
		Side:        side,
		Type:        alpaca.Market,
		TimeInForce: alpaca.Day,
	}

	order, err := c.TradeClient.PlaceOrder(req)
	if err != nil {
		return "", err
	}

	return order.ID, nil
}

func (c *AlpacaClient) GetNews(symbol string, limit int) ([]strategy.News, error) {
	end := time.Now()
	start := end.AddDate(0, 0, -1) // 최근 24시간 뉴스

	req := marketdata.GetNewsRequest{
		Symbols:    []string{symbol},
		Start:      start,
		End:        end,
		TotalLimit: limit,
	}

	news, err := c.DataClient.GetNews(req)
	if err != nil {
		return nil, err
	}

	var results []strategy.News
	for _, n := range news {
		results = append(results, strategy.News{
			Time:     n.CreatedAt.String(),
			Headline: n.Headline,
			Summary:  n.Summary,
		})
	}
	return results, nil
}

// GetSnapshot은 종목의 현재 호가, 체결가, VWAP, 전일 종가 등을 가져옵니다.
func (c *AlpacaClient) GetSnapshot(symbol string) (strategy.Snapshot, error) {
	snap, err := c.DataClient.GetSnapshot(symbol, marketdata.GetSnapshotRequest{
		Feed: marketdata.IEX,
	})
	if err != nil {
		return strategy.Snapshot{}, err
	}

	last := snap.LatestTrade.Price
	bid := snap.LatestQuote.BidPrice
	ask := snap.LatestQuote.AskPrice
	vwap := snap.DailyBar.VWAP
	prevClose := snap.PrevDailyBar.Close

	var changeRatio float64
	if prevClose != 0 {
		changeRatio = (last - prevClose) / prevClose * 100
	}

	return strategy.Snapshot{
		LastPrice:   last,
		BidPrice:    bid,
		AskPrice:    ask,
		Spread:      ask - bid,
		VWAP:        vwap,
		PrevClose:   prevClose,
		ChangeRatio: changeRatio,
	}, nil
}

// GetMarketContext는 SPY와 VXX의 전일 대비 변화율을 반환합니다.
func (c *AlpacaClient) GetMarketContext() (strategy.MarketContext, error) {
	fetchChange := func(sym string) float64 {
		snap, err := c.DataClient.GetSnapshot(sym, marketdata.GetSnapshotRequest{
			Feed: marketdata.IEX,
		})
		if err != nil {
			return 0
		}
		prev := snap.PrevDailyBar.Close
		last := snap.LatestTrade.Price
		if prev == 0 {
			return 0
		}
		return (last - prev) / prev * 100
	}

	return strategy.MarketContext{
		SPYChangeRatio: fetchChange("SPY"),
		VXXChangeRatio: fetchChange("VXX"),
	}, nil
}
