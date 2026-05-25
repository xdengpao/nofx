package chanlunv2

func calculateV2MACDHistogram(closes []float64, fast, slow, signal int) []float64 {
	out := make([]float64, len(closes))
	if len(closes) == 0 || fast <= 0 || slow <= 0 || signal <= 0 || fast >= slow || len(closes) < slow+signal-1 {
		return out
	}

	fastEMA := calculateV2EMA(closes, fast)
	slowEMA := calculateV2EMA(closes, slow)
	macd := make([]float64, len(closes))
	validMACD := make([]bool, len(closes))
	for i := range closes {
		if fastEMA[i] == 0 || slowEMA[i] == 0 {
			continue
		}
		macd[i] = fastEMA[i] - slowEMA[i]
		validMACD[i] = true
	}

	signalEMA := calculateV2SignalEMA(macd, validMACD, signal)
	for i := range closes {
		if !validMACD[i] || signalEMA[i] == 0 {
			continue
		}
		out[i] = macd[i] - signalEMA[i]
	}
	return out
}

func calculateV2EMA(values []float64, period int) []float64 {
	out := make([]float64, len(values))
	if period <= 0 || len(values) < period {
		return out
	}
	sum := 0.0
	for i := 0; i < period; i++ {
		sum += values[i]
	}
	out[period-1] = sum / float64(period)
	multiplier := 2.0 / float64(period+1)
	for i := period; i < len(values); i++ {
		out[i] = (values[i]-out[i-1])*multiplier + out[i-1]
	}
	return out
}

func calculateV2SignalEMA(values []float64, valid []bool, period int) []float64 {
	out := make([]float64, len(values))
	if period <= 0 || len(values) == 0 {
		return out
	}
	sum := 0.0
	count := 0
	start := -1
	for i, value := range values {
		if !valid[i] {
			continue
		}
		sum += value
		count++
		if count == period {
			start = i
			break
		}
	}
	if start < 0 {
		return out
	}
	out[start] = sum / float64(period)
	multiplier := 2.0 / float64(period+1)
	for i := start + 1; i < len(values); i++ {
		if !valid[i] {
			continue
		}
		out[i] = (values[i]-out[i-1])*multiplier + out[i-1]
	}
	return out
}
