import type { MarketKline, SignalMarker } from '../types';

export interface BacktestHealth {
  enabled: boolean;
  dry_run: boolean;
  live_trading: boolean;
  scope: string;
}

export interface BacktestJob {
  run_id: string;
  type: string;
  status: string;
  progress?: BacktestProgress;
  started_at: string;
  ended_at?: string;
  error?: string;
}

export interface BacktestProgress {
  run_id: string;
  status: string;
  current_time?: string;
  backtest_from?: string;
  backtest_to?: string;
  cycles: number;
  executions: number;
  signals: number;
  rejections: number;
  error?: string;
  output_dir?: string;
  completed_percent?: number;
}

export interface BacktestCoverage {
  source: string;
  symbol: string;
  timeframe: string;
  count: number;
  from_ms: number;
  to_ms: number;
  data_hash?: string;
}

export interface BacktestSummary {
  initial_equity: number;
  final_equity: number;
  net_pnl: number;
  net_return_pct: number;
  max_drawdown_pct: number;
  win_rate: number;
  profit_factor: number;
  trade_count: number;
  execution_event_count: number;
  signal_count: number;
  rejection_count: number;
  total_fees: number;
  total_slippage: number;
}

export interface BacktestReport {
  run_id: string;
  generated_at: string;
  config_hash: string;
  timezone: string;
  warmup_from: string;
  backtest_from: string;
  backtest_to: string;
  market_data_source: string;
  execution_model: string;
  funding_mode: string;
  oi_mode: string;
  liquidation_mode: string;
  assumptions: string[];
  summary: BacktestSummary;
  files?: Record<string, string>;
}

export interface BacktestKlineResponse {
  symbol: string;
  timeframe: string;
  limit: number;
  klines: MarketKline[];
}

export type BacktestMarkerResponse = SignalMarker[];
