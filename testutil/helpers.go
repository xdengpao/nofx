package testutil

import (
	"math"
	"time"

	"github.com/leanovate/gopter"

	"nofx/decision"
	"nofx/market"
)

// ============================================================================
// 浮点数比较辅助函数
// ============================================================================

// ApproxEqual 判断两个浮点数是否在给定绝对容差内相等
func ApproxEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) <= tolerance
}

// ApproxEqualRel 判断两个浮点数是否在相对容差内相等（相对于较大值）
func ApproxEqualRel(a, b, relTolerance float64) bool {
	if a == 0 && b == 0 {
		return true
	}
	maxVal := math.Max(math.Abs(a), math.Abs(b))
	return math.Abs(a-b)/maxVal <= relTolerance
}

// WithinRange 判断值是否在 [lo, hi] 范围内（含端点）
func WithinRange(val, lo, hi float64) bool {
	return val >= lo && val <= hi
}

// ============================================================================
// mock 数据构造辅助函数
// ============================================================================

// NewTestContext 构造测试用 decision.Context
func NewTestContext(equity float64, positions []decision.PositionInfo) *decision.Context {
	return &decision.Context{
		Account: decision.AccountInfo{
			TotalEquity:      equity,
			AvailableBalance: equity * 0.5,
			MarginUsedPct:    50.0,
		},
		Positions:      positions,
		MarketDataMap:  map[string]*market.Data{},
		CorrelationMap: map[string]*decision.CorrelationData{},
	}
}

// NewTestContextWithBTC 构造包含 BTC 市场数据的测试 Context
func NewTestContextWithBTC(equity, btcPriceChange1h float64) *decision.Context {
	ctx := NewTestContext(equity, nil)
	ctx.MarketDataMap["BTCUSDT"] = &market.Data{
		PriceChange1h: btcPriceChange1h,
	}
	return ctx
}

// NewTestPosition 构造测试持仓
func NewTestPosition(symbol, side string, qty, markPrice, stopLoss float64) decision.PositionInfo {
	return decision.PositionInfo{
		Symbol:     symbol,
		Side:       side,
		EntryPrice: markPrice,
		MarkPrice:  markPrice,
		Quantity:   qty,
		StopLoss:   stopLoss,
	}
}

// NewTestTradePlan 构造测试用交易计划
func NewTestTradePlan(symbol, direction string, entry, stopLoss, takeProfit float64) *decision.TradePlan {
	return &decision.TradePlan{
		Symbol:          symbol,
		Direction:       direction,
		EntryPrice:      entry,
		StopLoss:        stopLoss,
		TakeProfit:      takeProfit,
		CurrentStopLoss: stopLoss,
		PositionSizeUSD: 1000.0,
		Leverage:        5,
		Status:          "active",
		Confidence:      75,
		MinHoldMinutes:  30,
		CreatedAt:       time.Now(),
		EntryReason:     "测试计划",
	}
}

// NewTestMarketData 构造测试用市场数据
func NewTestMarketData(symbol string, price, atr14 float64) *market.Data {
	return &market.Data{
		Symbol:         symbol,
		CurrentPrice:   price,
		PriceChange1h:  0.0,
		PriceChange4h:  0.0,
		CurrentEMA20:   price * 0.99,
		CurrentEMA50:   price * 0.98,
		CurrentMACD:    0.0,
		CurrentRSI7:    50.0,
		CurrentRSI14:   50.0,
		CurrentADX:     25.0,
		CurrentDIPlus:  20.0,
		CurrentDIMinus: 15.0,
		BollingerWidth: 5.0,
		OpenInterest:   &market.OIData{Latest: 1000, Average: 950},
		FundingRate:    0.0001,
		LongerTermContext: &market.LongerTermData{
			EMA20: price * 0.99,
			EMA50: price * 0.98,
			ATR3:  atr14 * 0.7,
			ATR14: atr14,
		},
		IntradaySeries: &market.IntradayData{
			MidPrices: []float64{price * 0.99, price * 0.995, price, price * 1.001, price * 1.002},
		},
		MidTermSeries1h: &market.MidTermData1h{
			MidPrices: []float64{price * 0.98, price * 0.99, price, price * 1.01, price * 1.02},
		},
	}
}

// NewTestKlines 构造长度为 n 的测试 K 线序列（价格固定）
func NewTestKlines(n int, price float64) []market.Kline {
	klines := make([]market.Kline, n)
	baseTime := int64(1700000000000)
	for i := 0; i < n; i++ {
		klines[i] = market.Kline{
			OpenTime:  baseTime + int64(i)*180000,
			Open:      price * 0.999,
			High:      price * 1.003,
			Low:       price * 0.997,
			Close:     price,
			Volume:    1000.0,
			CloseTime: baseTime + int64(i)*180000 + 179999,
		}
	}
	return klines
}

// NewTrendingKlines 构造单调上升的 K 线序列（用于趋势测试）
func NewTrendingKlines(n int, startPrice, stepPct float64) []market.Kline {
	klines := make([]market.Kline, n)
	baseTime := int64(1700000000000)
	price := startPrice
	for i := 0; i < n; i++ {
		klines[i] = market.Kline{
			OpenTime:  baseTime + int64(i)*180000,
			Open:      price,
			High:      price * (1 + stepPct*0.5),
			Low:       price * (1 - stepPct*0.1),
			Close:     price * (1 + stepPct),
			Volume:    1000.0 + float64(i)*10,
			CloseTime: baseTime + int64(i)*180000 + 179999,
		}
		price = klines[i].Close
	}
	return klines
}

// ============================================================================
// gopter 测试参数辅助函数
// ============================================================================

// DefaultTestParameters 返回标准属性基测试参数（100 次迭代，固定种子）
func DefaultTestParameters() *gopter.TestParameters {
	params := gopter.DefaultTestParameters()
	params.MinSuccessfulTests = 100
	params.Rng.Seed(42)
	return params
}

// QuickTestParameters 返回快速测试参数（50 次迭代，固定种子）
func QuickTestParameters() *gopter.TestParameters {
	params := gopter.DefaultTestParameters()
	params.MinSuccessfulTests = 50
	params.Rng.Seed(42)
	return params
}
