package chanlun

import "time"

const (
	DirectionUp   = "up"
	DirectionDown = "down"
	SideLong      = "long"
	SideShort     = "short"

	SignalBuy1  = "buy1"
	SignalBuy2  = "buy2"
	SignalBuy3  = "buy3"
	SignalSell1 = "sell1"
	SignalSell2 = "sell2"
	SignalSell3 = "sell3"
)

type Candle struct {
	Timeframe string  `json:"timeframe,omitempty"`
	OpenTime  int64   `json:"open_time"`
	CloseTime int64   `json:"close_time"`
	Open      float64 `json:"open"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	Close     float64 `json:"close"`
	Volume    float64 `json:"volume,omitempty"`
}

type Fractal struct {
	Type      string  `json:"type"` // top, bottom
	Index     int     `json:"index"`
	Price     float64 `json:"price"`
	OpenTime  int64   `json:"open_time"`
	CloseTime int64   `json:"close_time"`
}

type Stroke struct {
	ID        string  `json:"id"`
	Direction string  `json:"direction"`
	Start     Fractal `json:"start"`
	End       Fractal `json:"end"`
	High      float64 `json:"high"`
	Low       float64 `json:"low"`
	ATRRatio  float64 `json:"atr_ratio,omitempty"`
}

type Segment struct {
	ID        string   `json:"id"`
	Direction string   `json:"direction"`
	StartTime int64    `json:"start_time"`
	EndTime   int64    `json:"end_time"`
	Start     float64  `json:"start"`
	End       float64  `json:"end"`
	High      float64  `json:"high"`
	Low       float64  `json:"low"`
	Strokes   []Stroke `json:"strokes,omitempty"`
}

type Center struct {
	ID        string    `json:"id"`
	Timeframe string    `json:"timeframe"`
	ZG        float64   `json:"zg"`
	ZD        float64   `json:"zd"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Segments  []Segment `json:"segments"`
}

type SignalDiagnostics struct {
	Reasons     []string       `json:"reasons,omitempty"`
	Metrics     map[string]any `json:"metrics,omitempty"`
	StateSource string         `json:"state_source,omitempty"`
	Bootstrap   bool           `json:"bootstrap,omitempty"`
}

type ChanlunSignal struct {
	SignalID         string            `json:"signal_id"`
	Symbol           string            `json:"symbol"`
	Direction        string            `json:"direction"`
	SignalType       string            `json:"signal_type"`
	ActionHint       string            `json:"action_hint"`
	AnalysisTF       string            `json:"analysis_timeframe"`
	TriggerTF        string            `json:"trigger_timeframe"`
	Level            string            `json:"level"`
	Price            float64           `json:"price"`
	StopLoss         float64           `json:"stop_loss"`
	TakeProfit       float64           `json:"take_profit"`
	StructureTarget  float64           `json:"structure_target"`
	CenterID         string            `json:"center_id,omitempty"`
	Confidence       int               `json:"confidence,omitempty"`
	ConfirmedAt      time.Time         `json:"confirmed_at,omitempty"`
	Diagnostics      SignalDiagnostics `json:"diagnostics,omitempty"`
	TriggerCloseTime int64             `json:"trigger_close_time,omitempty"`
	SegmentStartTime int64             `json:"segment_start_time,omitempty"`
	SegmentEndTime   int64             `json:"segment_end_time,omitempty"`
	Status           string            `json:"status,omitempty"`
	SourceLayer      string            `json:"source_layer,omitempty"`
}

type SignalReport struct {
	TraderID           string          `json:"trader_id"`
	Symbol             string          `json:"symbol"`
	DecisionMode       string          `json:"decision_mode"`
	StrategyName       string          `json:"strategy_name"`
	StrategyVersion    string          `json:"strategy_version"`
	ConfigHash         string          `json:"config_hash"`
	TradeTimeframe     string          `json:"trade_timeframe,omitempty"`
	ComponentTimeframe string          `json:"component_timeframe,omitempty"`
	MicroTimeframe     string          `json:"micro_timeframe,omitempty"`
	Signals            []ChanlunSignal `json:"signals"`
	SignalMarkers      []SignalMarker  `json:"signal_markers,omitempty"`
	LatestDiagnostics  map[string]any  `json:"latest_diagnostics,omitempty"`
}

type SignalMarker struct {
	Symbol      string  `json:"symbol"`
	Timeframe   string  `json:"timeframe"`
	CloseTime   int64   `json:"close_time"`
	SignalType  string  `json:"signal_type"`
	Direction   string  `json:"direction"`
	Level       string  `json:"level"`
	SourceLayer string  `json:"source_layer"`
	Status      string  `json:"status"`
	SignalID    string  `json:"signal_id"`
	Action      string  `json:"action,omitempty"`
	Price       float64 `json:"price,omitempty"`
	Reason      string  `json:"reason,omitempty"`
}

type StrategySymbol struct {
	Symbol      string   `json:"symbol"`
	Sources     []string `json:"sources"`
	Selected    bool     `json:"selected"`
	HasPosition bool     `json:"has_position"`
}
