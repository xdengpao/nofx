package drl

import (
	"fmt"
	"math"
	"nofx/config"
	"nofx/decision"
	"nofx/market"
	"strings"
)

type FeatureBuilder struct {
	Config       DRLEngineConfig
	Normalizer   *ZScoreNormalizer
	LastStats    FeatureStats
	LastRaw      []float64
	LastFeatures []float32
}

func NewFeatureBuilder(cfg DRLEngineConfig) *FeatureBuilder {
	return &FeatureBuilder{
		Config:     cfg,
		Normalizer: &ZScoreNormalizer{WindowSize: cfg.ObservationWindow},
	}
}

func (b *FeatureBuilder) Build(klines []market.Kline, account decision.AccountInfo, position *decision.PositionInfo) ([]float32, error) {
	if b == nil {
		return nil, fmt.Errorf("DRL特征构建器为空")
	}
	window := b.Config.ObservationWindow
	if window <= 0 {
		window = 60
	}
	values := make([]float32, window*featuresPerStep+accountFeatures)
	raw := make([]float64, len(values))
	stats := FeatureStats{
		Dimension:         len(values),
		Window:            window,
		FeaturePerStep:    featuresPerStep,
		AccountFeatureDim: accountFeatures,
		AvailableKlines:   len(klines),
	}
	if len(klines) < window {
		stats.MissingRows = window - len(klines)
		stats.ZeroPadded = true
	}
	if len(klines) == 0 {
		fillAccountFeatures(values[window*featuresPerStep:], raw[window*featuresPerStep:], account, position, b.Config.MaxPositionPct)
		b.LastStats = stats
		b.LastRaw = raw
		b.LastFeatures = append(b.LastFeatures[:0], values...)
		return values, nil
	}

	start := len(klines) - window
	targetOffset := 0
	if start < 0 {
		targetOffset = -start
		start = 0
	}
	series := klines[start:]
	ind := buildIndicators(series, b.Config.Features)
	closeStats := statsFor(extract(series, func(k market.Kline) float64 { return k.Close }))
	volumeStats := statsFor(extract(series, func(k market.Kline) float64 { return k.Volume }))
	for i, k := range series {
		target := (targetOffset + i) * featuresPerStep
		close := nonZero(k.Close, 1)
		raw[target+0] = k.Open
		raw[target+1] = k.High
		raw[target+2] = k.Low
		raw[target+3] = k.Close
		raw[target+4] = k.Volume
		raw[target+5] = ind.MACD[i]
		raw[target+6] = ind.MACDSignal[i]
		raw[target+7] = ind.MACDHist[i]
		raw[target+8] = ind.EMAShort[i]
		raw[target+9] = ind.EMALong[i]
		raw[target+10] = ind.RSI[i]
		raw[target+11] = ind.ATR[i]
		raw[target+12] = ind.CCI[i]
		raw[target+13] = ind.BollingerUpper[i]
		raw[target+14] = ind.BollingerMiddle[i]
		raw[target+15] = ind.BollingerLower[i]
		values[target+0] = zscore(k.Open, closeStats)
		values[target+1] = zscore(k.High, closeStats)
		values[target+2] = zscore(k.Low, closeStats)
		values[target+3] = zscore(k.Close, closeStats)
		values[target+4] = zscore(k.Volume, volumeStats)
		values[target+5] = ratio(ind.MACD[i], close)
		values[target+6] = ratio(ind.MACDSignal[i], close)
		values[target+7] = ratio(ind.MACDHist[i], close)
		values[target+8] = ratio(ind.EMAShort[i]-k.Close, close)
		values[target+9] = ratio(ind.EMALong[i]-k.Close, close)
		values[target+10] = float32(clipFloat64(ind.RSI[i]/100.0, 0, 1))
		values[target+11] = ratio(ind.ATR[i], close)
		values[target+12] = float32(clipFloat64(ind.CCI[i]/200.0, -1, 1))
		values[target+13] = ratio(ind.BollingerUpper[i]-k.Close, close)
		values[target+14] = ratio(ind.BollingerMiddle[i]-k.Close, close)
		values[target+15] = ratio(ind.BollingerLower[i]-k.Close, close)
	}
	last := klines[len(klines)-1]
	stats.LastClose = last.Close
	if len(ind.ATR) > 0 {
		stats.LastATR = ind.ATR[len(ind.ATR)-1]
	}
	fillAccountFeatures(values[window*featuresPerStep:], raw[window*featuresPerStep:], account, position, b.Config.MaxPositionPct)
	b.LastStats = stats
	b.LastRaw = raw
	b.LastFeatures = append(b.LastFeatures[:0], values...)
	return values, nil
}

