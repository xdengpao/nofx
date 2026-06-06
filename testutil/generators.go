// Package testutil 提供通用的 gopter 生成器和测试辅助函数，供各模块属性基测试复用。
package testutil

import (
	"reflect"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"

	"nofx/config"
	"nofx/decision"
	"nofx/market"
)

// ============================================================================
// config 包生成器
// ============================================================================

// GenTraderConfig 生成随机有效的 TraderConfig（Binance + DeepSeek 组合）
func GenTraderConfig() gopter.Gen {
	return gen.Struct(reflect.TypeOf(config.TraderConfig{}), map[string]gopter.Gen{
		"ID":                  gen.OneConstOf("trader-1", "trader-2", "trader-3"),
		"Name":                gen.OneConstOf("Alpha", "Beta", "Gamma"),
		"Enabled":             gen.Bool(),
		"AIModel":             gen.Const("deepseek"),
		"Exchange":            gen.Const("binance"),
		"BinanceAPIKey":       gen.Const("placeholder-api-key"),
		"BinanceSecretKey":    gen.Const("placeholder-secret-key"),
		"DeepSeekKey":         gen.Const("placeholder-deepseek-key"),
		"InitialBalance":      gen.Float64Range(100, 100000),
		"ScanIntervalMinutes": gen.IntRange(1, 60),
	})
}

// GenLeverageConfig 生成随机杠杆配置
func GenLeverageConfig() gopter.Gen {
	return gen.Struct(reflect.TypeOf(config.LeverageConfig{}), map[string]gopter.Gen{
		"BTCETHLeverage":  gen.IntRange(1, 20),
		"AltcoinLeverage": gen.IntRange(1, 20),
	})
}

// GenConfig 生成包含单个有效 TraderConfig 的完整 Config
func GenConfig() gopter.Gen {
	return GenTraderConfig().Map(func(tc config.TraderConfig) config.Config {
		tc.ID = "trader-1" // 确保唯一 ID
		return config.Config{
			Traders:         []config.TraderConfig{tc},
			UseDefaultCoins: true,
			APIServerPort:   8080,
			MaxDailyLoss:    10.0,
			MaxDrawdown:     20.0,
			Leverage: config.LeverageConfig{
				BTCETHLeverage:  5,
				AltcoinLeverage: 5,
			},
		}
	})
}

// ============================================================================
// decision 包生成器
// ============================================================================

