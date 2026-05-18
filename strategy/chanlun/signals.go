package chanlun

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"math"
	"nofx/market"
	"time"
)

type SignalInput struct {
	TraderID          string
	Symbol            string
	AnalysisTF        string
	TriggerTF         string
	Centers           []Center
	Segments          []Segment
	MACDHist          []float64
	ConfigHash        string
	Now               time.Time
	EnabledSignal     map[string]bool
	DivergenceRatio   float64
	PriceTolerancePct float64
	RequireBZeroAxis  bool
	MAKiss            KissResult
	MarketData        *market.Data
	ADXTimeframe      string
}

func DetectSignals(input SignalInput) []ChanlunSignal {
	if input.Now.IsZero() {
		input.Now = time.Now()
	}
	if len(input.Segments) < 3 {
		return nil
	}
	enabled := input.EnabledSignal
	if len(enabled) == 0 {
		enabled = map[string]bool{
			SignalBuy1: true, SignalBuy2: true, SignalBuy3: true,
			SignalSell1: true, SignalSell2: true, SignalSell3: true,
		}
	}

	var signals []ChanlunSignal
	a := input.Segments[len(input.Segments)-3]
	b := input.Segments[len(input.Segments)-2]
	c := input.Segments[len(input.Segments)-1]
	div := DetectMACDDivergence(a, c, input.MACDHist, input.DivergenceRatio)
	divergenceUsable := div.Diverged &&
		priceInnovated(a, c, input.PriceTolerancePct) &&
		(!input.RequireBZeroAxis || segmentTouchesZeroAxis(b, input.MACDHist))
	if divergenceUsable && c.Direction == DirectionDown && enabled[SignalBuy1] {
		signals = append(signals, buildSignal(input, SignalBuy1, SideLong, c, lastCenterID(input.Centers), div))
	}
	if divergenceUsable && c.Direction == DirectionUp && enabled[SignalSell1] {
		signals = append(signals, buildSignal(input, SignalSell1, SideShort, c, lastCenterID(input.Centers), div))
	}
	if !divergenceUsable && c.Direction == DirectionDown && c.Low > a.Low && enabled[SignalBuy2] {
		signals = append(signals, buildSignal(input, SignalBuy2, SideLong, c, lastCenterID(input.Centers), div))
	}
	if !divergenceUsable && c.Direction == DirectionUp && c.High < a.High && enabled[SignalSell2] {
		signals = append(signals, buildSignal(input, SignalSell2, SideShort, c, lastCenterID(input.Centers), div))
	}

	if len(input.Centers) > 0 {
		center := input.Centers[len(input.Centers)-1]
		last := input.Segments[len(input.Segments)-1]
		if last.Direction == DirectionUp && last.Low >= center.ZG && enabled[SignalBuy3] {
			signals = append(signals, buildSignal(input, SignalBuy3, SideLong, last, center.ID, DivergenceResult{}))
		}
		if last.Direction == DirectionDown && last.High <= center.ZD && enabled[SignalSell3] {
			signals = append(signals, buildSignal(input, SignalSell3, SideShort, last, center.ID, DivergenceResult{}))
		}
	}

	return signals
}

func IsThirdBuyFailed(center Center, close15m float64, closes3m []float64) bool {
	if center.ZG <= 0 {
		return false
	}
	if close15m > 0 && close15m < center.ZG {
		return true
	}
	return lastTwoBreak(closes3m, func(v float64) bool { return v < center.ZG })
}

func IsSecondBuyConfirmed(firstBuy ChanlunSignal, pullbackLow float64, macdCrossedAboveZero bool) bool {
	return firstBuy.SignalType == SignalBuy1 &&
		firstBuy.Direction == SideLong &&
		macdCrossedAboveZero &&
		pullbackLow > 0 &&
		pullbackLow >= firstBuy.StopLoss
}

func IsSecondSellConfirmed(firstSell ChanlunSignal, reboundHigh float64, macdCrossedBelowZero bool) bool {
	return firstSell.SignalType == SignalSell1 &&
		firstSell.Direction == SideShort &&
		macdCrossedBelowZero &&
		reboundHigh > 0 &&
		reboundHigh <= firstSell.StopLoss
}

func IsThirdSellFailed(center Center, close15m float64, closes3m []float64) bool {
	if center.ZD <= 0 {
		return false
	}
	if close15m > center.ZD {
		return true
	}
	return lastTwoBreak(closes3m, func(v float64) bool { return v > center.ZD })
}

