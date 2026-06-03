package backtest

import (
	"fmt"
	"math"
	"math/rand"
	"sort"
)

type PathEvaluator func(path []float64, initialValue float64) float64

type MonteCarloSimulator struct {
	Paths             int
	HorizonSteps      int
	Seed              int64
	StrategyEvaluator PathEvaluator
}

type MonteCarloResult struct {
	Paths           int       `json:"paths"`
	HorizonSteps    int       `json:"horizon_steps"`
	InitialPrice    float64   `json:"initial_price"`
	InitialValue    float64   `json:"initial_value"`
	Drift           float64   `json:"drift"`
	Volatility      float64   `json:"volatility"`
	TerminalValues  []float64 `json:"terminal_values,omitempty"`
	VaR95           float64   `json:"var_95"`
	VaR99           float64   `json:"var_99"`
	CVaR95          float64   `json:"cvar_95"`
	LossProbability float64   `json:"loss_probability"`
	MedianTerminal  float64   `json:"median_terminal"`
	MeanTerminal    float64   `json:"mean_terminal"`
	SurvivalRate    float64   `json:"survival_rate"`
}

func (s MonteCarloSimulator) Simulate(prices []float64, initialValue float64) (MonteCarloResult, error) {
	clean := positiveSeries(prices)
	if len(clean) < 2 {
		return MonteCarloResult{}, fmt.Errorf("蒙特卡洛模拟至少需要2个正价格样本")
	}
	if initialValue <= 0 {
		return MonteCarloResult{}, fmt.Errorf("initial_value必须大于0")
	}
	paths := s.Paths
	if paths <= 0 {
		paths = 2000
	}
	horizon := s.HorizonSteps
	if horizon <= 0 {
		horizon = len(clean)
	}
	drift, vol := estimateDriftVolatility(clean)
	rng := rand.New(rand.NewSource(s.Seed))
	evaluator := s.StrategyEvaluator
	if evaluator == nil {
		evaluator = terminalValueEvaluator
	}
	terminal := make([]float64, 0, paths)
	for i := 0; i < paths; i++ {
		path := simulateGBMPath(clean[len(clean)-1], drift, vol, horizon, rng)
		terminal = append(terminal, evaluator(path, initialValue))
	}
	sort.Float64s(terminal)
	result := MonteCarloResult{
		Paths:           paths,
		HorizonSteps:    horizon,
		InitialPrice:    clean[len(clean)-1],
		InitialValue:    initialValue,
		Drift:           drift,
		Volatility:      vol,
		TerminalValues:  terminal,
		VaR95:           valueAtRisk(terminal, initialValue, 0.05),
		VaR99:           valueAtRisk(terminal, initialValue, 0.01),
		CVaR95:          conditionalValueAtRisk(terminal, initialValue, 0.05),
		LossProbability: lossProbability(terminal, initialValue),
		MedianTerminal:  percentile(terminal, 0.50),
		MeanTerminal:    mean(terminal),
		SurvivalRate:    survivalRate(terminal),
	}
	return result, nil
}

func estimateDriftVolatility(prices []float64) (float64, float64) {
	returns := logReturns(prices)
	if len(returns) == 0 {
		return 0, 0
	}
	avg, std := meanStd(returns)
	return avg, std
}

func simulateGBMPath(startPrice, drift, volatility float64, steps int, rng *rand.Rand) []float64 {
	if steps <= 0 {
		steps = 1
	}
	path := make([]float64, steps+1)
	path[0] = startPrice
	for i := 1; i <= steps; i++ {
		z := rng.NormFloat64()
		path[i] = path[i-1] * math.Exp((drift-0.5*volatility*volatility)+volatility*z)
		if path[i] <= 0 || math.IsNaN(path[i]) || math.IsInf(path[i], 0) {
			path[i] = path[i-1]
		}
	}
	return path
}

func terminalValueEvaluator(path []float64, initialValue float64) float64 {
	if len(path) < 2 || path[0] <= 0 {
		return initialValue
	}
	return initialValue * path[len(path)-1] / path[0]
}

func positiveSeries(values []float64) []float64 {
	out := make([]float64, 0, len(values))
	for _, value := range values {
		if value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
			out = append(out, value)
		}
	}
	return out
}

func logReturns(prices []float64) []float64 {
	if len(prices) < 2 {
		return nil
	}
	out := make([]float64, 0, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		if prices[i-1] <= 0 || prices[i] <= 0 {
			continue
		}
		out = append(out, math.Log(prices[i]/prices[i-1]))
	}
	return out
}

func valueAtRisk(sortedTerminal []float64, initialValue float64, tail float64) float64 {
	if len(sortedTerminal) == 0 {
		return 0
	}
	p := percentile(sortedTerminal, tail)
	loss := initialValue - p
	if loss < 0 {
		return 0
	}
	return loss
}

func conditionalValueAtRisk(sortedTerminal []float64, initialValue float64, tail float64) float64 {
	if len(sortedTerminal) == 0 {
		return 0
	}
	count := int(math.Ceil(float64(len(sortedTerminal)) * tail))
	if count < 1 {
		count = 1
	}
	totalLoss := 0.0
	for i := 0; i < count && i < len(sortedTerminal); i++ {
		loss := initialValue - sortedTerminal[i]
		if loss > 0 {
			totalLoss += loss
		}
	}
	return totalLoss / float64(count)
}

func lossProbability(sortedTerminal []float64, initialValue float64) float64 {
	if len(sortedTerminal) == 0 {
		return 0
	}
	losses := 0
	for _, value := range sortedTerminal {
		if value < initialValue {
			losses++
		}
	}
	return float64(losses) / float64(len(sortedTerminal))
}

func survivalRate(sortedTerminal []float64) float64 {
	if len(sortedTerminal) == 0 {
		return 0
	}
	survived := 0
	for _, value := range sortedTerminal {
		if value > 0 {
			survived++
		}
	}
	return float64(survived) / float64(len(sortedTerminal))
}

func percentile(sortedValues []float64, p float64) float64 {
	if len(sortedValues) == 0 {
		return 0
	}
	if p <= 0 {
		return sortedValues[0]
	}
	if p >= 1 {
		return sortedValues[len(sortedValues)-1]
	}
	idx := int(math.Floor(float64(len(sortedValues)-1) * p))
	return sortedValues[idx]
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0.0
	for _, value := range values {
		sum += value
	}
	return sum / float64(len(values))
}
