package chanlunv2

// AnalysisInput 传给 Rust 的输入
type AnalysisInput struct {
	Klines   []Kline        `json:"klines"`
	MACDHist []float64      `json:"macd_hist"`
	Config   AnalysisConfig `json:"config"`
}

type Kline struct {
	OpenTime  int64   `json:"open_time"`
	CloseTime int64   `json:"close_time"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume"`
}

type AnalysisConfig struct {
	MinStrokeBars         int     `json:"min_stroke_bars"`
	DivergenceThreshold   float64 `json:"divergence_threshold"`
	EnableExtendedSignals bool    `json:"enable_extended_signals"`
	EnableRecursive       bool    `json:"enable_recursive"`
	RecursiveDepth        int     `json:"recursive_depth"`
}

// AnalysisOutput Rust 返回的结果
type AnalysisOutput struct {
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Result  *AnalysisResult `json:"result"`
}

type AnalysisResult struct {
	MergedKlines []MergedKline `json:"merged_klines"`
	Fractals     []Fractal     `json:"fractals"`
	Strokes      []Stroke      `json:"strokes"`
	Segments     []Segment     `json:"segments"`
	Centers      []Center      `json:"centers"`
	Trend        string        `json:"trend"`
	Divergences  []Divergence  `json:"divergences"`
	Signals      []Signal      `json:"signals"`
}

type MergedKline struct {
	Index       int     `json:"index"`
	OpenTime    int64   `json:"open_time"`
	CloseTime   int64   `json:"close_time"`
	High        float64 `json:"high"`
	Low         float64 `json:"low"`
	Direction   string  `json:"direction"`
	MergedCount int     `json:"merged_count"`
}

type Fractal struct {
	FxType    string  `json:"fx_type"`
	Index     int     `json:"index"`
	Price     float64 `json:"price"`
	OpenTime  int64   `json:"open_time"`
	CloseTime int64   `json:"close_time"`
}

type Stroke struct {
	ID        int     `json:"id"`
	Direction string  `json:"direction"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
}

type Segment struct {
	ID        int     `json:"id"`
	Direction string  `json:"direction"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	StartTime int64   `json:"start_time"`
	EndTime   int64   `json:"end_time"`
}

type Center struct {
	ID        int     `json:"id"`
	ZG        float64 `json:"zg"`
	ZD        float64 `json:"zd"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	StartTime int64   `json:"start_time"`
	EndTime   int64   `json:"end_time"`
}

type Divergence struct {
	DivType    string  `json:"div_type"`
	SegAID     int     `json:"seg_a_id"`
	SegCID     int     `json:"seg_c_id"`
	MACDRatio  float64 `json:"macd_ratio"`
	SlopeRatio float64 `json:"slope_ratio"`
	Strength   float64 `json:"strength"`
}

type Signal struct {
	SignalType          string  `json:"signal_type"`
	Direction          string  `json:"direction"`
	Price              float64 `json:"price"`
	StopLoss           float64 `json:"stop_loss"`
	TakeProfit         float64 `json:"take_profit"`
	Confidence         int     `json:"confidence"`
	CenterID           *int    `json:"center_id"`
	DivergenceStrength float64 `json:"divergence_strength"`
	Timestamp          int64   `json:"timestamp"`
}
