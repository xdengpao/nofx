import type {
  DRLPPOCoverage,
  DRLPPOEvaluation,
  DRLPPOGapCheck,
  DRLPPOHealth,
  DRLPPOHistoryFetchJob,
  DRLPPOJob,
  DRLPPOLogChunk,
  DRLPPOMarketCatalog,
  DRLPPOModel,
  DRLPPOStagingConfig,
  DRLPPOTrainRequest,
  StorageDiagnostics,
  StorageMigrationJob,
  StorageMigrationRequest,
} from '../types/drlPpoTraining';

const TRAIN_API_BASE = '/api/drl-ppo';
const STORAGE_API_BASE = '/api/storage';

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error || `请求失败: ${res.status}`);
  }
  return res.json();
}

function jsonRequest<T>(url: string, payload: unknown): Promise<T> {
  return request<T>(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
}

export const drlPpoTrainApi = {
  health(): Promise<DRLPPOHealth> {
    return request(`${TRAIN_API_BASE}/health`);
  },

  storage(): Promise<StorageDiagnostics> {
    return request(`${TRAIN_API_BASE}/storage`);
  },

  storageDiagnostics(): Promise<StorageDiagnostics> {
    return request(`${STORAGE_API_BASE}/diagnostics`);
  },

  markets(refresh = false): Promise<DRLPPOMarketCatalog> {
    const params = refresh ? '?refresh=true' : '';
    return request(`${TRAIN_API_BASE}/markets${params}`);
  },

  startStorageMigration(payload: StorageMigrationRequest): Promise<StorageMigrationJob> {
    return jsonRequest(`${STORAGE_API_BASE}/migrations`, payload);
  },

  storageMigration(migrationId: string): Promise<StorageMigrationJob> {
    return request(`${STORAGE_API_BASE}/migrations/${encodeURIComponent(migrationId)}`);
  },

  coverage(source = 'binance-futures'): Promise<DRLPPOCoverage[]> {
    const params = new URLSearchParams({ source });
    return request(`${TRAIN_API_BASE}/history/coverage?${params.toString()}`);
  },

  gaps(payload: {
    source?: string;
    symbol: string;
    timeframe: string;
    from: string;
    to: string;
    timezone?: string;
  }): Promise<DRLPPOGapCheck> {
    return jsonRequest(`${TRAIN_API_BASE}/history/gaps`, payload);
  },

  fetchHistory(payload: {
    source?: string;
    symbols: string[];
    timeframes: string[];
    data_from: string;
    data_to: string;
    timezone?: string;
    rate_limit?: {
      requests_per_minute?: number;
      concurrency?: number;
      page_limit?: number;
      max_retries?: number;
    };
  }): Promise<DRLPPOHistoryFetchJob> {
    return jsonRequest(`${TRAIN_API_BASE}/history/fetch`, payload);
  },

  historyFetchJob(runId: string): Promise<DRLPPOHistoryFetchJob> {
    return request(`${TRAIN_API_BASE}/history/fetch/${encodeURIComponent(runId)}`);
  },

  jobs(): Promise<DRLPPOJob[]> {
    return request(`${TRAIN_API_BASE}/jobs`);
  },

  createJob(payload: DRLPPOTrainRequest): Promise<DRLPPOJob> {
    return jsonRequest(`${TRAIN_API_BASE}/jobs`, payload);
  },

  job(jobId: string): Promise<DRLPPOJob> {
    return request(`${TRAIN_API_BASE}/jobs/${encodeURIComponent(jobId)}`);
  },

  cancelJob(jobId: string): Promise<{ status: string }> {
    return jsonRequest(`${TRAIN_API_BASE}/jobs/${encodeURIComponent(jobId)}/cancel`, {});
  },

  logs(jobId: string, stream: 'stdout' | 'stderr', offset = 0, tailBytes = 0): Promise<DRLPPOLogChunk> {
    const params = new URLSearchParams({ stream, offset: String(offset) });
    if (tailBytes > 0) params.set('tail_bytes', String(tailBytes));
    return request(`${TRAIN_API_BASE}/jobs/${encodeURIComponent(jobId)}/logs?${params.toString()}`);
  },

  models(): Promise<DRLPPOModel[]> {
    return request(`${TRAIN_API_BASE}/models`);
  },

  evaluateModel(modelId: string, payload: {
    source?: string;
    symbol?: string;
    timeframe?: string;
    start?: string;
    end?: string;
    observation_window?: number;
  }): Promise<DRLPPOEvaluation> {
    return jsonRequest(`${TRAIN_API_BASE}/models/${encodeURIComponent(modelId)}/evaluate`, payload);
  },

  evaluation(modelId: string): Promise<DRLPPOEvaluation> {
    return request(`${TRAIN_API_BASE}/models/${encodeURIComponent(modelId)}/evaluation`);
  },

  stagingConfig(modelId: string): Promise<DRLPPOStagingConfig> {
    return jsonRequest(`${TRAIN_API_BASE}/models/${encodeURIComponent(modelId)}/staging-config`, {});
  },
};
