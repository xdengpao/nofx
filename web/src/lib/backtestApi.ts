import type {
  BacktestCoverage,
  BacktestHealth,
  BacktestJob,
  BacktestKlineResponse,
  BacktestMarkerResponse,
  BacktestReport,
} from '../types/backtest';

const API_BASE = '/api/backtest';

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init);
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error || `请求失败: ${res.status}`);
  }
  return res.json();
}

export const backtestApi = {
  health(): Promise<BacktestHealth> {
    return request(`${API_BASE}/health`);
  },

  inspect(): Promise<BacktestCoverage[]> {
    return request(`${API_BASE}/history/inspect`);
  },

  fetchHistory(payload: Record<string, unknown>): Promise<BacktestJob> {
    return request(`${API_BASE}/history/fetch`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  },

  run(payload: Record<string, unknown>): Promise<BacktestJob> {
    return request(`${API_BASE}/runs`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  },

  jobs(): Promise<BacktestJob[]> {
    return request(`${API_BASE}/runs`);
  },

  report(runId: string): Promise<BacktestReport> {
    return request(`${API_BASE}/reports/${encodeURIComponent(runId)}`);
  },

  klines(symbol: string, timeframe: string, from: string, to: string): Promise<BacktestKlineResponse> {
    const params = new URLSearchParams({ symbol, timeframe, from, to });
    return request(`${API_BASE}/history/klines?${params.toString()}`);
  },

  markers(runId: string, symbol: string, timeframe: string): Promise<BacktestMarkerResponse> {
    return request(`${API_BASE}/reports/${encodeURIComponent(runId)}/files/markers/${encodeURIComponent(`${symbol}_${timeframe}.json`)}`);
  },
};