func StableSignalID(traderID, symbol, direction, signalType, analysisTF, triggerTF, centerID string, segment Segment, configHash string) string {
	value := fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%d|%d|%s",
		traderID, symbol, direction, signalType, analysisTF, triggerTF, centerID,
		segment.StartTime, segment.EndTime, configHash)
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func StableEntryTriggerID(traderID, symbol, parentSignalID, triggerType, triggerTF string, triggerCloseTime int64, configHash string) string {
	value := fmt.Sprintf("%s|%s|%s|%s|%s|%d|%s",
		traderID, symbol, parentSignalID, triggerType, triggerTF, triggerCloseTime, configHash)
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func buildSignal(input SignalInput, signalType, direction string, segment Segment, centerID string, div DivergenceResult) ChanlunSignal {
	price := segment.End
	stop := segment.Low
	target := segment.High
	actionHint := "open"
	if signalType == SignalSell1 || signalType == SignalSell2 || signalType == SignalSell3 {
		stop = segment.High
		target = segment.Low
	}
	diagnostics := SignalDiagnostics{
		Metrics: map[string]any{
			"a_area":      div.AArea,
			"c_area":      div.CArea,
			"ratio":       div.Ratio,
			"ma_position": input.MAKiss.Position,
			"ma_kiss":     input.MAKiss.KissType,
		},
	}
	confidence, confidenceMetrics := calculateSignalConfidence(input, signalType, direction, div)
	for key, value := range confidenceMetrics {
		diagnostics.Metrics[key] = value
	}
	signal := ChanlunSignal{
		SignalID:         StableSignalID(input.TraderID, input.Symbol, direction, signalType, input.AnalysisTF, input.TriggerTF, centerID, segment, input.ConfigHash),
		Symbol:           input.Symbol,
		Direction:        direction,
		SignalType:       signalType,
		ActionHint:       actionHint,
		AnalysisTF:       input.AnalysisTF,
		TriggerTF:        input.TriggerTF,
		Level:            input.AnalysisTF,
		Price:            price,
		StopLoss:         stop,
		TakeProfit:       target,
		StructureTarget:  target,
		CenterID:         centerID,
		Confidence:       confidence,
		ConfirmedAt:      input.Now,
		Diagnostics:      diagnostics,
		TriggerCloseTime: segment.EndTime,
		SignalCloseTime:  segment.EndTime,
		SegmentStartTime: segment.StartTime,
		SegmentEndTime:   segment.EndTime,
		Status:           "detected",
		SourceLayer:      "structure",
	}
	return signal
}

func calculateSignalConfidence(input SignalInput, signalType, direction string, div DivergenceResult) (int, map[string]any) {
	score := baseSignalConfidence(signalType)
	metrics := map[string]any{
		"confidence_base":        score,
		"confidence_signal_type": signalType,
	}
	adjust := func(key string, delta int) {
		score += delta
		metrics[key] = delta
	}

	if div.Diverged {
		adjust("confidence_divergence", 3)
	} else if signalType == SignalBuy1 || signalType == SignalSell1 {
		adjust("confidence_missing_divergence", -2)
	}
	if input.MAKiss.KissType != "" {
		adjust("confidence_ma_kiss", 1)
	}
	switch {
	case direction == SideLong && input.MAKiss.Position == "female":
		adjust("confidence_ma_position", 2)
	case direction == SideShort && input.MAKiss.Position == "male":
		adjust("confidence_ma_position", 2)
	case input.MAKiss.Position != "":
		adjust("confidence_ma_position", -1)
	}

	data := input.MarketData
	if data == nil {
		adjust("confidence_missing_market_data", -4)
		return clampConfidence(score), metrics
	}

	state, stateConfidence := market.GetMarketState(data)
	metrics["confidence_market_state"] = state
	metrics["confidence_market_state_score"] = stateConfidence
	applyMarketStateConfidence(direction, state, adjust)

	timeframe := input.ADXTimeframe
	if timeframe == "" {
		timeframe = "1h"
	}
	snapshot := market.GetDirectionalSnapshot(data, timeframe)
	metrics["confidence_adx_timeframe"] = snapshot.Timeframe
	metrics["confidence_adx"] = snapshot.ADX
	metrics["confidence_di_plus"] = snapshot.DIPlus
	metrics["confidence_di_minus"] = snapshot.DIMinus
	if snapshot.ADX <= 0 || snapshot.DIPlus <= 0 || snapshot.DIMinus <= 0 {
		adjust("confidence_missing_adx_di", -4)
	} else {
		aligned := signalDirectionAligned(direction, snapshot.DIPlus, snapshot.DIMinus)
		metrics["confidence_di_aligned"] = aligned
		if aligned {
			adjust("confidence_adx_strength", adxConfidenceDelta(snapshot.ADX))
			adjust("confidence_di_spread", diSpreadConfidenceDelta(snapshot.DIPlus, snapshot.DIMinus))
		} else {
			adjust("confidence_di_counter_direction", -10)
		}
	}

	applyMomentumConfidence(direction, data.PriceChange1h, data.PriceChange4h, metrics, adjust)
	return clampConfidence(score), metrics
}

func baseSignalConfidence(signalType string) int {
	switch signalType {
	case SignalBuy3, SignalSell3:
		return 80
	case SignalBuy2, SignalSell2:
		return 78
	case SignalBuy1, SignalSell1:
		return 76
	default:
		return 74
	}
}

func applyMarketStateConfidence(direction, state string, adjust func(string, int)) {
	switch state {
	case "STRONG_UPTREND":
		if direction == SideLong {
			adjust("confidence_market_alignment", 5)
		} else {
			adjust("confidence_market_counter", -8)
		}
	case "WEAK_UPTREND":
		if direction == SideLong {
			adjust("confidence_market_alignment", 2)
		} else {
			adjust("confidence_market_counter", -5)
		}
	case "STRONG_DOWNTREND":
		if direction == SideShort {
			adjust("confidence_market_alignment", 5)
		} else {
			adjust("confidence_market_counter", -8)
		}
	case "WEAK_DOWNTREND":
		if direction == SideShort {
			adjust("confidence_market_alignment", 2)
		} else {
			adjust("confidence_market_counter", -5)
		}
	case "SQUEEZE":
		adjust("confidence_market_range", -2)
	case "RANGING":
		adjust("confidence_market_range", -3)
	}
}

func signalDirectionAligned(direction string, diPlus, diMinus float64) bool {
	switch direction {
	case SideLong:
		return diPlus > diMinus
	case SideShort:
		return diMinus > diPlus
	default:
		return true
	}
}

func adxConfidenceDelta(adx float64) int {
	switch {
	case adx >= 40:
		return 8
	case adx >= 30:
		return 6
	case adx >= 25:
		return 5
	case adx >= 20:
		return 2
	default:
		return -6
	}
}

func diSpreadConfidenceDelta(diPlus, diMinus float64) int {
	sum := math.Abs(diPlus) + math.Abs(diMinus)
	if sum <= 0 {
		return -2
	}
	spreadPct := math.Abs(diPlus-diMinus) / sum * 100
	switch {
	case spreadPct >= 30:
		return 4
	case spreadPct >= 15:
		return 3
	case spreadPct >= 5:
		return 1
	default:
		return -1
	}
}

func applyMomentumConfidence(direction string, priceChange1h, priceChange4h float64, metrics map[string]any, adjust func(string, int)) {
	metrics["confidence_price_change_1h"] = priceChange1h
	metrics["confidence_price_change_4h"] = priceChange4h
	switch direction {
	case SideLong:
		switch {
		case priceChange1h > 0 && priceChange4h >= 0:
			adjust("confidence_price_momentum", 2)
		case priceChange1h < 0 && priceChange4h < 0:
			adjust("confidence_price_momentum", -3)
		case priceChange1h < 0:
			adjust("confidence_price_momentum", -1)
		}
	case SideShort:
		switch {
		case priceChange1h < 0 && priceChange4h <= 0:
			adjust("confidence_price_momentum", 2)
		case priceChange1h > 0 && priceChange4h > 0:
			adjust("confidence_price_momentum", -3)
		case priceChange1h > 0:
			adjust("confidence_price_momentum", -1)
		}
	}
}

func clampConfidence(score int) int {
	if score < 60 {
		return 60
	}
	if score > 95 {
		return 95
	}
	return score
}

func lastCenterID(centers []Center) string {
	if len(centers) == 0 {
		return ""
	}
	return centers[len(centers)-1].ID
}

func lastTwoBreak(values []float64, pred func(float64) bool) bool {
	if len(values) < 2 {
		return false
	}
	return pred(values[len(values)-1]) && pred(values[len(values)-2])
}

func priceInnovated(a, c Segment, tolerancePct float64) bool {
	if tolerancePct < 0 {
		tolerancePct = 0
	}
	switch c.Direction {
	case DirectionUp:
		return c.High >= a.High*(1-tolerancePct)
	case DirectionDown:
		return c.Low <= a.Low*(1+tolerancePct)
	default:
		return false
	}
}

func segmentTouchesZeroAxis(segment Segment, hist []float64) bool {
	start := segmentStartIndex(segment)
	end := segmentEndIndex(segment)
	if len(hist) == 0 {
		return false
	}
	if start > end {
		start, end = end, start
	}
	if start < 0 {
		start = 0
	}
	if end >= len(hist) {
		end = len(hist) - 1
	}
	seenPositive := false
	seenNegative := false
	for i := start; i <= end; i++ {
		if hist[i] >= 0 {
			seenPositive = true
		}
		if hist[i] <= 0 {
			seenNegative = true
		}
	}
	return seenPositive && seenNegative
}
