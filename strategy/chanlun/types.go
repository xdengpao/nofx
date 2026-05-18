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
	SignalID           string            `json:"signal_id"`
	StructureKey       string            `json:"structure_key,omitempty"`
	LifecycleKey       string            `json:"lifecycle_key,omitempty"`
	Symbol             string            `json:"symbol"`
	Direction          string            `json:"direction"`
	SignalType         string            `json:"signal_type"`
	ActionHint         string            `json:"action_hint"`
	AnalysisTF         string            `json:"analysis_timeframe"`
	TriggerTF          string            `json:"trigger_timeframe"`
	Level              string            `json:"level"`
	Price              float64           `json:"price"`
	StopLoss           float64           `json:"stop_loss"`
	TakeProfit         float64           `json:"take_profit"`
	StructureTarget    float64           `json:"structure_target"`
	CenterID           string            `json:"center_id,omitempty"`
	Confidence         int               `json:"confidence,omitempty"`
	ConfirmedAt        time.Time         `json:"confirmed_at,omitempty"`
	Diagnostics        SignalDiagnostics `json:"diagnostics,omitempty"`
	TriggerCloseTime   int64             `json:"trigger_close_time,omitempty"`
	SignalCloseTime    int64             `json:"signal_close_time,omitempty"`
	DecisionCloseTime  int64             `json:"decision_close_time,omitempty"`
	SegmentStartTime   int64             `json:"segment_start_time,omitempty"`
	SegmentEndTime     int64             `json:"segment_end_time,omitempty"`
	Status             string            `json:"status,omitempty"`
	SourceLayer        string            `json:"source_layer,omitempty"`
	ParentSignalID     string            `json:"parent_signal_id,omitempty"`
	ParentStructureKey string            `json:"parent_structure_key,omitempty"`
	ReasonCode         string            `json:"reason_code,omitempty"`
	EntryTriggerID     string            `json:"entry_trigger_id,omitempty"`
	EntryTriggerType   string            `json:"entry_trigger_type,omitempty"`
	EntryTriggerTF     string            `json:"entry_trigger_timeframe,omitempty"`
	EntryTriggerClose  int64             `json:"entry_trigger_close_time,omitempty"`
	EntryWindowState   string            `json:"entry_window_state,omitempty"`
	EntryReference     float64           `json:"entry_reference_price,omitempty"`
	EntryInvalidated   bool              `json:"entry_invalidated,omitempty"`
	EntryInvalidReason string            `json:"entry_invalidation_reason,omitempty"`
	RemainingNetRR     float64           `json:"remaining_net_rr,omitempty"`
	TriggerConfidence  int               `json:"trigger_confidence,omitempty"`
	PreviewPhase       string            `json:"preview_phase,omitempty"`
	PreviewSourceTF    string            `json:"preview_source_timeframe,omitempty"`
	PreviewComponents  int               `json:"preview_closed_components,omitempty"`
	PreviewConfirmed   bool              `json:"preview_confirmed,omitempty"`
}

type SignalReport struct {
	TraderID           string              `json:"trader_id"`
	Symbol             string              `json:"symbol"`
	DecisionMode       string              `json:"decision_mode"`
	StrategyName       string              `json:"strategy_name"`
	StrategyVersion    string              `json:"strategy_version"`
	ConfigHash         string              `json:"config_hash"`
	TradeTimeframe     string              `json:"trade_timeframe,omitempty"`
	ComponentTimeframe string              `json:"component_timeframe,omitempty"`
	MicroTimeframe     string              `json:"micro_timeframe,omitempty"`
	Signals            []ChanlunSignal     `json:"signals"`
	SignalMarkers      []SignalMarker      `json:"signal_markers,omitempty"`
	View               string              `json:"view,omitempty"`
	MarkerSummary      SignalMarkerSummary `json:"marker_summary,omitempty"`
	Filters            SignalReportFilters `json:"filters,omitempty"`
	LatestDiagnostics  map[string]any      `json:"latest_diagnostics,omitempty"`
}

