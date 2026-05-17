package chanlun

import (
	"nofx/decision"
	"path/filepath"
	"testing"
	"time"
)

func testCandles(values ...float64) []Candle {
	candles := make([]Candle, len(values))
	for i, v := range values {
		candles[i] = Candle{
			OpenTime:  int64(i * 1000),
			CloseTime: int64(i*1000 + 999),
			Open:      v,
			High:      v + 1,
			Low:       v - 1,
			Close:     v,
			Volume:    1,
		}
	}
	return candles
}

func TestNormalizeInclusion(t *testing.T) {
	candles := []Candle{
		{High: 10, Low: 5, Close: 8},
		{High: 9, Low: 6, Close: 7},
		{High: 11, Low: 8, Close: 10},
	}
	got := NormalizeInclusion(candles, DirectionUp)
	if len(got) != 2 {
		t.Fatalf("包含关系应合并为2根K线: %+v", got)
	}
	if got[0].High != 10 || got[0].Low != 6 {
		t.Fatalf("向上包含处理错误: %+v", got[0])
	}
}

func TestFractalStrokeSegmentCenter(t *testing.T) {
	candles := testCandles(10, 8, 6, 9, 12, 9, 7, 10, 13, 9, 6, 10, 14)
	fractals := FindFractals(candles, 1, 1)
	if len(fractals) < 4 {
		t.Fatalf("应识别多个分型: %+v", fractals)
	}
	strokes := BuildStrokes(fractals, candles, 1, 0, 0)
	if len(strokes) < 3 {
		t.Fatalf("应生成笔: %+v", strokes)
	}
	segments := BuildSegments(strokes, "enhanced")
	if len(segments) != len(strokes) {
		t.Fatalf("enhanced应保留笔线段: %+v", segments)
	}
	centers := BuildCenters([]Segment{
		{ID: "a", High: 12, Low: 8},
		{ID: "b", High: 11, Low: 7},
		{ID: "c", High: 13, Low: 9},
	}, "1h")
	if len(centers) != 1 {
		t.Fatalf("三段重叠应形成中枢: %+v", centers)
	}
	if centers[0].ZG != 11 || centers[0].ZD != 9 {
		t.Fatalf("ZG/ZD计算错误: %+v", centers[0])
	}
}

func TestMACDDivergenceAndMAKiss(t *testing.T) {
	hist := []float64{-5, -4, -3, 0, -2, -1}
	a := Segment{Direction: DirectionDown, Strokes: []Stroke{{Start: Fractal{Index: 0}, End: Fractal{Index: 2}}}}
	c := Segment{Direction: DirectionDown, Strokes: []Stroke{{Start: Fractal{Index: 4}, End: Fractal{Index: 5}}}}
	div := DetectMACDDivergence(a, c, hist, 0.8)
	if !div.Diverged || div.Kind != "bottom" {
		t.Fatalf("应识别底背驰: %+v", div)
	}

	kiss := DetectMAKiss([]float64{10, 9, 8.9, 9.2}, []float64{11, 10, 9, 9}, 0.02, 5)
	if kiss.KissType == "" {
		t.Fatalf("应识别均线吻: %+v", kiss)
	}
}

func TestSignalsAndStateStore(t *testing.T) {
	center := Center{ID: "c1", ZG: 10, ZD: 8}
	if !IsThirdBuyFailed(center, 9.9, nil) {
		t.Fatal("15m close跌破ZG应判定三买失败")
	}
	if !IsThirdSellFailed(center, 8.1, []float64{8.2, 8.3}) {
		t.Fatal("连续两根3m升破ZD应判定三卖失败")
	}

	firstBuy := ChanlunSignal{SignalType: SignalBuy1, Direction: SideLong, StopLoss: 7}
	if !IsSecondBuyConfirmed(firstBuy, 7.5, true) {
		t.Fatal("二买确认失败")
	}

	dir := t.TempDir()
	store := NewStateStore(filepath.Join(dir, "state.json"))
	signal := ChanlunSignal{SignalID: "s1", Symbol: "BTCUSDT", SignalType: SignalBuy1, ConfirmedAt: time.Now()}
	store.StoreConfirmedSignal("t1", "BTCUSDT", signal, false)
	if !store.MarkExecuted("t1", "BTCUSDT", "s1", "open_long") {
		t.Fatal("首次执行应成功记录")
	}
	if store.MarkExecuted("t1", "BTCUSDT", "s1", "open_long") {
		t.Fatal("重复signal_id不应再次执行")
	}
	store.SetLastAnalyzedClosedKline("t1", "BTCUSDT", "1h", 123)
	if err := store.Save(); err != nil {
		t.Fatalf("保存状态失败: %v", err)
	}
	reloaded := NewStateStore(filepath.Join(dir, "state.json"))
	state := reloaded.SymbolState("t1", "BTCUSDT")
	if state.LastAnalyzedClosedKline["1h"] != 123 {
		t.Fatalf("状态恢复失败: %+v", state)
	}
}

func TestResolveProgrammaticSymbols_CustomModesKeepPositions(t *testing.T) {
	candidates := []decision.CandidateCoin{
		{Symbol: "BTCUSDT", Sources: []string{"dynamic"}},
		{Symbol: "ETHUSDT", Sources: []string{"dynamic"}},
	}
	positions := []decision.PositionInfo{{Symbol: "SOLUSDT", Side: "long"}}

	override := ResolveProgrammaticSymbols(candidates, positions, decision.ProgrammaticStrategyPolicy{
		SymbolPool: decision.ProgrammaticSymbolPoolPolicy{
			Mode:        "override",
			Symbols:     []string{"DOGEUSDT"},
			CoreSymbols: []string{"BTCUSDT"},
		},
	})
	if !containsStrategySymbol(override, "DOGEUSDT") || !containsStrategySymbol(override, "BTCUSDT") || !containsStrategySymbol(override, "SOLUSDT") {
		t.Fatalf("override应包含自定义、核心和持仓标的: %+v", override)
	}
	if containsStrategySymbol(override, "ETHUSDT") {
		t.Fatalf("override不应保留未指定候选: %+v", override)
	}

	filter := ResolveProgrammaticSymbols(candidates, positions, decision.ProgrammaticStrategyPolicy{
		SymbolPool: decision.ProgrammaticSymbolPoolPolicy{Mode: "filter", Symbols: []string{"ETHUSDT"}},
	})
	if !containsStrategySymbol(filter, "ETHUSDT") || !containsStrategySymbol(filter, "SOLUSDT") {
		t.Fatalf("filter应保留匹配候选和持仓标的: %+v", filter)
	}
	if containsStrategySymbol(filter, "BTCUSDT") {
		t.Fatalf("filter不应保留未匹配候选: %+v", filter)
	}
}

func containsStrategySymbol(symbols []StrategySymbol, symbol string) bool {
	for _, item := range symbols {
		if item.Symbol == symbol {
			return true
		}
	}
	return false
}
