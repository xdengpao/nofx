package chanlun

import "math"

type DivergenceResult struct {
	Diverged bool
	Kind     string
	AArea    float64
	CArea    float64
	Ratio    float64
	Reason   string
}

func MACDArea(hist []float64, start, end int, direction string) float64 {
	if len(hist) == 0 {
		return 0
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
	area := 0.0
	for i := start; i <= end; i++ {
		value := hist[i]
		if direction == DirectionUp && value > 0 {
			area += value
		}
		if direction == DirectionDown && value < 0 {
			area += math.Abs(value)
		}
	}
	return area
}

func DetectMACDDivergence(aSegment, cSegment Segment, hist []float64, ratioThreshold float64) DivergenceResult {
	if ratioThreshold <= 0 {
		ratioThreshold = 0.8
	}
	if aSegment.Direction != cSegment.Direction {
		return DivergenceResult{Reason: "A/C段方向不一致"}
	}
	aArea := MACDArea(hist, segmentStartIndex(aSegment), segmentEndIndex(aSegment), aSegment.Direction)
	cArea := MACDArea(hist, segmentStartIndex(cSegment), segmentEndIndex(cSegment), cSegment.Direction)
	if aArea <= 0 || cArea <= 0 {
		return DivergenceResult{AArea: aArea, CArea: cArea, Reason: "MACD面积不足"}
	}
	ratio := cArea / aArea
	kind := "top"
	if cSegment.Direction == DirectionDown {
		kind = "bottom"
	}
	return DivergenceResult{
		Diverged: ratio <= ratioThreshold,
		Kind:     kind,
		AArea:    aArea,
		CArea:    cArea,
		Ratio:    ratio,
	}
}

func segmentStartIndex(segment Segment) int {
	if len(segment.Strokes) == 0 {
		return 0
	}
	return segment.Strokes[0].Start.Index
}

func segmentEndIndex(segment Segment) int {
	if len(segment.Strokes) == 0 {
		return 0
	}
	return segment.Strokes[len(segment.Strokes)-1].End.Index
}

type KissResult struct {
	Position string
	KissType string
	Index    int
	Distance float64
}

func DetectMAKiss(shortEMA, longEMA []float64, kissDistancePct float64, wetBars int) KissResult {
	if len(shortEMA) == 0 || len(shortEMA) != len(longEMA) {
		return KissResult{}
	}
	if kissDistancePct <= 0 {
		kissDistancePct = 0.0015
	}
	if wetBars <= 0 {
		wetBars = 5
	}
	last := len(shortEMA) - 1
	position := "male"
	if shortEMA[last] > longEMA[last] {
		position = "female"
	}
	distance := math.Abs(shortEMA[last]-longEMA[last]) / math.Max(math.Abs(longEMA[last]), 1)
	if crossedWithin(shortEMA, longEMA, wetBars) {
		return KissResult{Position: position, KissType: "wet", Index: last, Distance: distance}
	}
	if distance <= kissDistancePct {
		return KissResult{Position: position, KissType: "lip", Index: last, Distance: distance}
	}
	if len(shortEMA) >= 3 {
		prevDistance := math.Abs(shortEMA[last-1]-longEMA[last-1]) / math.Max(math.Abs(longEMA[last-1]), 1)
		prevPrevDistance := math.Abs(shortEMA[last-2]-longEMA[last-2]) / math.Max(math.Abs(longEMA[last-2]), 1)
		if prevDistance < prevPrevDistance && distance > prevDistance && prevDistance > kissDistancePct {
			return KissResult{Position: position, KissType: "fly", Index: last - 1, Distance: prevDistance}
		}
	}
	return KissResult{Position: position, Index: last, Distance: distance}
}

func crossedWithin(shortEMA, longEMA []float64, bars int) bool {
	if len(shortEMA) < 2 {
		return false
	}
	start := len(shortEMA) - bars
	if start < 1 {
		start = 1
	}
	for i := start; i < len(shortEMA); i++ {
		prev := shortEMA[i-1] - longEMA[i-1]
		curr := shortEMA[i] - longEMA[i]
		if prev == 0 || curr == 0 || prev*curr < 0 {
			return true
		}
	}
	return false
}