type SignalMarkerSummary struct {
	TotalRaw           int            `json:"total_raw"`
	TotalReturned      int            `json:"total_returned"`
	HiddenByDefault    int            `json:"hidden_by_default"`
	CollapsedLifecycle int            `json:"collapsed_lifecycle"`
	SuppressedRepeats  int            `json:"suppressed_repeats"`
	PreviewHidden      int            `json:"preview_hidden"`
	ByCategory         map[string]int `json:"by_category,omitempty"`
	ByStatus           map[string]int `json:"by_status,omitempty"`
	MaxLatencyHours    float64        `json:"max_latency_hours,omitempty"`
	MedianLatencyHours float64        `json:"median_latency_hours,omitempty"`
}

type SignalReportFilters struct {
	Layers   []string `json:"layers,omitempty"`
	Statuses []string `json:"statuses,omitempty"`
	From     int64    `json:"from,omitempty"`
	To       int64    `json:"to,omitempty"`
	Limit    int      `json:"limit,omitempty"`
}

type SignalReportOptions struct {
	View     string
	Layers   []string
	Statuses []string
	From     int64
	To       int64
	Limit    int
}

type SignalMarker struct {
	Symbol             string  `json:"symbol"`
	Timeframe          string  `json:"timeframe"`
	CloseTime          int64   `json:"close_time"`
	SignalCloseTime    int64   `json:"signal_close_time,omitempty"`
	DecisionCloseTime  int64   `json:"decision_close_time,omitempty"`
	DisplayCloseTime   int64   `json:"display_close_time,omitempty"`
	SignalType         string  `json:"signal_type"`
	Direction          string  `json:"direction"`
	Level              string  `json:"level"`
	SourceLayer        string  `json:"source_layer"`
	Status             string  `json:"status"`
	SignalID           string  `json:"signal_id"`
	StructureKey       string  `json:"structure_key,omitempty"`
	LifecycleKey       string  `json:"lifecycle_key,omitempty"`
	ParentStructureKey string  `json:"parent_structure_key,omitempty"`
	ReasonCode         string  `json:"reason_code,omitempty"`
	DisplayCategory    string  `json:"display_category,omitempty"`
	DisplayPriority    int     `json:"display_priority,omitempty"`
	HiddenByDefault    bool    `json:"hidden_by_default,omitempty"`
	Collapsed          bool    `json:"collapsed,omitempty"`
	CollapsedCount     int     `json:"collapsed_count,omitempty"`
	FirstSeenCloseTime int64   `json:"first_seen_close_time,omitempty"`
	LastSeenCloseTime  int64   `json:"last_seen_close_time,omitempty"`
	LastUpdatedAt      int64   `json:"last_updated_at,omitempty"`
	Action             string  `json:"action,omitempty"`
	FinalAction        string  `json:"final_action,omitempty"`
	TradeIntent        string  `json:"trade_intent,omitempty"`
	PositionSide       string  `json:"position_side,omitempty"`
	Price              float64 `json:"price,omitempty"`
	Reason             string  `json:"reason,omitempty"`
	ParentSignalID     string  `json:"parent_signal_id,omitempty"`
	EntryTriggerID     string  `json:"entry_trigger_id,omitempty"`
	EntryTriggerType   string  `json:"entry_trigger_type,omitempty"`
	EntryTriggerTF     string  `json:"entry_trigger_timeframe,omitempty"`
	EntryTriggerClose  int64   `json:"entry_trigger_close_time,omitempty"`
	EntryWindowState   string  `json:"entry_window_state,omitempty"`
	EntryReference     float64 `json:"entry_reference_price,omitempty"`
	EntryInvalidated   bool    `json:"entry_invalidated,omitempty"`
	EntryInvalidReason string  `json:"entry_invalidation_reason,omitempty"`
	RemainingNetRR     float64 `json:"remaining_net_rr,omitempty"`
	FreshnessState     string  `json:"freshness_state,omitempty"`
	AgeCandles         int     `json:"age_candles,omitempty"`
	PreviewPhase       string  `json:"preview_phase,omitempty"`
	PreviewSourceTF    string  `json:"preview_source_timeframe,omitempty"`
	PreviewComponents  int     `json:"preview_closed_components,omitempty"`
	PreviewConfirmed   bool    `json:"preview_confirmed,omitempty"`
}

type StrategySymbol struct {
	Symbol      string   `json:"symbol"`
	Sources     []string `json:"sources"`
	Selected    bool     `json:"selected"`
	HasPosition bool     `json:"has_position"`
}
