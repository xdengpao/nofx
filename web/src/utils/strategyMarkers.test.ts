import { describe, expect, it } from 'vitest';
import {
  compareVisualMarkers,
  clusterVisualMarkers,
  expandVisualMarkers,
  markerBelongsToKline,
  markerBelongsToKlineAt,
  normalizeEpochMs,
  resolveTradeIntent,
  signalLabel,
  tradeIntentLabel,
} from './strategyMarkers';
import type { SignalMarker } from '../types';

describe('strategy marker helpers', () => {
  it('normalizes second, millisecond, microsecond and nanosecond epochs to milliseconds', () => {
    expect(normalizeEpochMs(1_779_001_200)).toBe(1_779_001_200_000);
    expect(normalizeEpochMs(1_779_001_200_000)).toBe(1_779_001_200_000);
    expect(normalizeEpochMs(1_779_001_200_000_000)).toBe(1_779_001_200_000);
    expect(normalizeEpochMs(1_779_001_200_000_000_000)).toBe(1_779_001_200_000);
  });

  it('matches marker and kline close_time after unit normalization', () => {
    expect(markerBelongsToKline(
      { close_time: 1_779_001_200 },
      { close_time: 1_779_001_200_000 },
    )).toBe(true);
    expect(markerBelongsToKline(
      { close_time: 1_779_001_260_000 },
      { close_time: 1_779_001_200_000 },
    )).toBe(false);
  });

  it('matches action markers on display or decision close time with close-time tolerance', () => {
    const marker = markerFixture({
      close_time: 1_779_001_200_000,
      signal_close_time: 1_779_001_200_000,
      decision_close_time: 1_779_004_800_000,
      display_close_time: 1_779_004_800_000,
      action: 'open_short',
      trade_intent: 'open_short',
      status: 'rejected',
    });
    expect(markerBelongsToKline(marker, { close_time: 1_779_004_800_500 })).toBe(true);
    expect(markerBelongsToKlineAt(marker.signal_close_time, { close_time: 1_779_004_800_000 })).toBe(false);
  });

  it('expands action markers into paired signal and decision visual markers', () => {
    const marker = markerFixture({
      signal_id: 'sig-pair',
      signal_type: 'sell2',
      close_time: 1_779_001_200_000,
      signal_close_time: 1_779_001_200_000,
      decision_close_time: 1_779_004_800_000,
      display_close_time: 1_779_004_800_000,
      action: 'open_short',
      trade_intent: 'open_short',
      status: 'rejected',
    });
    const visuals = expandVisualMarkers(markerArray(marker), [
      { close_time: 1_779_001_200_000 },
      { close_time: 1_779_004_800_000 },
    ]);
    expect(visuals.map((visual) => visual.kind).sort()).toEqual(['decision', 'signal']);
    expect(visuals.find((visual) => visual.kind === 'signal')?.label).toContain('结构点');
    expect(visuals.find((visual) => visual.kind === 'decision')?.label).toContain('已拒绝');
  });

  it('keeps visible side when paired close time is outside current chart range', () => {
    const marker = markerFixture({
      signal_id: 'sig-out-of-range',
      signal_type: 'sell2',
      close_time: 1_779_001_200_000,
      signal_close_time: 1_779_001_200_000,
      decision_close_time: 1_779_004_800_000,
      action: 'open_short',
      trade_intent: 'open_short',
      status: 'rejected',
    });
    const visuals = expandVisualMarkers(markerArray(marker), [{ close_time: 1_779_004_800_000 }]);
    expect(visuals).toHaveLength(1);
    expect(visuals[0].kind).toBe('decision');
    expect(visuals[0].pairOutOfRange).toBe(true);
  });

  it('merges signal and decision visuals when times are the same', () => {
    const marker = markerFixture({
      signal_id: 'sig-merged',
      close_time: 1_779_001_200_000,
      signal_close_time: 1_779_001_200_000,
      decision_close_time: 1_779_001_200_000,
      action: 'open_long',
      trade_intent: 'open_long',
      status: 'executed',
    });
    const visuals = expandVisualMarkers(markerArray(marker), [{ close_time: 1_779_001_200_000 }]);
    expect(visuals).toHaveLength(1);
    expect(visuals[0].kind).toBe('merged');
  });

  it('sorts sell markers before buy markers with stable visual keys', () => {
    const sell = expandVisualMarkers(markerArray(markerFixture({
      signal_id: 'sell',
      signal_type: 'sell2',
      direction: 'short',
      close_time: 1_779_001_200_000,
      signal_close_time: 1_779_001_200_000,
    })), [{ close_time: 1_779_001_200_000 }])[0];
    const buy = expandVisualMarkers(markerArray(markerFixture({
      signal_id: 'buy',
      signal_type: 'buy2',
      direction: 'long',
      close_time: 1_779_001_200_000,
      signal_close_time: 1_779_001_200_000,
    })), [{ close_time: 1_779_001_200_000 }])[0];
    expect([buy, sell].sort(compareVisualMarkers).map((visual) => visual.placement)).toEqual(['sell', 'buy']);
  });

  it('renders buy/sell signal type labels', () => {
    expect(signalLabel('buy1')).toBe('B1');
    expect(signalLabel('sell3')).toBe('S3');
  });

  it('uses final_action before action when resolving trade intent', () => {
    expect(resolveTradeIntent({
      action: 'partial_close',
      final_action: 'close_long',
      position_side: 'long',
      direction: 'long',
    })).toBe('close_long');
    expect(tradeIntentLabel('close_long')).toBe('平多');
  });

  it('maps partial_close with position side to reduce long or short', () => {
    expect(resolveTradeIntent({ action: 'partial_close', position_side: 'long', direction: '' })).toBe('reduce_long');
    expect(resolveTradeIntent({ action: 'partial_close', position_side: 'short', direction: '' })).toBe('reduce_short');
  });

  it('does not infer a trade intent from detected signal direction alone', () => {
    expect(resolveTradeIntent({ direction: 'long' })).toBe('');
    expect(resolveTradeIntent({ action: '', final_action: '', position_side: '', direction: 'short' })).toBe('');
  });

  it('clusters crowded markers on the same candle and placement', () => {
    const klines = [{ close_time: 1_779_001_200_000 }];
    const visuals = expandVisualMarkers([
      markerFixture({ signal_id: 's1', signal_type: 'sell2', direction: 'short' }),
      markerFixture({ signal_id: 's2', signal_type: 'sell3', direction: 'short' }),
      markerFixture({ signal_id: 's3', signal_type: 'sell1', direction: 'short' }),
    ], klines);
    const clustered = clusterVisualMarkers(visuals, 2);
    expect(clustered).toHaveLength(1);
    expect(clustered[0].kind).toBe('cluster');
    expect(clustered[0].clusterCount).toBe(3);
  });
});

function markerFixture(overrides: Partial<SignalMarker> = {}): SignalMarker {
  return {
    symbol: 'ETHUSDT',
    timeframe: '1h',
    close_time: 1_779_001_200_000,
    signal_type: 'buy2',
    direction: 'long',
    level: '1h',
    source_layer: 'main_signal',
    status: 'detected',
    signal_id: 'sig',
    ...overrides,
  };
}

function markerArray(marker: SignalMarker): SignalMarker[] {
  return [marker];
}
