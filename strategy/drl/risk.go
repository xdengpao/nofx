package drl

import "math"

func atrStopLossTakeProfit(action string, referencePrice, atr float64, cfg *DRLEngineConfig) (float64, float64) {
	if referencePrice <= 0 {
		return 0, 0
	}
	if atr <= 0 {
		atr = referencePrice * 0.01
	}
	stopMult := 2.0
	tpMult := 3.0
	if cfg != nil {
		if cfg.StopLossATRMult > 0 {
			stopMult = cfg.StopLossATRMult
		}
		if cfg.TakeProfitATRMult > 0 {
			tpMult = cfg.TakeProfitATRMult
		}
	}
	switch action {
	case "open_short", "add_short":
		return referencePrice + atr*stopMult, math.Max(0, referencePrice-atr*tpMult)
	default:
		return math.Max(0, referencePrice-atr*stopMult), referencePrice + atr*tpMult
	}
}

func clipFloat64(value, minValue, maxValue float64) float64 {
	return math.Max(minValue, math.Min(maxValue, value))
}
