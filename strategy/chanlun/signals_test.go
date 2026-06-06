package chanlun

import (
	"nofx/market"
	"testing"
)

func TestSignalConfidenceUsesTrendContext(t *testing.T) {
	alignedData := &market.Data{
		Symbol:         "BTCUSDT",
		PriceChange1h:  -0.8,
		PriceChange4h:  -2.1,
		CurrentADX:     34,
		CurrentDIPlus:  12,
		CurrentDIMinus: 32,
		MidTermSeries1h: &market.MidTermData1h{
			ADXValues: []float64{34},
			DIPlus:    []float64{12},
			DIMinus:   []float64{32},
		},
	}
	aligned, metrics := calculateSignalConfidence(SignalInput{
		MarketData:   alignedData,
		ADXTimeframe: "1h",
		MAKiss:       KissResult{Position: "male", KissType: "fly"},
	}, SignalSell2, SideShort, DivergenceResult{})
	if aligned < 82 {
		t.Fatalf("趋势同向空单置信度应达到开仓基础门槛，实际=%d metrics=%+v", aligned, metrics)
	}
	if metrics["confidence_di_aligned"] != true {
		t.Fatalf("应记录DI同向诊断: %+v", metrics)
	}

	counterData := &market.Data{
		Symbol:         "BTCUSDT",
		PriceChange1h:  0.8,
		PriceChange4h:  2.1,
		CurrentADX:     34,
		CurrentDIPlus:  32,
		CurrentDIMinus: 12,
		MidTermSeries1h: &market.MidTermData1h{
			ADXValues: []float64{34},
			DIPlus:    []float64{32},
			DIMinus:   []float64{12},
		},
	}
	counter, counterMetrics := calculateSignalConfidence(SignalInput{
		MarketData:   counterData,
		ADXTimeframe: "1h",
		MAKiss:       KissResult{Position: "female", KissType: "wet"},
	}, SignalSell2, SideShort, DivergenceResult{})
	if counter >= 82 {
		t.Fatalf("逆DI空单置信度不应达到空单基础门槛，实际=%d metrics=%+v", counter, counterMetrics)
	}
	if aligned <= counter {
		t.Fatalf("同向趋势置信度应高于逆向趋势: aligned=%d counter=%d", aligned, counter)
	}
}
