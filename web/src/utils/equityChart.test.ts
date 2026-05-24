import { describe, expect, it } from 'vitest';
import { buildEquityChartData } from './equityChart';

describe('buildEquityChartData', () => {
  it('uses backend cost basis instead of first equity for 161-like history', () => {
    const result = buildEquityChartData(
      [{
        timestamp: '2026-05-24 12:00:00',
        total_equity: 44.48471397,
        total_pnl: 0.0717623067,
        total_pnl_pct: 0.161584,
        cost_basis: 44.4129516633,
        return_reliable: true,
        cycle_number: 12,
      }],
      {
        total_equity: 44.48471397,
        initial_balance: 10,
        cost_basis: 44.4129516633,
      },
      'percent'
    );

    expect(result.strategyBaseline).toBeCloseTo(44.4129516633, 8);
    expect(result.chartData[0].raw_pnl).toBeCloseTo(0.0717623067, 8);
    expect(result.chartData[0].raw_pnl_pct).toBeCloseTo(0.161584, 6);
  });

  it('does not publish percent values for unreliable legacy points', () => {
    const result = buildEquityChartData(
      [{
        timestamp: '2026-05-24 12:00:00',
        total_equity: 44.48,
        total_pnl: 34.48,
        total_pnl_pct: 344.8,
        cost_basis: 10,
        return_reliable: false,
        cycle_number: 1,
      }],
      { initial_balance: 10 },
      'percent'
    );

    expect(result.chartData[0].value).toBeNull();
    expect(result.chartData[0].raw_pnl_pct).toBeNull();
  });
});
