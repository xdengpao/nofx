package chanlun

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
	"github.com/leanovate/gopter/prop"
)

func TestPropertyChanlunStructureDeterministic(t *testing.T) {
	parameters := gopter.DefaultTestParametersWithSeed(42)
	parameters.MinSuccessfulTests = 50
	properties := gopter.NewProperties(parameters)
	properties.Property("Property 2: Chanlun_Engine 确定性输出", prop.ForAll(
		func(values []float64, leftBars int, minBars int) bool {
			candles := propertyCandles(values)
			storeA := NewStateStore(filepath.Join(t.TempDir(), "a.json"))
			storeB := NewStateStore(filepath.Join(t.TempDir(), "b.json"))
			storeA.SetLastAnalyzedClosedKline("t1", "BTCUSDT", "1h", 123)
			storeB.SetLastAnalyzedClosedKline("t1", "BTCUSDT", "1h", 123)

			a := buildPropertyStructure(candles, leftBars, minBars, storeA)
			b := buildPropertyStructure(candles, leftBars, minBars, storeB)
			aj, err := json.Marshal(a)
			if err != nil {
				return false
			}
			bj, err := json.Marshal(b)
			if err != nil {
				return false
			}
			return string(aj) == string(bj)
		},
		gen.SliceOfN(40, gen.Float64Range(20, 200)),
		gen.IntRange(1, 2),
		gen.IntRange(1, 3),
	))
	properties.TestingRun(t)
}

type propertyStructureOutput struct {
	Normalized []Candle                `json:"normalized"`
	Fractals   []Fractal               `json:"fractals"`
	Strokes    []Stroke                `json:"strokes"`
	Segments   []Segment               `json:"segments"`
	Centers    []Center                `json:"centers"`
	State      ProgrammaticSymbolState `json:"state"`
	Signals    []ChanlunSignal         `json:"signals"`
}

func buildPropertyStructure(candles []Candle, leftBars, minBars int, store *StateStore) propertyStructureOutput {
	normalized := NormalizeInclusion(candles, DirectionUp)
	fractals := FindFractals(normalized, leftBars, leftBars)
	strokes := BuildStrokes(fractals, normalized, minBars, 0, 0)
	segments := BuildSegments(strokes, "enhanced")
	centers := BuildCenters(segments, "1h")
	signals := DetectSignals(SignalInput{
		TraderID:   "t1",
		Symbol:     "BTCUSDT",
		AnalysisTF: "1h",
		TriggerTF:  "15m",
		ConfigHash: "property",
		Now:        time.Unix(1_800_000_000, 0).UTC(),
		Centers:    centers,
		Segments:   segments,
		MACDHist:   make([]float64, len(candles)),
	})
	return propertyStructureOutput{
		Normalized: normalized,
		Fractals:   fractals,
		Strokes:    strokes,
		Segments:   segments,
		Centers:    centers,
		State:      store.SymbolState("t1", "BTCUSDT"),
		Signals:    signals,
	}
}

func propertyCandles(values []float64) []Candle {
	out := make([]Candle, len(values))
	for i, value := range values {
		if value <= 0 {
			value = 1
		}
		out[i] = Candle{
			Timeframe: "1h",
			OpenTime:  int64(i) * 3600_000,
			CloseTime: int64(i+1)*3600_000 - 1,
			Open:      value,
			High:      value * 1.01,
			Low:       value * 0.99,
			Close:     value,
			Volume:    1,
		}
	}
	return out
}
