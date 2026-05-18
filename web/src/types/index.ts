// 系统状态
export interface SystemStatus {
  is_running: boolean;
  start_time: string;
  runtime_minutes: number;
  call_count: number;
  initial_balance: number;
  scan_interval: string;
  stop_until: string;
  last_reset_time: string;
  ai_provider: string;
  decision_mode?: string;
}

// 账户信息
export interface AccountInfo {
  total_equity: number;
  available_balance: number;
  total_pnl: number;
  total_pnl_pct: number;
  total_unrealized_pnl: number;
  margin_used: number;
  margin_used_pct: number;
  position_count: number;
  initial_balance: number;
  daily_pnl: number;
}

// 持仓信息
export interface Position {
  symbol: string;
  side: string;
  entry_price: number;
  mark_price: number;
  stop_loss_price?: number;
  take_profit_price?: number;
  quantity: number;
  leverage: number;
  unrealized_pnl: number;
  unrealized_pnl_pct: number;
  liquidation_price: number;
  margin_used: number;
}

// 决策动作
export interface DecisionAction {
  action: string;
  symbol: string;
  quantity: number;
  leverage: number;
  price: number;
  order_id: number;
  timestamp: string;
  success: boolean;
  error: string;
  reasoning?: string;
  risk_usd?: number;
  gate_state?: string;
  gate_reasons?: string[];
  execution_risk?: string;
  stop_loss_set?: boolean;
  take_profit_set?: boolean;
  protection_error?: string;
  high_risk?: boolean;
  high_risk_reason?: string;
  remaining_position_usd?: number;
  strategy_mode?: string;
  strategy_name?: string;
  strategy_version?: string;
  config_hash?: string;
  signal_id?: string;
  signal_type?: string;
  signal_timeframe?: string;
  structure_target?: number;
  trade_intent?: string;
  signal_close_time?: number;
  decision_close_time?: number;
  strategy_metadata?: Record<string, unknown>;
  strategy_diagnostics?: Record<string, unknown>;
  requested_close_percentage?: number;
  executed_close_percentage?: number;
  final_action?: string;
  close_quantity?: number;
  explanation?: DecisionExplanation;
}

export interface DecisionExplanation {
  summary?: string;
  layer?: string;
  rule?: string;
  reason_code?: string;
  timeframe?: string;
  signal_type?: string;
  signal_id?: string;
  trigger_price?: number;
  reference_price?: number;
  threshold?: number;
  cooldown_status?: Record<string, unknown>;
  budget_status?: Record<string, unknown>;
  risk_checks?: Array<Record<string, unknown>>;
  details?: Record<string, unknown>;
}

export interface TradeOutcome {
  symbol: string;
  side: string;
  quantity: number;
  leverage: number;
  open_price: number;
  close_price: number;
  position_value: number;
  margin_used: number;
  pn_l: number;
  pn_l_pct: number;
  duration: string;
  open_time: string;
  close_time: string;
  was_stop_loss: boolean;
  event_type?: string;
  is_partial?: boolean;
  close_quantity?: number;
  remaining_quantity?: number;
  requested_close_percentage?: number;
  executed_close_percentage?: number;
  order_id?: number;
  signal_id?: string;
  strategy_name?: string;
  strategy_version?: string;
  commission?: number;
  pnl_source?: string;
  reconciled?: boolean;
  reconciliation_status?: string;
  reconciliation_reason?: string;
  open_reason?: string;
  close_reason?: string;
}

export interface TradeEventStats {
  total_events: number;
  full_close_events: number;
  auto_close_events: number;
  partial_close_events: number;
  partial_close_realized_pnl: number;
  partial_close_estimated_pnl: number;
  partial_close_reconciled: number;
  partial_close_pending: number;
}

export interface PerformanceAnalysis {
  total_trades: number;
  winning_trades: number;
  losing_trades: number;
  win_rate: number;
  avg_win: number;
  avg_loss: number;
  profit_factor: number;
  sharpe_ratio: number;
  recent_trades: TradeOutcome[];
  recent_trade_events: TradeOutcome[];
  trade_event_stats?: TradeEventStats;
  execution_quality?: ExecutionQuality;
  symbol_stats: Record<string, unknown>;
  best_symbol: string;
  worst_symbol: string;
}

export interface ExecutionRiskEvent {
  timestamp: string;
  symbol?: string;
  action?: string;
  risk_type: string;
  reason: string;
}

export interface ExecutionQuality {
  total_actions: number;
  open_attempts?: number;
  open_failures?: number;
  open_rejected_count?: number;
  partial_close_attempts: number;
  partial_close_failures: number;
  partial_close_failure_rate: number;
  protection_order_failures?: number;
  high_risk_execution_failures?: number;
  ai_failure_count: number;
  unmatched_action_count: number;
  recent_high_risk_errors?: ExecutionRiskEvent[];
  recent_open_rejection_reasons?: string[];
  protection_order_failure_rate?: number;
  high_risk_execution_failure_rate?: number;
}

