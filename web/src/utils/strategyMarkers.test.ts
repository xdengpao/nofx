import { describe, expect, it } from 'vitest';
import {
  markerBelongsToKline,
  normalizeEpochMs,
  resolveTradeIntent,
  signalLabel,
  tradeIntentLabel,
} from './strategyMarkers';

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
});
