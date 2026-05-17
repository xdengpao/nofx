package chanlun

import (
	"fmt"
	"math"
)

func NormalizeInclusion(candles []Candle, directionBias ...string) []Candle {
	if len(candles) == 0 {
		return nil
	}
	normalized := make([]Candle, 0, len(candles))
	bias := DirectionUp
	if len(directionBias) > 0 && directionBias[0] != "" {
		bias = directionBias[0]
	}
	for _, current := range candles {
		if len(normalized) == 0 {
			normalized = append(normalized, current)
			continue
		}
		last := normalized[len(normalized)-1]
		if !hasInclusion(last, current) {
			if current.Close >= last.Close {
				bias = DirectionUp
			} else {
				bias = DirectionDown
			}
			normalized = append(normalized, current)
			continue
		}
		merged := mergeIncluded(last, current, bias)
		normalized[len(normalized)-1] = merged
	}
	return normalized
}

func FindFractals(candles []Candle, leftBars, rightBars int) []Fractal {
	if leftBars <= 0 {
		leftBars = 2
	}
	if rightBars <= 0 {
		rightBars = 2
	}
	var fractals []Fractal
	for i := leftBars; i < len(candles)-rightBars; i++ {
		top := true
		bottom := true
		for j := i - leftBars; j <= i+rightBars; j++ {
			if j == i {
				continue
			}
			if candles[i].High <= candles[j].High {
				top = false
			}
			if candles[i].Low >= candles[j].Low {
				bottom = false
			}
		}
		if top {
			fractals = append(fractals, Fractal{
				Type: "top", Index: i, Price: candles[i].High,
				OpenTime: candles[i].OpenTime, CloseTime: candles[i].CloseTime,
			})
		}
		if bottom {
			fractals = append(fractals, Fractal{
				Type: "bottom", Index: i, Price: candles[i].Low,
				OpenTime: candles[i].OpenTime, CloseTime: candles[i].CloseTime,
			})
		}
	}
	return normalizeFractalAlternation(fractals)
}

func BuildStrokes(fractals []Fractal, candles []Candle, minBars int, minSwingPct, atrMultiplier float64) []Stroke {
	if minBars <= 0 {
		minBars = 5
	}
	var strokes []Stroke
	for i := 1; i < len(fractals); i++ {
		start := fractals[i-1]
		end := fractals[i]
		if start.Type == end.Type {
			continue
		}
		if int(math.Abs(float64(end.Index-start.Index))) < minBars {
			continue
		}
		direction := DirectionUp
		if start.Type == "top" && end.Type == "bottom" {
			direction = DirectionDown
		}
		base := math.Max(math.Abs(start.Price), 1)
		swingPct := math.Abs(end.Price-start.Price) / base
		atrRatio := 0.0
		if atrMultiplier > 0 {
			atr := averageRange(candles, start.Index, end.Index)
			if base > 0 {
				atrRatio = atr / base
			}
		}
		minMove := minSwingPct
		if atrMultiplier > 0 && atrRatio*atrMultiplier > minMove {
			minMove = atrRatio * atrMultiplier
		}
		if minMove > 0 && swingPct < minMove {
			continue
		}
		high := math.Max(start.Price, end.Price)
		low := math.Min(start.Price, end.Price)
		strokes = append(strokes, Stroke{
			ID:        fmt.Sprintf("stroke_%d_%d", start.Index, end.Index),
			Direction: direction,
			Start:     start,
			End:       end,
			High:      high,
			Low:       low,
			ATRRatio:  atrRatio,
		})
	}
	return strokes
}

func BuildSegments(strokes []Stroke, strictness string) []Segment {
	if len(strokes) == 0 {
		return nil
	}
	var segments []Segment
	for i, stroke := range strokes {
		if strictness == "confirm_both" && i > 0 && strokes[i-1].Direction == stroke.Direction {
			continue
		}
		segments = append(segments, Segment{
			ID:        fmt.Sprintf("segment_%d", i),
			Direction: stroke.Direction,
			StartTime: stroke.Start.CloseTime,
			EndTime:   stroke.End.CloseTime,
			Start:     stroke.Start.Price,
			End:       stroke.End.Price,
			High:      stroke.High,
			Low:       stroke.Low,
			Strokes:   []Stroke{stroke},
		})
	}
	return segments
}

func BuildCenters(segments []Segment, timeframe string) []Center {
	if len(segments) < 3 {
		return nil
	}
	var centers []Center
	for i := 0; i <= len(segments)-3; i++ {
		group := []Segment{segments[i], segments[i+1], segments[i+2]}
		zg := math.Min(group[0].High, math.Min(group[1].High, group[2].High))
		zd := math.Max(group[0].Low, math.Max(group[1].Low, group[2].Low))
		if zd > zg {
			continue
		}
		centers = append(centers, Center{
			ID:        fmt.Sprintf("%s_center_%d", timeframe, i),
			Timeframe: timeframe,
			ZG:        zg,
			ZD:        zd,
			High:      max3(group[0].High, group[1].High, group[2].High),
			Low:       min3(group[0].Low, group[1].Low, group[2].Low),
			Segments:  group,
		})
	}
	return centers
}

func ComponentTimeframe(target string) string {
	switch target {
	case "1h":
		return "15m"
	case "15m":
		return "3m"
	case "4h":
		return "1h"
	default:
		return ""
	}
}

func hasInclusion(a, b Candle) bool {
	return (a.High >= b.High && a.Low <= b.Low) || (b.High >= a.High && b.Low <= a.Low)
}

func mergeIncluded(a, b Candle, direction string) Candle {
	merged := b
	merged.OpenTime = a.OpenTime
	if direction == DirectionDown {
		merged.High = math.Min(a.High, b.High)
		merged.Low = math.Min(a.Low, b.Low)
	} else {
		merged.High = math.Max(a.High, b.High)
		merged.Low = math.Max(a.Low, b.Low)
	}
	merged.Open = a.Open
	merged.Close = b.Close
	merged.Volume = a.Volume + b.Volume
	return merged
}

func normalizeFractalAlternation(fractals []Fractal) []Fractal {
	if len(fractals) <= 1 {
		return fractals
	}
	result := []Fractal{fractals[0]}
	for _, f := range fractals[1:] {
		last := &result[len(result)-1]
		if f.Type != last.Type {
			result = append(result, f)
			continue
		}
		if f.Type == "top" && f.Price > last.Price {
			*last = f
		}
		if f.Type == "bottom" && f.Price < last.Price {
			*last = f
		}
	}
	return result
}

func averageRange(candles []Candle, start, end int) float64 {
	if len(candles) == 0 {
		return 0
	}
	if start > end {
		start, end = end, start
	}
	if start < 0 {
		start = 0
	}
	if end >= len(candles) {
		end = len(candles) - 1
	}
	sum := 0.0
	count := 0
	for i := start; i <= end; i++ {
		sum += math.Abs(candles[i].High - candles[i].Low)
		count++
	}
	if count == 0 {
		return 0
	}
	return sum / float64(count)
}

func max3(a, b, c float64) float64 {
	return math.Max(a, math.Max(b, c))
}

func min3(a, b, c float64) float64 {
	return math.Min(a, math.Min(b, c))
}
