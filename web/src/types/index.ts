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
}

// 统计信息
export interface Statistics {
  total_cycles: number;
  successful_cycles: number;
  failed_cycles: number;
  total_open_positions: number;
  total_close_positions: number;
}
