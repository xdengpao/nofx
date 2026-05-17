export interface SystemStatus {
  trader_id: string;
  trader_name: string;
  ai_model: string;
  decision_mode?: string;
  is_running: boolean;
  start_time: string;
  runtime_minutes: number;
  call_count: number;
  initial_balance: number;
  scan_interval: string;
  stop_until: string;
  last_reset_time: string;
  ai_provider: string;
}

export interface AccountInfo {
  total_equity: number;
  wallet_balance: number;
  unrealized_profit: number;
  available_balance: number;
  total_pnl: number;
  total_pnl_pct: number;
  total_unrealized_pnl: number;
  initial_balance: number;
  daily_pnl: number;
  position_count: number;
  margin_used: number;
  margin_used_pct: number;
}

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

export interface DecisionAction {
  action: string;
  symbol: string;
  quantity: number;
  leverage: number;
  price: number;
  order_id: number;
  timestamp: string;
  success: boolean;
  error?: string;
  reasoning?: string;
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

export interface AccountSnapshot {
  total_balance: number;
  available_balance: number;
  total_unrealized_profit: number;
  position_count: number;
  margin_used_pct: number;
}

export interface DecisionRecord {
  timestamp: string;
  cycle_number: number;
  input_prompt: string;
  cot_trace: string;
  decision_json: string;
  account_state: AccountSnapshot;
  positions: any[];
  candidate_coins: string[];
  decisions: DecisionAction[];
  execution_log: string[];
  success: boolean;
  error_message?: string;
  decision_mode?: string;
  strategy_name?: string;
  strategy_version?: string;
  config_hash?: string;
  strategy_params?: Record<string, unknown>;
  strategy_diagnostics?: Record<string, unknown>;
}

export interface Statistics {
  total_cycles: number;
  successful_cycles: number;
  failed_cycles: number;
  total_open_positions: number;
  total_close_positions: number;
}

// 新增：竞赛相关类型
export interface TraderInfo {
  trader_id: string;
  trader_name: string;
  ai_model: string;
  decision_mode?: string;
}

export interface CompetitionTraderData {
  trader_id: string;
  trader_name: string;
  ai_model: string;
  decision_mode?: string;
  total_equity: number;
  total_pnl: number;
  total_pnl_pct: number;
  position_count: number;
  margin_used_pct: number;
  call_count: number;
  is_running: boolean;
}

export interface CompetitionData {
  traders: CompetitionTraderData[];
  count: number;
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
  diagnostics?: {
    reasons?: string[];
    metrics?: Record<string, unknown>;
    state_source?: string;
    bootstrap?: boolean;
  };
}

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
  action?: string;
  final_action?: string;
  trade_intent?: string;
  position_side?: string;
  price?: number;
  reason?: string;
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