type ZScoreNormalizer struct {
	WindowSize int
}

func (n *ZScoreNormalizer) Normalize(raw []float64) []float32 {
	stats := statsFor(raw)
	out := make([]float32, len(raw))
	for i, value := range raw {
		out[i] = zscore(value, stats)
	}
	return out
}

type seriesStats struct {
	Mean float64
	Std  float64
}

type indicatorSeries struct {
	EMAShort        []float64
	EMALong         []float64
	MACD            []float64
	MACDSignal      []float64
	MACDHist        []float64
	RSI             []float64
	ATR             []float64
	CCI             []float64
	BollingerUpper  []float64
	BollingerMiddle []float64
	BollingerLower  []float64
}

func buildIndicators(klines []market.Kline, cfg config.DRLFeatureConfig) indicatorSeries {
	closes := extract(klines, func(k market.Kline) float64 { return k.Close })
	out := indicatorSeries{
		EMAShort:        make([]float64, len(klines)),
		EMALong:         make([]float64, len(klines)),
		MACD:            make([]float64, len(klines)),
		MACDSignal:      make([]float64, len(klines)),
		MACDHist:        make([]float64, len(klines)),
		RSI:             make([]float64, len(klines)),
		ATR:             make([]float64, len(klines)),
		CCI:             make([]float64, len(klines)),
		BollingerUpper:  make([]float64, len(klines)),
		BollingerMiddle: make([]float64, len(klines)),
		BollingerLower:  make([]float64, len(klines)),
	}
	if len(klines) == 0 {
		return out
	}
	if featureEnabled(cfg.IncludeEMA) {
		out.EMAShort = emaSeries(closes, firstPositive(cfg.EMAShortPeriod, 12))
		out.EMALong = emaSeries(closes, firstPositive(cfg.EMALongPeriod, 26))
	}
	if featureEnabled(cfg.IncludeMACD) {
		fast := emaSeries(closes, 12)
		slow := emaSeries(closes, 26)
		for i := range closes {
			out.MACD[i] = fast[i] - slow[i]
		}
		out.MACDSignal = emaSeries(out.MACD, 9)
		for i := range closes {
			out.MACDHist[i] = out.MACD[i] - out.MACDSignal[i]
		}
	}
	if featureEnabled(cfg.IncludeRSI) {
		out.RSI = rsiSeries(closes, firstPositive(cfg.RSIPeriod, 14))
	}
	if featureEnabled(cfg.IncludeATR) {
		out.ATR = atrSeries(klines, firstPositive(cfg.ATRPeriod, 14))
	}
	if featureEnabled(cfg.IncludeCCI) {
		out.CCI = cciSeries(klines, firstPositive(cfg.CCIPeriod, 20))
	}
	if featureEnabled(cfg.IncludeBollinger) {
		out.BollingerUpper, out.BollingerMiddle, out.BollingerLower = bollingerSeries(closes, firstPositive(cfg.BollingerPeriod, 20), nonZero(cfg.BollingerStdDev, 2.0))
	}
	return out
}

func fillAccountFeatures(target []float32, raw []float64, account decision.AccountInfo, position *decision.PositionInfo, maxPositionPct float64) {
	if len(target) < accountFeatures || len(raw) < accountFeatures {
		return
	}
	equity := decision.AccountSizingEquity(account)
	if equity <= 0 {
		equity = account.TotalEquity
	}
	if equity <= 0 {
		equity = 1
	}
	positionRatio := 0.0
	unrealizedRatio := account.TotalPnL / equity
	if position != nil {
		price := position.MarkPrice
		if price <= 0 {
			price = position.EntryPrice
		}
		notional := math.Abs(position.Quantity * price)
		denominator := equity * nonZero(maxPositionPct, 0.3)
		if denominator > 0 {
			positionRatio = notional / denominator
		}
		if strings.EqualFold(position.Side, "short") {
			positionRatio *= -1
		}
		unrealizedRatio = position.UnrealizedPnL / equity
	}
	target[0] = float32(clipFloat64(positionRatio, -1, 1))
	target[1] = float32(clipFloat64(unrealizedRatio, -1, 1))
	target[2] = float32(clipFloat64(account.AvailableBalance/equity, 0, 1))
	raw[0] = positionRatio
	raw[1] = unrealizedRatio
	raw[2] = account.AvailableBalance / equity
}

