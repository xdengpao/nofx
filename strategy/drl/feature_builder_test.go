package drl

import (
	"math"
	"nofx/decision"
	"nofx/market"
	"testing"
	"time"
)

func TestFeatureBuilderBuildDimensionAndPadding(t *testing.T) {
	cfg := testEngineConfig()
	builder := NewFeatureBuilder(cfg)
	klines := makeTestKlines(5)
	account := decision.AccountInfo{TotalEquity: 1000, AvailableBalance: 750, SizingEquity: 1000}
	pos := &decision.PositionInfo{Symbol: "ETHUSDT", Side: "short", Quantity: 0.2, EntryPrice: 1000, MarkPrice: 1020, UnrealizedPnL: -4}

	values, err := builder.Build(klines, account, pos)
	if err != nil {
		t.Fatalf("特征构建不应失败: %v", err)
	}
	expectedDim := cfg.ObservationWindow*featuresPerStep + accountFeatures
	if len(values) != expectedDim {
		t.Fatalf("观测维度错误: got=%d want=%d", len(values), expectedDim)
	}
	if !builder.LastStats.ZeroPadded || builder.LastStats.MissingRows != 5 {
		t.Fatalf("K线不足应记录零填充诊断: %+v", builder.LastStats)
	}
	for i, value := range values {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			t.Fatalf("观测向量包含非法值 index=%d value=%v", i, value)
		}
	}
	accountOffset := cfg.ObservationWindow * featuresPerStep
	if values[accountOffset] >= 0 {
		t.Fatalf("空头持仓的仓位比例应为负数: %.4f", values[accountOffset])
	}
	if values[accountOffset+2] != 0.75 {
		t.Fatalf("可用保证金比例应为0.75，实际=%.4f", values[accountOffset+2])
	}
}

func TestZScoreNormalizer(t *testing.T) {
	n := &ZScoreNormalizer{WindowSize: 3}
	out := n.Normalize([]float64{1, 2, 3})
	if len(out) != 3 {
		t.Fatalf("输出维度错误: %+v", out)
	}
	if math.Abs(float64(out[1])) > 1e-6 {
		t.Fatalf("中间值应接近均值: %+v", out)
	}
}

func makeTestKlines(n int) []market.Kline {
	out := make([]market.Kline, n)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	for i := 0; i < n; i++ {
		close := 1000 + float64(i)*2
		out[i] = market.Kline{
			OpenTime:  base + int64(i)*60_000,
			Open:      close - 1,
			High:      close + 5,
			Low:       close - 5,
			Close:     close,
			Volume:    100 + float64(i),
			CloseTime: base + int64(i+1)*60_000 - 1,
		}
	}
	return out
}
