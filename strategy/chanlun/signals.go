package chanlun

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
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
	signal := ChanlunSignal{
		SignalID:        StableSignalID(input.TraderID, input.Symbol, direction, signalType, input.AnalysisTF, input.TriggerTF, centerID, segment, input.ConfigHash),
		Symbol:          input.Symbol,
		Direction:       direction,
		SignalType:      signalType,
		ActionHint:      actionHint,
		AnalysisTF:      input.AnalysisTF,
		TriggerTF:       input.TriggerTF,
		Level:           input.AnalysisTF,
		Price:           price,
		StopLoss:        stop,
		TakeProfit:      target,
		StructureTarget: target,
		CenterID:        centerID,
		Confidence:      75,
		ConfirmedAt:     input.Now,
		Diagnostics:     diagnostics,
	}
	return signal
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
