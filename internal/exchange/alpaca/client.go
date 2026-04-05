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

func (c *AlpacaClient) GetDailyCandles(symbol string, count int) ([]strategy.Candle, error) {
	end := time.Now()
	start := end.AddDate(0, 0, -count*3) // 여유있게 조회

	req := marketdata.GetBarsRequest{
		TimeFrame: marketdata.OneDay,
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
