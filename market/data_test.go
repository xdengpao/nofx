package market

import (
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

// ─── 辅助函数 ────────────────────────────────────────────────────────────────

// makeKlines 从收盘价切片生成 K 线（High = Close+1, Low = Close-1）
func makeKlines(closes []float64) []Kline {
	klines := make([]Kline, len(closes))
	for i, c := range closes {
		klines[i] = Kline{
			Open:  c,
			High:  c + 1,
			Low:   c - 1,
			Close: c,
		}
	}
	return klines
}

// makeKlinesN 生成 n 根收盘价固定为 price 的 K 线
func makeKlinesN(n int, price float64) []Kline {
	closes := make([]float64, n)
	for i := range closes {
		closes[i] = price
	}
	return makeKlines(closes)
}

// checkSeriesLen 断言切片长度不超过 10
func checkSeriesLen(t *testing.T, name string, s []float64) {
	t.Helper()
	if len(s) > 10 {
		t.Errorf("%s 长度 %d 超过上限 10", name, len(s))
	}
}

// buildSampleData 构造包含所有字段的 Data 样本
func buildSampleData() *Data {
	klines60 := makeKlinesN(60, 50000.0)
	klines80 := makeKlinesN(80, 50000.0)

	return &Data{
		Symbol:            "BTCUSDT",
		CurrentPrice:      50000.0,
		PriceChange1h:     1.5,
		PriceChange4h:     -0.5,
		CurrentEMA20:      calculateEMA(klines60, 20),
		CurrentEMA50:      calculateEMA(klines60, 50),
		CurrentMACD:       calculateMACD(klines60),
		CurrentRSI7:       calculateRSI(klines60, 7),
		CurrentRSI14:      calculateRSI(klines60, 14),
		CurrentADX:        30.0,
		CurrentDIPlus:     25.0,
		CurrentDIMinus:    15.0,
		BollingerWidth:    2.5,
		FundingRate:       0.0001,
		IntradaySeries:    calculateIntradaySeriesEnhanced(klines60),
		MidTermSeries15m:  calculateMidTermSeries15mEnhanced(klines60),
		MidTermSeries1h:   calculateMidTermSeries1hEnhanced(klines80),
		LongerTermContext: calculateLongerTermDataEnhanced(klines80),
	}
}

// ─── EMA 单元测试 ─────────────────────────────────────────────────────────────

func TestCalculateEMA_InsufficientData(t *testing.T) {
	result := calculateEMA(makeKlinesN(5, 100), 20)
	if result != 0 {
		t.Errorf("数据不足时期望 0，得到 %f", result)
	}
}

func TestCalculateEMA_ConstantPrice(t *testing.T) {
	result := calculateEMA(makeKlinesN(50, 100.0), 20)
	if math.Abs(result-100.0) > 1e-9 {
		t.Errorf("常数价格时 EMA 应等于该价格，得到 %f", result)
	}
}

func TestCalculateEMA_Monotonicity(t *testing.T) {
	n := 50
	asc := make([]float64, n)
	desc := make([]float64, n)
	for i := 0; i < n; i++ {
		asc[i] = float64(i + 1)
		desc[i] = float64(n - i)
	}
	emaAsc := calculateEMA(makeKlines(asc), 20)
	emaDes := calculateEMA(makeKlines(desc), 20)
	if emaAsc <= emaDes {
		t.Errorf("单调递增序列的 EMA(%f) 应大于单调递减序列的 EMA(%f)", emaAsc, emaDes)
	}
}

// ─── RSI 单元测试 ─────────────────────────────────────────────────────────────

func TestCalculateRSI_InsufficientData(t *testing.T) {
	result := calculateRSI(makeKlinesN(5, 100), 14)
	if result != 0 {
		t.Errorf("数据不足时期望 0，得到 %f", result)
	}
}

func TestCalculateRSI_AllGains(t *testing.T) {
	closes := make([]float64, 30)
	for i := range closes {
		closes[i] = float64(i + 1)
	}
	result := calculateRSI(makeKlines(closes), 14)
	if math.Abs(result-100.0) > 1e-6 {
		t.Errorf("全部上涨时 RSI 应为 100，得到 %f", result)
	}
}

func TestCalculateRSI_AllLosses(t *testing.T) {
	closes := make([]float64, 30)
	for i := range closes {
		closes[i] = float64(30 - i)
	}
	result := calculateRSI(makeKlines(closes), 14)
	if math.Abs(result) > 1e-6 {
		t.Errorf("全部下跌时 RSI 应为 0，得到 %f", result)
	}
}

// ─── ATR 单元测试 ─────────────────────────────────────────────────────────────

func TestCalculateATR_InsufficientData(t *testing.T) {
	result := calculateATR(makeKlinesN(5, 100), 14)
	if result != 0 {
		t.Errorf("数据不足时期望 0，得到 %f", result)
	}
}

func TestCalculateATR_NonNegative(t *testing.T) {
	result := calculateATR(makeKlinesN(30, 100.0), 14)
	if result < 0 {
		t.Errorf("ATR 不应为负数，得到 %f", result)
	}
}

// ─── ADX 单元测试 ─────────────────────────────────────────────────────────────

func TestCalculateADX_InsufficientData(t *testing.T) {
	adx, diPlus, diMinus := calculateADX(makeKlinesN(10, 100), 14)
	if adx != 0 || diPlus != 0 || diMinus != 0 {
		t.Errorf("数据不足时期望全 0，得到 adx=%f diPlus=%f diMinus=%f", adx, diPlus, diMinus)
	}
}

// ─── 布林带单元测试 ───────────────────────────────────────────────────────────

func TestCalculateBollingerBands_InsufficientData(t *testing.T) {
	upper, lower, width := calculateBollingerBands(makeKlinesN(5, 100), 20, 2.0)
	if upper != 0 || lower != 0 || width != 0 {
		t.Errorf("数据不足时期望全 0，得到 upper=%f lower=%f width=%f", upper, lower, width)
	}
}

func TestCalculateBollingerBands_ConstantPrice(t *testing.T) {
	upper, lower, width := calculateBollingerBands(makeKlinesN(30, 100.0), 20, 2.0)
	if math.Abs(upper-lower) > 1e-9 {
		t.Errorf("常数价格时上下轨应相等，upper=%f lower=%f", upper, lower)
	}
	if math.Abs(width) > 1e-9 {
		t.Errorf("常数价格时宽度应为 0，得到 %f", width)
	}
}

func TestCalculateBollingerBands_UpperGTLower(t *testing.T) {
	closes := make([]float64, 30)
	for i := range closes {
		closes[i] = 100.0 + float64(i%5) // 有波动
	}
	upper, lower, _ := calculateBollingerBands(makeKlines(closes), 20, 2.0)
	if upper <= lower {
		t.Errorf("有波动时上轨(%f)应大于下轨(%f)", upper, lower)
	}
}

// ─── MACD 单元测试 ────────────────────────────────────────────────────────────

func TestCalculateMACDFull_InsufficientData(t *testing.T) {
	macdLine, signalLine, hist := calculateMACDFull(makeKlinesN(20, 100))
	if macdLine != 0 || signalLine != 0 || hist != 0 {
		t.Errorf("数据不足时期望全 0，得到 macd=%f signal=%f hist=%f", macdLine, signalLine, hist)
	}
}

// ─── 序列长度上限单元测试 ─────────────────────────────────────────────────────

func TestCalculateIntradaySeriesEnhanced_LengthLimit(t *testing.T) {
	data := calculateIntradaySeriesEnhanced(makeKlinesN(60, 100.0))
	checkSeriesLen(t, "MidPrices", data.MidPrices)
	checkSeriesLen(t, "EMA20Values", data.EMA20Values)
	checkSeriesLen(t, "RSI7Values", data.RSI7Values)
	checkSeriesLen(t, "RSI14Values", data.RSI14Values)
	checkSeriesLen(t, "ATRValues", data.ATRValues)
	checkSeriesLen(t, "MACDValues", data.MACDValues)
}

func TestCalculateMidTermSeries15m_LengthLimit(t *testing.T) {
	data := calculateMidTermSeries15mEnhanced(makeKlinesN(70, 100.0))
	checkSeriesLen(t, "MidPrices", data.MidPrices)
	checkSeriesLen(t, "ADXValues", data.ADXValues)
	checkSeriesLen(t, "ATRValues", data.ATRValues)
}

func TestCalculateMidTermSeries1h_LengthLimit(t *testing.T) {
	data := calculateMidTermSeries1hEnhanced(makeKlinesN(80, 100.0))
	checkSeriesLen(t, "MidPrices", data.MidPrices)
	checkSeriesLen(t, "ADXValues", data.ADXValues)
}

func TestCalculateLongerTermData_LengthLimit(t *testing.T) {
	data := calculateLongerTermDataEnhanced(makeKlinesN(80, 100.0))
	checkSeriesLen(t, "MACDValues", data.MACDValues)
	checkSeriesLen(t, "RSI14Values", data.RSI14Values)
	checkSeriesLen(t, "ADXValues", data.ADXValues)
}

// ─── Format 单元测试 ──────────────────────────────────────────────────────────

func TestFormat_ContainsKeywords(t *testing.T) {
	output := Format(buildSampleData())
	for _, kw := range []string{"价格", "EMA", "MACD", "RSI", "ADX"} {
		if !strings.Contains(output, kw) {
			t.Errorf("Format 输出缺少关键字: %s", kw)
		}
	}
}

func TestFormat_NonEmpty(t *testing.T) {
	if strings.TrimSpace(Format(buildSampleData())) == "" {
		t.Error("Format 输出不应为空")
	}
}

// ─── 工具函数单元测试 ─────────────────────────────────────────────────────────

func TestNormalize(t *testing.T) {
	cases := []struct{ input, expected string }{
		{"btc", "BTCUSDT"},
		{"BTCUSDT", "BTCUSDT"},
		{"eth", "ETHUSDT"},
		{"ETHUSDT", "ETHUSDT"},
	}
	for _, c := range cases {
		if got := Normalize(c.input); got != c.expected {
			t.Errorf("Normalize(%q) = %q，期望 %q", c.input, got, c.expected)
		}
	}
}

func TestGetLastValue(t *testing.T) {
	if got := GetLastValue([]float64{1, 2, 3, 4, 5}); got != 5 {
		t.Errorf("期望 5，得到 %f", got)
	}
	if got := GetLastValue(nil); got != 0 {
		t.Errorf("空切片期望 0，得到 %f", got)
	}
}

func TestGetLastNValues(t *testing.T) {
	result := GetLastNValues([]float64{1, 2, 3, 4, 5}, 3)
	if len(result) != 3 || result[0] != 3 || result[2] != 5 {
		t.Errorf("期望 [3,4,5]，得到 %v", result)
	}
}

func TestCalculateCorrelation_PerfectPositive(t *testing.T) {
	prices := make([]float64, 20)
	for i := range prices {
		prices[i] = float64(i + 1)
	}
	if corr := CalculateCorrelation(prices, prices); math.Abs(corr-1.0) > 1e-9 {
		t.Errorf("完全正相关时相关系数应为 1，得到 %f", corr)
	}
}

func TestCalculateCorrelation_PerfectNegative(t *testing.T) {
	n := 20
	p1, p2 := make([]float64, n), make([]float64, n)
	for i := range p1 {
		p1[i] = float64(i + 1)
		p2[i] = float64(n - i)
	}
	if corr := CalculateCorrelation(p1, p2); math.Abs(corr+1.0) > 1e-9 {
		t.Errorf("完全负相关时相关系数应为 -1，得到 %f", corr)
	}
}

// ─── 属性基测试 ───────────────────────────────────────────────────────────────

// Feature: quant-trading-system, Property 27: 技术指标值域
// 对任意有效 K 线数据，RSI ∈ [0,100]，ATR ≥ 0，EMA > 0
func TestProperty27_IndicatorValueRange(t *testing.T) {
	params := gopter.DefaultTestParameters()
	params.MinSuccessfulTests = 100
	properties := gopter.NewProperties(params)

	// 生成 50 根收盘价在 [1, 100000] 的 K 线
	klinesGen := gen.SliceOfN(50, gen.Float64Range(1, 100000)).
		Map(func(closes []float64) []Kline {
			return makeKlines(closes)
		})

	properties.Property("RSI 值域 [0,100]", prop.ForAll(
		func(klines []Kline) bool {
			rsi7 := calculateRSI(klines, 7)
			rsi14 := calculateRSI(klines, 14)
			return rsi7 >= 0 && rsi7 <= 100 && rsi14 >= 0 && rsi14 <= 100
		},
		klinesGen,
	))

	properties.Property("ATR 非负", prop.ForAll(
		func(klines []Kline) bool {
			return calculateATR(klines, 14) >= 0
		},
		klinesGen,
	))

	properties.Property("EMA 正值（收盘价 > 0 时）", prop.ForAll(
		func(klines []Kline) bool {
			return calculateEMA(klines, 20) > 0
		},
		klinesGen,
	))

	properties.TestingRun(t)
}

// Feature: quant-trading-system, Property 28: 指标序列长度上限
// 对任意足够长的 K 线数据，各指标序列长度应 ≤ 10
func TestProperty28_SeriesLengthLimit(t *testing.T) {
	params := gopter.DefaultTestParameters()
	params.MinSuccessfulTests = 100
	properties := gopter.NewProperties(params)

	// 生成 60~100 根 K 线
	klinesGen := gen.IntRange(60, 100).FlatMap(
		func(n interface{}) gopter.Gen {
			return gen.SliceOfN(n.(int), gen.Float64Range(1, 100000)).
				Map(func(closes []float64) []Kline {
					return makeKlines(closes)
				})
		},
		reflect.TypeOf([]Kline{}),
	)

	properties.Property("IntradaySeries 各序列长度 ≤ 10", prop.ForAll(
		func(klines []Kline) bool {
			d := calculateIntradaySeriesEnhanced(klines)
			return len(d.MidPrices) <= 10 &&
				len(d.EMA20Values) <= 10 &&
				len(d.RSI7Values) <= 10 &&
				len(d.RSI14Values) <= 10 &&
				len(d.ATRValues) <= 10 &&
				len(d.MACDValues) <= 10
		},
		klinesGen,
	))

	properties.Property("MidTermSeries15m 各序列长度 ≤ 10", prop.ForAll(
		func(klines []Kline) bool {
			d := calculateMidTermSeries15mEnhanced(klines)
			return len(d.MidPrices) <= 10 &&
				len(d.ADXValues) <= 10 &&
				len(d.ATRValues) <= 10
		},
		klinesGen,
	))

	properties.Property("LongerTermData 各序列长度 ≤ 10", prop.ForAll(
		func(klines []Kline) bool {
			d := calculateLongerTermDataEnhanced(klines)
			return len(d.MACDValues) <= 10 &&
				len(d.RSI14Values) <= 10 &&
				len(d.ADXValues) <= 10
		},
		klinesGen,
	))

	properties.TestingRun(t)
}

// Feature: quant-trading-system, Property 29: 市场数据格式化完整性
// 对任意有效 market.Data，Format() 应产生包含价格、EMA、MACD、RSI、ADX 关键字的非空字符串
func TestProperty29_FormatCompleteness(t *testing.T) {
	params := gopter.DefaultTestParameters()
	params.MinSuccessfulTests = 100
	properties := gopter.NewProperties(params)

	properties.Property("Format 输出包含所有关键指标关键字", prop.ForAll(
		func(price float64) bool {
			data := &Data{
				Symbol:         "BTCUSDT",
				CurrentPrice:   price,
				PriceChange1h:  1.0,
				PriceChange4h:  -0.5,
				CurrentEMA20:   price * 0.99,
				CurrentEMA50:   price * 0.98,
				CurrentMACD:    0.001,
				CurrentRSI7:    55.0,
				CurrentRSI14:   50.0,
				CurrentADX:     25.0,
				CurrentDIPlus:  20.0,
				CurrentDIMinus: 15.0,
				BollingerWidth: 2.0,
				FundingRate:    0.0001,
			}
			output := Format(data)
			if len(output) == 0 {
				return false
			}
			for _, kw := range []string{"价格", "EMA", "MACD", "RSI", "ADX"} {
				if !strings.Contains(output, kw) {
					return false
				}
			}
			return true
		},
		gen.Float64Range(0.01, 1000000),
	))

	properties.TestingRun(t)
}
