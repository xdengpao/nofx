export type EquityDisplayMode = 'dollar' | 'percent';

export interface EquityHistoryPoint {
  timestamp: string;
  total_equity: number;
  available_balance?: number;
  total_pnl?: number;
  total_pnl_pct?: number;
  cost_basis?: number;
  strategy_baseline?: number;
  baseline_source?: string;
  equity_source?: string;
  return_reliable?: boolean;
  cycle_number: number;
}

export interface EquityAccountInfo {
  total_equity?: number;
  total_pnl?: number;
  total_pnl_pct?: number;
  cost_basis?: number;
  strategy_baseline?: number;
  initial_balance?: number;
}

export interface EquityChartDatum {
  time: string;
  value: number | null;
  cycle: number;
  raw_equity: number;
  raw_pnl: number | null;
  raw_pnl_pct: number | null;
  basis: number;
  return_reliable: boolean;
}

export function validEquityHistory(history?: EquityHistoryPoint[]): EquityHistoryPoint[] {
  return history?.filter(point => point.total_equity > 1) || [];
}

export function resolveEquityBasis(point: EquityHistoryPoint, account?: EquityAccountInfo): number {
  if (point.strategy_baseline && point.strategy_baseline > 0) return point.strategy_baseline;
  if (point.cost_basis && point.cost_basis > 0) return point.cost_basis;
  if (account?.strategy_baseline && account.strategy_baseline > 0) return account.strategy_baseline;
  if (account?.cost_basis && account.cost_basis > 0) return account.cost_basis;
  if (account?.initial_balance && account.initial_balance > 0) return account.initial_balance;
  if (point.total_pnl !== undefined && point.total_equity - point.total_pnl > 0) {
    return point.total_equity - point.total_pnl;
  }
  return account?.total_equity || point.total_equity || 100;
}

export function buildEquityChartData(
  history: EquityHistoryPoint[] | undefined,
  account: EquityAccountInfo | undefined,
  displayMode: EquityDisplayMode,
  maxDisplayPoints = 2000
) {
  const validHistory = validEquityHistory(history);
  const displayHistory = validHistory.length > maxDisplayPoints
    ? validHistory.slice(-maxDisplayPoints)
    : validHistory;

  const chartData: EquityChartDatum[] = displayHistory.map(point => {
    const basis = resolveEquityBasis(point, account);
    const returnReliable = point.return_reliable !== false && basis > 0;
    const fallbackPnl = point.total_equity - basis;
    const rawPnl = returnReliable ? (point.total_pnl ?? fallbackPnl) : null;
    const rawPnlPct = returnReliable
      ? (point.total_pnl_pct ?? (rawPnl !== null ? (rawPnl / basis) * 100 : null))
      : null;

    return {
      time: new Date(point.timestamp).toLocaleTimeString('zh-CN', {
        hour: '2-digit',
        minute: '2-digit',
      }),
      value: displayMode === 'dollar' ? point.total_equity : rawPnlPct,
      cycle: point.cycle_number,
      raw_equity: point.total_equity,
      raw_pnl: rawPnl,
      raw_pnl_pct: rawPnlPct,
      basis,
      return_reliable: returnReliable,
    };
  });

  const latestReliable = [...chartData].reverse().find(point => point.return_reliable);
  const strategyBaseline = latestReliable?.basis
    || account?.strategy_baseline
    || account?.cost_basis
    || account?.initial_balance
    || account?.total_equity
    || 100;

  return {
    validHistory,
    displayHistory,
    chartData,
    strategyBaseline,
    currentValue: chartData[chartData.length - 1],
    latestReliable,
  };
}
