export interface StorageDiagnostics {
  root: string;
  root_source?: string;
  host_mount_path?: string;
  container_root?: string;
  allow_external_paths: boolean;
  directories: StorageDirectory[];
  disk: {
    total_bytes: number;
    free_bytes: number;
    used_bytes: number;
    source?: string;
  };
  warnings?: string[];
}

export interface StorageDirectory {
  name: string;
  path: string;
  exists: boolean;
  writable: boolean;
  bytes: number;
  error?: string;
}

export interface DRLPPOHealth {
  enabled: boolean;
  python_available: boolean;
  python_bin?: string;
  python_path?: string;
  train_script?: string;
  train_script_available?: boolean;
  evaluate_script?: string;
  evaluate_script_available?: boolean;
  training_ready?: boolean;
  environment_errors?: string[];
  storage_root: string;
  onnx_runtime: string;
  message?: string;
}

export interface DRLPPOMarketCatalog {
  sources: DRLPPOMarketSource[];
  synced_at: string;
}

export interface DRLPPOMarketSource {
  source: string;
  display_name: string;
  exchange: string;
  timeframes: string[];
  symbols: DRLPPOMarketSymbol[];
  synced_at: string;
}

export interface DRLPPOMarketSymbol {
  symbol: string;
  base_asset?: string;
  quote_asset?: string;
  status?: string;
  contract_type?: string;
}

export interface DRLPPOCoverage {
  source: string;
  symbol: string;
  timeframe: string;
  count: number;
  from_ms: number;
  to_ms: number;
  data_hash?: string;
}

export interface DRLPPOGapIssue {
  id: string;
  source: string;
  symbol: string;
  timeframe: string;
  issue_type: string;
  start_time_ms: number;
  end_time_ms: number;
  detail: string;
}

export interface DRLPPOGapCheck {
  ok: boolean;
  detail?: string;
  gaps: DRLPPOGapIssue[];
}

export interface DRLPPOHistoryFetchJob {
  run_id: string;
  type: string;
  status: string;
  progress?: {
    run_id?: string;
    status?: string;
    executions?: number;
    rejections?: number;
    error?: string;
  };
  started_at?: string;
  ended_at?: string;
  error?: string;
  fetch_summary?: DRLPPOHistoryFetchSummary;
}

export interface DRLPPOHistoryFetchSummary {
  id: string;
  source: string;
  symbols: string[];
  timeframes: string[];
  data_from: string;
  data_to: string;
  status: string;
  request_count: number;
  rate_wait_count: number;
  retry_count: number;
  inserted_count: number;
  duplicate_count: number;
  failed?: Record<string, string>;
  started_at: string;
  finished_at?: string;
  last_fetched_close_ms?: number;
}

export interface DRLPPOJob {
  job_id: string;
  type: string;
  status: string;
  request: DRLPPOTrainRequest;
  started_at?: string;
  ended_at?: string;
  pid?: number;
  progress?: DRLPPOProgress;
  stdout_path?: string;
  stderr_path?: string;
  error?: string;
  artifacts?: DRLPPOArtifacts;
}

export interface DRLPPOProgress {
  timesteps?: number;
  total_timesteps: number;
  last_log_at?: string;
  runtime_seconds: number;
  stalled: boolean;
}

export interface DRLPPOArtifacts {
  model_id?: string;
  model_version?: string;
  zip_path?: string;
  onnx_path?: string;
  rolling_summary_path?: string;
  window_model_paths?: string[];
  metadata_path?: string;
  evaluation_path?: string;
}

export interface DRLPPOTrainRequest {
  source?: string;
  symbol: string;
  timeframe: string;
  start: string;
  end: string;
  total_timesteps: number;
  n_steps?: number;
  batch_size?: number;
  n_epochs?: number;
  observation_window: number;
  initial_balance: number;
  taker_fee: number;
  maker_fee: number;
  slippage: number;
  rolling: boolean;
  output_model_name: string;
  allow_incomplete_data?: boolean;
}

export interface DRLPPOLogChunk {
  job_id: string;
  stream: string;
  offset: number;
  next_offset: number;
  content: string;
  eof: boolean;
}

export interface DRLPPOModel {
  model_id: string;
  model_version?: string;
  symbol?: string;
  timeframe?: string;
  source?: string;
  data_from?: string;
  data_to?: string;
  total_timesteps?: number;
  observation_window?: number;
  created_at?: string;
  zip_path?: string;
  onnx_path?: string;
  evaluation_path?: string;
  deployable: boolean;
  runtime_note?: string;
}

export interface DRLPPOEvaluation {
  annual_return?: number;
  sharpe?: number;
  sortino?: number;
  max_drawdown?: number;
  win_rate?: number;
  directional_accuracy?: number;
  recommended_for_deploy?: boolean;
  [key: string]: unknown;
}

export interface DRLPPOStagingConfig {
  trader_patch: Record<string, unknown>;
  warnings?: string[];
}

export interface StorageMigrationRequest {
  sources?: string[];
  overwrite?: boolean;
  dry_run?: boolean;
}

export interface StorageMigrationReport {
  migration_id: string;
  status: string;
  root: string;
  copied_files: number;
  copied_bytes: number;
  skipped: number;
  conflicts?: Array<{ source: string; target: string; reason: string }>;
  errors?: Array<{ path: string; error: string }>;
  started_at: string;
  ended_at?: string;
  dry_run?: boolean;
}

export interface StorageMigrationJob {
  migration_id: string;
  status: string;
  started_at: string;
  ended_at?: string;
  error?: string;
  report?: StorageMigrationReport;
}
