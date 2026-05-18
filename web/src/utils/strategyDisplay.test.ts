import { describe, expect, it } from 'vitest';
import { buildSignalDisplayModel, deriveSignalCategory } from './strategyDisplay';
import type { SignalMarker, StrategySignalReport } from '../types';

describe('strategy display model', () => {
  it('derives categories without symbol-specific behavior', () => {
    expect(deriveSignalCategory(markerFixture({ symbol: 'BTCUSDT', source_layer: 'preview_signal' }))).toBe('preview_watch');
    expect(deriveSignalCategory(markerFixture({ symbol: 'ETHUSDT', source_layer: 'preview_signal' }))).toBe('preview_watch');
    expect(deriveSignalCategory(markerFixture({ source_layer: 'entry_trigger', entry_trigger_id: 'trigger-1' }))).toBe('entry_trigger');
    expect(deriveSignalCategory(markerFixture({ action: 'open_short', trade_intent: 'open_short', status: 'rejected' }))).toBe('trade_action');
  });

  it('selects actionable items before stale structures and counts hidden markers', () => {
    const report: StrategySignalReport = {
      trader_id: 't1',
      symbol: 'SOLUSDT',
      decision_mode: 'programmatic',
      signals: [],
      signal_markers: [
        markerFixture({
          signal_id: 'old-structure',
          source_layer: 'structure',
          display_category: 'structure_background',
          status: 'background',
          display_priority: 60,
          reason_code: 'waiting_for_fresh_entry_trigger',
        }),
        markerFixture({
          signal_id: 'ready-trigger',
          source_layer: 'entry_trigger',
          display_category: 'entry_trigger',
          status: 'ready',
          display_priority: 90,
          entry_trigger_id: 'trigger-1',
        }),
        markerFixture({
          signal_id: 'preview-hidden',
          source_layer: 'preview_signal',
          display_category: 'preview_watch',
          status: 'confirmed',
          hidden_by_default: true,
        }),
      ],
      marker_summary: {
        total_raw: 3,
        total_returned: 2,
        hidden_by_default: 1,
        collapsed_lifecycle: 4,
        suppressed_repeats: 2,
        preview_hidden: 1,
      },
    };
    const model = buildSignalDisplayModel(report);
    expect(model.latest?.marker.signal_id).toBe('ready-trigger');
    expect(model.chartMarkers.map((marker) => marker.signal_id)).not.toContain('preview-hidden');
    expect(model.hiddenCount).toBe(1);
    expect(model.collapsedCount).toBe(4);
  });

  it('applies the same 161-like marker template across active symbols', () => {
    const symbols = ['BTCUSDT', 'ETHUSDT', 'SOLUSDT', 'XAGUSDT', 'XRPUSDT', 'CLUSDT'];
    for (const symbol of symbols) {
      const report: StrategySignalReport = {
        trader_id: 'aster_deepseek',
        symbol,
        decision_mode: 'programmatic',
        signals: [],
        signal_markers: [
          markerFixture({
            symbol,
            signal_id: `${symbol}-preview`,
            source_layer: 'preview_signal',
            display_category: 'preview_watch',
            status: 'confirmed',
            hidden_by_default: true,
            preview_phase: 'preview_3x15m',
          }),
          markerFixture({
            symbol,
            signal_id: `${symbol}-invalid`,
            source_layer: 'structure',
            display_category: 'invalid_rejected',
            status: 'invalidated',
            reason_code: 'target_already_crossed',
            collapsed_count: 3,
          }),
        ],
      };
      const model = buildSignalDisplayModel(report);
      expect(model.latest?.shortLabel).toBe('S2 失效');
      expect(model.chartMarkers).toHaveLength(1);
      expect(model.chartMarkers[0].symbol).toBe(symbol);
    }
  });
});

function markerFixture(overrides: Partial<SignalMarker> = {}): SignalMarker {
  return {
    symbol: 'BTCUSDT',
    timeframe: '1h',
    close_time: 1_779_001_200_000,
    signal_type: 'sell2',
    direction: 'short',
    level: '1h',
    source_layer: 'structure',
    status: 'detected',
    signal_id: 'sig',
    ...overrides,
  };
}