// GenSymbol 生成随机交易对符号
func GenSymbol() gopter.Gen {
	return gen.OneConstOf("BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "ADAUSDT", "DOTUSDT", "AVAXUSDT")
}

// GenDirection 生成随机方向（long/short）
func GenDirection() gopter.Gen {
	return gen.OneConstOf("long", "short")
}

// GenAction 生成随机决策动作
func GenAction() gopter.Gen {
	return gen.OneConstOf("open_long", "open_short", "close", "hold", "wait", "update_sl", "partial_close")
}

// GenDecision 生成随机 Decision
func GenDecision() gopter.Gen {
	return gen.Struct(reflect.TypeOf(decision.Decision{}), map[string]gopter.Gen{
		"Symbol":          GenSymbol(),
		"Action":          GenAction(),
		"Leverage":        gen.IntRange(1, 20),
		"PositionSizeUSD": gen.Float64Range(10, 10000),
		"StopLoss":        gen.Float64Range(10, 99000),
		"TakeProfit":      gen.Float64Range(11, 200000),
		"Confidence":      gen.IntRange(0, 100),
		"RiskUSD":         gen.Float64Range(0, 1000),
		"Reasoning":       gen.Const("测试推理"),
		"MinHoldMinutes":  gen.IntRange(0, 240),
	})
}

// GenOpenLongDecision 生成随机开多决策（止损 < 入场 < 止盈）
func GenOpenLongDecision() gopter.Gen {
	return gen.Float64Range(100, 100000).FlatMap(func(price interface{}) gopter.Gen {
		entryPrice := price.(float64)
		return gen.Struct(reflect.TypeOf(decision.Decision{}), map[string]gopter.Gen{
			"Symbol":          GenSymbol(),
			"Action":          gen.Const("open_long"),
			"Leverage":        gen.IntRange(1, 10),
			"PositionSizeUSD": gen.Float64Range(100, 5000),
			"StopLoss":        gen.Const(entryPrice * 0.95),
			"TakeProfit":      gen.Const(entryPrice * 1.15),
			"Confidence":      gen.IntRange(60, 100),
			"RiskUSD":         gen.Float64Range(10, 200),
			"Reasoning":       gen.Const("测试开多"),
			"MinHoldMinutes":  gen.IntRange(30, 120),
		})
	}, reflect.TypeOf(decision.Decision{}))
}

// GenOpenShortDecision 生成随机开空决策（止盈 < 入场 < 止损）
func GenOpenShortDecision() gopter.Gen {
	return gen.Float64Range(100, 100000).FlatMap(func(price interface{}) gopter.Gen {
		entryPrice := price.(float64)
		return gen.Struct(reflect.TypeOf(decision.Decision{}), map[string]gopter.Gen{
			"Symbol":          GenSymbol(),
			"Action":          gen.Const("open_short"),
			"Leverage":        gen.IntRange(1, 10),
			"PositionSizeUSD": gen.Float64Range(100, 5000),
			"StopLoss":        gen.Const(entryPrice * 1.05),
			"TakeProfit":      gen.Const(entryPrice * 0.85),
			"Confidence":      gen.IntRange(60, 100),
			"RiskUSD":         gen.Float64Range(10, 200),
			"Reasoning":       gen.Const("测试开空"),
			"MinHoldMinutes":  gen.IntRange(30, 120),
		})
	}, reflect.TypeOf(decision.Decision{}))
}

// GenPositionInfo 生成随机持仓信息
func GenPositionInfo() gopter.Gen {
	return gen.Struct(reflect.TypeOf(decision.PositionInfo{}), map[string]gopter.Gen{
		"Symbol":           GenSymbol(),
		"Side":             GenDirection(),
		"EntryPrice":       gen.Float64Range(100, 100000),
		"MarkPrice":        gen.Float64Range(100, 100000),
		"Quantity":         gen.Float64Range(0.001, 100),
		"Leverage":         gen.IntRange(1, 20),
		"UnrealizedPnL":    gen.Float64Range(-5000, 5000),
		"UnrealizedPnLPct": gen.Float64Range(-50, 100),
		"MarginUsed":       gen.Float64Range(10, 10000),
	})
}

// GenLongPosition 生成随机多头持仓（止损 < 入场价，止盈 > 入场价）
func GenLongPosition() gopter.Gen {
	return gen.Float64Range(100, 100000).FlatMap(func(price interface{}) gopter.Gen {
		markPrice := price.(float64)
		return gen.Struct(reflect.TypeOf(decision.PositionInfo{}), map[string]gopter.Gen{
			"Symbol":           GenSymbol(),
			"Side":             gen.Const("long"),
			"EntryPrice":       gen.Const(markPrice),
			"MarkPrice":        gen.Const(markPrice),
			"Quantity":         gen.Float64Range(0.001, 10),
			"Leverage":         gen.IntRange(1, 10),
			"UnrealizedPnL":    gen.Float64Range(-1000, 1000),
			"UnrealizedPnLPct": gen.Float64Range(-20, 50),
			"StopLoss":         gen.Const(markPrice * 0.95),
			"TakeProfit":       gen.Const(markPrice * 1.15),
			"MarginUsed":       gen.Float64Range(100, 5000),
		})
	}, reflect.TypeOf(decision.PositionInfo{}))
}

// GenShortPosition 生成随机空头持仓（止损 > 入场价，止盈 < 入场价）
func GenShortPosition() gopter.Gen {
	return gen.Float64Range(100, 100000).FlatMap(func(price interface{}) gopter.Gen {
		markPrice := price.(float64)
		return gen.Struct(reflect.TypeOf(decision.PositionInfo{}), map[string]gopter.Gen{
			"Symbol":           GenSymbol(),
			"Side":             gen.Const("short"),
			"EntryPrice":       gen.Const(markPrice),
			"MarkPrice":        gen.Const(markPrice),
			"Quantity":         gen.Float64Range(0.001, 10),
			"Leverage":         gen.IntRange(1, 10),
			"UnrealizedPnL":    gen.Float64Range(-1000, 1000),
			"UnrealizedPnLPct": gen.Float64Range(-20, 50),
			"StopLoss":         gen.Const(markPrice * 1.05),
			"TakeProfit":       gen.Const(markPrice * 0.85),
			"MarginUsed":       gen.Float64Range(100, 5000),
		})
	}, reflect.TypeOf(decision.PositionInfo{}))
}

// GenTradePlan 生成随机交易计划
func GenTradePlan() gopter.Gen {
	return gen.Struct(reflect.TypeOf(decision.TradePlan{}), map[string]gopter.Gen{
		"Symbol":             GenSymbol(),
		"Direction":          GenDirection(),
		"EntryPrice":         gen.Float64Range(100, 100000),
		"StopLoss":           gen.Float64Range(50, 99000),
		"TakeProfit":         gen.Float64Range(101, 200000),
		"PositionSizeUSD":    gen.Float64Range(10, 10000),
		"Leverage":           gen.IntRange(1, 20),
		"Status":             gen.OneConstOf("active", "closed"),
		"Confidence":         gen.IntRange(0, 100),
		"PeakPnLPercent":     gen.Float64Range(-50, 200),
		"TotalClosedPercent": gen.Float64Range(0, 100),
		"MinHoldMinutes":     gen.IntRange(0, 240),
		"EntryReason":        gen.Const("测试入场原因"),
	})
}

// GenLongTradePlan 生成多头交易计划（止损 < 入场 < 止盈）
func GenLongTradePlan() gopter.Gen {
	return gen.Float64Range(100, 100000).FlatMap(func(price interface{}) gopter.Gen {
		entry := price.(float64)
		return gen.Struct(reflect.TypeOf(decision.TradePlan{}), map[string]gopter.Gen{
			"Symbol":          GenSymbol(),
			"Direction":       gen.Const("long"),
			"EntryPrice":      gen.Const(entry),
			"StopLoss":        gen.Const(entry * 0.95),
			"TakeProfit":      gen.Const(entry * 1.15),
			"CurrentStopLoss": gen.Const(entry * 0.95),
			"PositionSizeUSD": gen.Float64Range(100, 5000),
			"Leverage":        gen.IntRange(1, 10),
			"Status":          gen.Const("active"),
			"Confidence":      gen.IntRange(60, 100),
			"MinHoldMinutes":  gen.IntRange(30, 120),
			"EntryReason":     gen.Const("测试多头计划"),
		})
	}, reflect.TypeOf(decision.TradePlan{}))
}

// GenShortTradePlan 生成空头交易计划（止盈 < 入场 < 止损）
func GenShortTradePlan() gopter.Gen {
	return gen.Float64Range(100, 100000).FlatMap(func(price interface{}) gopter.Gen {
		entry := price.(float64)
		return gen.Struct(reflect.TypeOf(decision.TradePlan{}), map[string]gopter.Gen{
			"Symbol":          GenSymbol(),
			"Direction":       gen.Const("short"),
			"EntryPrice":      gen.Const(entry),
			"StopLoss":        gen.Const(entry * 1.05),
			"TakeProfit":      gen.Const(entry * 0.85),
			"CurrentStopLoss": gen.Const(entry * 1.05),
			"PositionSizeUSD": gen.Float64Range(100, 5000),
			"Leverage":        gen.IntRange(1, 10),
			"Status":          gen.Const("active"),
			"Confidence":      gen.IntRange(60, 100),
			"MinHoldMinutes":  gen.IntRange(30, 120),
			"EntryReason":     gen.Const("测试空头计划"),
		})
	}, reflect.TypeOf(decision.TradePlan{}))
}

// GenTradeStatistics 生成随机交易统计
func GenTradeStatistics() gopter.Gen {
	return gen.Struct(reflect.TypeOf(decision.TradeStatistics{}), map[string]gopter.Gen{
		"TotalTrades":       gen.IntRange(0, 1000),
		"WinningTrades":     gen.IntRange(0, 500),
		"LosingTrades":      gen.IntRange(0, 500),
		"TotalPnL":          gen.Float64Range(-100, 500),
		"AverageWin":        gen.Float64Range(0, 50),
		"AverageLoss":       gen.Float64Range(0, 50),
		"WinRate":           gen.Float64Range(0, 1),
		"ProfitFactor":      gen.Float64Range(0, 10),
		"SharpeRatio":       gen.Float64Range(-5, 10),
		"SortinoRatio":      gen.Float64Range(-5, 10),
		"MaxDrawdown":       gen.Float64Range(0, 100),
		"AverageHoldTime":   gen.Float64Range(0, 10000),
		"ConsecutiveWins":   gen.IntRange(0, 20),
		"ConsecutiveLosses": gen.IntRange(0, 20),
		"MaxConsecLosses":   gen.IntRange(0, 20),
	})
}

// GenAccountInfo 生成随机账户信息
func GenAccountInfo() gopter.Gen {
	return gen.Struct(reflect.TypeOf(decision.AccountInfo{}), map[string]gopter.Gen{
		"TotalEquity":      gen.Float64Range(100, 1000000),
		"AvailableBalance": gen.Float64Range(0, 500000),
		"TotalPnL":         gen.Float64Range(-50000, 100000),
		"TotalPnLPct":      gen.Float64Range(-50, 200),
		"MarginUsed":       gen.Float64Range(0, 500000),
		"MarginUsedPct":    gen.Float64Range(0, 100),
		"PositionCount":    gen.IntRange(0, 3),
	})
}

// GenReturnsSlice 生成长度为 n 的随机收益率序列
func GenReturnsSlice(n int) gopter.Gen {
	if n == 0 {
		return gen.Const([]float64{})
	}
	return gen.SliceOfN(n, gen.Float64Range(-20, 50)).
		Map(func(s []float64) []float64 { return s })
}

// GenVariableReturnsSlice 生成长度在 [minLen, maxLen] 之间的随机收益率序列
func GenVariableReturnsSlice(minLen, maxLen int) gopter.Gen {
	return gen.IntRange(minLen, maxLen).FlatMap(func(n interface{}) gopter.Gen {
		return GenReturnsSlice(n.(int))
	}, reflect.TypeOf([]float64{}))
}

// ============================================================================
// market 包生成器
// ============================================================================

// GenKlineSlice 生成长度为 n 的 K 线序列（价格随机游走，确保连续性）
func GenKlineSlice(n int) gopter.Gen {
	return gen.Float64Range(100, 50000).Map(func(startPrice interface{}) []market.Kline {
		price := startPrice.(float64)
		klines := make([]market.Kline, n)
		baseTime := int64(1700000000000)
		for i := 0; i < n; i++ {
			// 简单随机游走：±0.5%
			delta := price * 0.005
			if i%2 == 0 {
				price += delta
			} else {
				price -= delta * 0.8
			}
			if price < 1 {
				price = 1
			}
			klines[i] = market.Kline{
				OpenTime:  baseTime + int64(i)*180000,
				Open:      price * 0.999,
				High:      price * 1.003,
				Low:       price * 0.997,
				Close:     price,
				Volume:    1000 + float64(i)*10,
				CloseTime: baseTime + int64(i)*180000 + 179999,
			}
		}
		return klines
	})
}

// GenMarketData 生成随机 market.Data（含基础指标，不依赖网络请求）
func GenMarketData() gopter.Gen {
	return gen.Float64Range(100, 100000).Map(func(price interface{}) *market.Data {
		p := price.(float64)
		return &market.Data{
			Symbol:         "BTCUSDT",
			CurrentPrice:   p,
			PriceChange1h:  -1.0,
			PriceChange4h:  -2.0,
			CurrentEMA20:   p * 0.99,
			CurrentEMA50:   p * 0.98,
			CurrentMACD:    p * 0.001,
			CurrentRSI7:    50.0,
			CurrentRSI14:   50.0,
			CurrentADX:     25.0,
			CurrentDIPlus:  20.0,
			CurrentDIMinus: 15.0,
			BollingerWidth: 5.0,
			OpenInterest: &market.OIData{
				Latest:   1000,
				Average:  950,
				Change1h: 1.0,
				Change4h: 2.0,
			},
			FundingRate: 0.0001,
			LongerTermContext: &market.LongerTermData{
				EMA20:    p * 0.99,
				EMA50:    p * 0.98,
				ATR3:     p * 0.01,
				ATR14:    p * 0.015,
				ATRRatio: 0.67,
			},
			IntradaySeries: &market.IntradayData{
				MidPrices: []float64{p * 0.99, p * 0.995, p, p * 1.001, p * 1.002},
			},
			MidTermSeries1h: &market.MidTermData1h{
				MidPrices: []float64{p * 0.98, p * 0.99, p, p * 1.01, p * 1.02},
			},
		}
	})
}

// GenTimestamp 生成随机时间戳（过去 30 天内）
func GenTimestamp() gopter.Gen {
	now := time.Now()
	return gen.Int64Range(0, 30*24*60*60).Map(func(secondsAgo interface{}) time.Time {
		return now.Add(-time.Duration(secondsAgo.(int64)) * time.Second)
	})
}