// 决策记录
export interface DecisionRecord {
  timestamp: string;
  cycle_number: number;
  input_prompt: string;
  cot_trace: string;
  decision_json: string;
  account_state: {
    total_balance: number;
    available_balance: number;
    total_unrealized_profit: number;
    position_count: number;
    margin_used_pct: number;
  };
  positions: Array<{
    symbol: string;
    side: string;
    position_amt: number;
    entry_price: number;
    mark_price: number;
    unrealized_profit: number;
    leverage: number;
    liquidation_price: number;
  }>;
  candidate_coins: string[];
  decisions: DecisionAction[];
  execution_log: string[];
  success: boolean;
  error_message: string;
  risk_state?: {
    trader_id?: string;
    exchange?: string;
    max_risk_per_trade?: number;
    effective_max_risk_per_trade?: number;
    total_risk_budget?: number;
    remaining_risk_budget?: number;
    max_daily_loss_pct?: number;
    max_account_drawdown_pct?: number;
    ai_backoff_until?: string;
    consecutive_ai_fails?: number;
    open_gate_reasons?: string[];
  };
  decision_mode?: string;
  strategy_name?: string;
  strategy_version?: string;
  config_hash?: string;
  strategy_params?: Record<string, unknown>;
  strategy_diagnostics?: Record<string, unknown>;
}

// 统计信息
export interface Statistics {
  total_cycles: number;
  successful_cycles: number;
  failed_cycles: number;
  total_open_positions: number;
  total_close_positions: number;
}

export interface StrategySymbol {
  symbol: string;
  sources?: string[];
  selected?: boolean;
  has_position?: boolean;
}

export interface StrategySymbolsResponse {
  trader_id: string;
  symbols: StrategySymbol[];
}

export interface ChanlunSignal {
  signal_id: string;
  structure_key?: string;
  lifecycle_key?: string;
  symbol: string;
  direction: string;
  signal_type: string;
  action_hint: string;
  analysis_timeframe: string;
  trigger_timeframe: string;
  level: string;
  price: number;
  stop_loss: number;
  take_profit: number;
  structure_target: number;
  center_id?: string;
  confidence?: number;
  confirmed_at?: string;
  trigger_close_time?: number;
  signal_close_time?: number;
  decision_close_time?: number;
  segment_start_time?: number;
  segment_end_time?: number;
  status?: string;
  source_layer?: string;
  parent_signal_id?: string;
  parent_structure_key?: string;
  reason_code?: string;
  entry_trigger_id?: string;
  entry_trigger_type?: string;
  entry_trigger_timeframe?: string;
  entry_trigger_close_time?: number;
  entry_window_state?: string;
  entry_reference_price?: number;
  entry_invalidated?: boolean;
  entry_invalidation_reason?: string;
  remaining_net_rr?: number;
  freshness_state?: string;
  age_candles?: number;
  preview_phase?: string;
  preview_source_timeframe?: string;
  preview_closed_components?: number;
  preview_confirmed?: boolean;
  diagnostics?: {
    reasons?: string[];
    metrics?: Record<string, unknown>;
    state_source?: string;
    bootstrap?: boolean;
  };
}

export type SignalDisplayCategory =
  | 'structure_background'
  | 'preview_watch'
  | 'entry_trigger'
  | 'trade_action'
  | 'invalid_rejected'
  | 'position_management';

export interface SignalMarker {
  symbol: string;
  timeframe: string;
  close_time: number;
  signal_close_time?: number;
  decision_close_time?: number;
  display_close_time?: number;
  signal_type: string;
  direction: string;
  level: string;
  source_layer: string;
  status: string;
  signal_id: string;
  structure_key?: string;
  lifecycle_key?: string;
  parent_structure_key?: string;
  reason_code?: string;
  display_category?: SignalDisplayCategory;
  display_priority?: number;
  hidden_by_default?: boolean;
  collapsed?: boolean;
  collapsed_count?: number;
  first_seen_close_time?: number;
  last_seen_close_time?: number;
  last_updated_at?: number;
  action?: string;
  final_action?: string;
  trade_intent?: string;
  position_side?: string;
  price?: number;
  reason?: string;
  parent_signal_id?: string;
  entry_trigger_id?: string;
  entry_trigger_type?: string;
  entry_trigger_timeframe?: string;
  entry_trigger_close_time?: number;
  entry_window_state?: string;
  entry_reference_price?: number;
  entry_invalidated?: boolean;
  entry_invalidation_reason?: string;
  remaining_net_rr?: number;
  freshness_state?: string;
  age_candles?: number;
  preview_phase?: string;
  preview_source_timeframe?: string;
  preview_closed_components?: number;
  preview_confirmed?: boolean;
}

export interface SignalMarkerSummary {
  total_raw: number;
  total_returned: number;
  hidden_by_default: number;
  collapsed_lifecycle: number;
  suppressed_repeats: number;
  preview_hidden: number;
  by_category?: Record<string, number>;
  by_status?: Record<string, number>;
  max_latency_hours?: number;
  median_latency_hours?: number;
}

export interface SignalReportFilters {
  layers?: string[];
  statuses?: string[];
  from?: number;
  to?: number;
  limit?: number;
}

export type StrategySignalView = 'default' | 'audit';

export interface StrategySignalQuery extends SignalReportFilters {
  view?: StrategySignalView;
  include_history?: boolean;
}

export interface StrategySignalReport {
  trader_id: string;
  symbol: string;
  decision_mode: string;
  strategy_name?: string;
  strategy_version?: string;
  config_hash?: string;
  trade_timeframe?: string;
  component_timeframe?: string;
  micro_timeframe?: string;
  signals: ChanlunSignal[];
  signal_markers?: SignalMarker[];
  view?: StrategySignalView;
  marker_summary?: SignalMarkerSummary;
  filters?: SignalReportFilters;
  latest_diagnostics?: Record<string, unknown>;
}

export interface MarketKline {
  open_time: number;
  close_time: number;
  open: number;
  high: number;
  low: number;
  close: number;
  volume: number;
}

export interface MarketKlineResponse {
  symbol: string;
  timeframe: string;
  limit: number;
  configured_limit?: number;
  limit_source?: string;
  klines: MarketKline[];
}