func featureEnabled(value *bool) bool {
	return value == nil || *value
}

func extract(klines []market.Kline, f func(market.Kline) float64) []float64 {
	out := make([]float64, len(klines))
	for i, k := range klines {
		out[i] = f(k)
	}
	return out
}

func statsFor(values []float64) seriesStats {
	if len(values) == 0 {
		return seriesStats{Std: 1}
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	mean := sum / float64(len(values))
	variance := 0.0
	for _, value := range values {
		diff := value - mean
		variance += diff * diff
	}
	std := math.Sqrt(variance / float64(len(values)))
	if std < 1e-8 {
		std = 1
	}
	return seriesStats{Mean: mean, Std: std}
}

func zscore(value float64, stats seriesStats) float32 {
	return float32((value - stats.Mean) / (stats.Std + 1e-8))
}

func ratio(value, base float64) float32 {
	if math.Abs(base) < 1e-8 {
		return 0
	}
	return float32(clipFloat64(value/base, -10, 10))
}

func emaSeries(values []float64, period int) []float64 {
	out := make([]float64, len(values))
	if len(values) == 0 {
		return out
	}
	if period <= 0 {
		period = 1
	}
	alpha := 2.0 / (float64(period) + 1.0)
	out[0] = values[0]
	for i := 1; i < len(values); i++ {
		out[i] = values[i]*alpha + out[i-1]*(1-alpha)
	}
	return out
}

func rsiSeries(closes []float64, period int) []float64 {
	out := make([]float64, len(closes))
	if len(closes) == 0 {
		return out
	}
	if period <= 0 {
		period = 14
	}
	for i := range closes {
		if i == 0 {
			out[i] = 50
			continue
		}
		start := i - period + 1
		if start < 1 {
			start = 1
		}
		gain := 0.0
		loss := 0.0
		for j := start; j <= i; j++ {
			diff := closes[j] - closes[j-1]
			if diff >= 0 {
				gain += diff
			} else {
				loss -= diff
			}
		}
		if loss == 0 {
			out[i] = 100
		} else {
			rs := gain / loss
			out[i] = 100 - (100 / (1 + rs))
		}
	}
	return out
}

func atrSeries(klines []market.Kline, period int) []float64 {
	out := make([]float64, len(klines))
	if len(klines) == 0 {
		return out
	}
	for i := range klines {
		start := i - period + 1
		if start < 0 {
			start = 0
		}
		total := 0.0
		count := 0
		for j := start; j <= i; j++ {
			prevClose := klines[j].Close
			if j > 0 {
				prevClose = klines[j-1].Close
			}
			tr := math.Max(klines[j].High-klines[j].Low, math.Max(math.Abs(klines[j].High-prevClose), math.Abs(klines[j].Low-prevClose)))
			total += tr
			count++
		}
		if count > 0 {
			out[i] = total / float64(count)
		}
	}
	return out
}

func cciSeries(klines []market.Kline, period int) []float64 {
	out := make([]float64, len(klines))
	typical := make([]float64, len(klines))
	for i, k := range klines {
		typical[i] = (k.High + k.Low + k.Close) / 3
	}
	for i := range klines {
		start := i - period + 1
		if start < 0 {
			start = 0
		}
		window := typical[start : i+1]
		stats := statsFor(window)
		meanDev := 0.0
		for _, value := range window {
			meanDev += math.Abs(value - stats.Mean)
		}
		meanDev /= float64(len(window))
		if meanDev > 1e-8 {
			out[i] = (typical[i] - stats.Mean) / (0.015 * meanDev)
		}
	}
	return out
}

func bollingerSeries(closes []float64, period int, stdDev float64) ([]float64, []float64, []float64) {
	upper := make([]float64, len(closes))
	middle := make([]float64, len(closes))
	lower := make([]float64, len(closes))
	for i := range closes {
		start := i - period + 1
		if start < 0 {
			start = 0
		}
		stats := statsFor(closes[start : i+1])
		middle[i] = stats.Mean
		upper[i] = stats.Mean + stdDev*stats.Std
		lower[i] = stats.Mean - stdDev*stats.Std
	}
	return upper, middle, lower
}

func firstPositive(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func nonZero(value, fallback float64) float64 {
	if math.Abs(value) > 1e-8 {
		return value
	}
	return fallback
}
