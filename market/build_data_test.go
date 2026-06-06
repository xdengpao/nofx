package market

import (
	"testing"
	"time"
)

func TestBuildDataFromKlinesDisabledEnrichment(t *testing.T) {
	bundle := KlineBundle{
		M3:  testKlines(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 80, 3*time.Minute),
		M15: testKlines(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 80, 15*time.Minute),
		H1:  testKlines(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 80, time.Hour),
		H4:  testKlines(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), 80, 4*time.Hour),
	}
	data, err := BuildDataFromKlines("BTCUSDT", bundle, BuildDataOptions{EnrichmentMode: "disabled", IncludeMicroADX: true})
	if err != nil {
		t.Fatalf("构建market data失败: %v", err)
	}
	if data.OpenInterest == nil {
		t.Fatal("OI disabled时也应返回空OI结构")
	}
	if data.OIValueUSD != 0 || data.FundingRate != 0 {
		t.Fatalf("disabled模式不应填充实时OI/funding: oi=%.2f funding=%.4f", data.OIValueUSD, data.FundingRate)
	}
	if len(data.Klines["3m"]) != 80 {
		t.Fatalf("应保留K线序列")
	}
	if data.IntradaySeries == nil || len(data.IntradaySeries.ADXValues) == 0 {
		t.Fatalf("IncludeMicroADX应计算3m ADX序列")
	}
}

func TestBuildDataFromKlinesRequiresAllTimeframes(t *testing.T) {
	_, err := BuildDataFromKlines("BTCUSDT", KlineBundle{M3: testKlines(time.Now(), 10, time.Minute)}, BuildDataOptions{EnrichmentMode: "disabled"})
	if err == nil {
		t.Fatal("缺少timeframe应失败")
	}
}

func testKlines(start time.Time, n int, step time.Duration) []Kline {
	klines := make([]Kline, 0, n)
	price := 100.0
	for i := 0; i < n; i++ {
		open := start.Add(time.Duration(i) * step)
		closeTime := open.Add(step).Add(-time.Millisecond)
		klines = append(klines, Kline{
			OpenTime:  open.UnixMilli(),
			CloseTime: closeTime.UnixMilli(),
			Open:      price,
			High:      price + 2,
			Low:       price - 1,
			Close:     price + 1,
			Volume:    1000,
		})
		price += 0.2
	}
	return klines
}
