import type { MarketKline, SignalMarker } from '../types';

export type TradeIntent =
  | 'open_long'
  | 'open_short'
  | 'add_long'
  | 'add_short'
  | 'reduce_long'
  | 'reduce_short'
  | 'close_long'
  | 'close_short'
  | 'reduce_skipped';

export function normalizeEpochMs(value: number | string | undefined | null): number {
  if (value === undefined || value === null || value === '') return 0;
  if (typeof value === 'string') {
    const numeric = Number(value);
    if (Number.isFinite(numeric)) return normalizeEpochMs(numeric);
    const parsed = Date.parse(value);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  if (!Number.isFinite(value) || value <= 0) return 0;
  const abs = Math.abs(value);
  if (abs < 1e11) return Math.round(value * 1000);
  if (abs < 1e14) return Math.round(value);
  if (abs < 1e17) return Math.round(value / 1000);
  return Math.round(value / 1_000_000);
}

export function signalLabel(signalType?: string): string {
  const normalized = (signalType || '').trim().toLowerCase();
  const match = normalized.match(/^(buy|sell)([123])$/);
  if (!match) return normalized ? normalized.toUpperCase() : '--';
  return `${match[1] === 'buy' ? 'B' : 'S'}${match[2]}`;
}

export function resolveTradeIntent(marker: Pick<SignalMarker, 'trade_intent' | 'action' | 'final_action' | 'position_side' | 'direction'>): TradeIntent | '' {
  if (isTradeIntent(marker.trade_intent)) return marker.trade_intent;
  const action = (marker.final_action || marker.action || '').trim().toLowerCase();
  switch (action) {
    case 'open_long':
    case 'open_short':
    case 'add_long':
    case 'add_short':
    case 'close_long':
    case 'close_short':
      return action;
    case 'partial_close_skipped':
      return 'reduce_skipped';
    case 'partial_close': {
      const side = normalizeSide(marker.position_side || marker.direction);
      if (side === 'long') return 'reduce_long';
      if (side === 'short') return 'reduce_short';
      return '';
    }
    default:
      return '';
  }
}

export function tradeIntentLabel(intent?: string): string {
  switch (intent) {
    case 'open_long':
      return '开多';
    case 'open_short':
      return '开空';
    case 'add_long':
      return '加多';
    case 'add_short':
      return '加空';
    case 'reduce_long':
      return '减多';
    case 'reduce_short':
      return '减空';
    case 'close_long':
      return '平多';
    case 'close_short':
      return '平空';
    case 'reduce_skipped':
      return '减仓跳过';
    default:
      return '';
  }
}

export function markerDisplayLabel(marker: SignalMarker): string {
  const label = signalLabel(marker.signal_type);
  const intent = tradeIntentLabel(resolveTradeIntent(marker));
  return intent ? `${label} · ${intent}` : label;
}

export function markerBelongsToKline(marker: Pick<SignalMarker, 'close_time'>, kline: Pick<MarketKline, 'close_time'>): boolean {
  const markerClose = normalizeEpochMs(marker.close_time);
  const klineClose = normalizeEpochMs(kline.close_time);
  return markerClose > 0 && klineClose > 0 && markerClose === klineClose;
}

export function markerStatusLabel(status?: string): string {
  switch ((status || '').toLowerCase()) {
    case 'executed':
      return '已执行';
    case 'rejected':
      return '已拒绝';
    case 'failed':
      return '失败';
    case 'deduped':
      return '已去重';
    case 'detected':
      return '已检测';
    default:
      return status || '--';
  }
}

export function markerTone(marker: Pick<SignalMarker, 'signal_type' | 'direction' | 'status'>): 'buy' | 'sell' | 'muted' {
  const status = (marker.status || '').toLowerCase();
  if (status === 'rejected' || status === 'failed' || status === 'deduped') return 'muted';
  const signalType = (marker.signal_type || '').toLowerCase();
  if (signalType.startsWith('buy') || marker.direction === 'long') return 'buy';
  if (signalType.startsWith('sell') || marker.direction === 'short') return 'sell';
  return 'muted';
}

export function normalizeSide(value?: string): 'long' | 'short' | '' {
  switch ((value || '').trim().toLowerCase()) {
    case 'long':
    case 'buy':
    case 'bull':
    case 'bullish':
      return 'long';
    case 'short':
    case 'sell':
    case 'bear':
    case 'bearish':
      return 'short';
    default:
      return '';
  }
}

function isTradeIntent(value?: string): value is TradeIntent {
  return value === 'open_long'
    || value === 'open_short'
    || value === 'add_long'
    || value === 'add_short'
    || value === 'reduce_long'
    || value === 'reduce_short'
    || value === 'close_long'
    || value === 'close_short'
    || value === 'reduce_skipped';
}
