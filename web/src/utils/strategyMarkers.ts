import type { MarketKline, SignalMarker } from '../types';

export type VisualMarkerKind = 'signal' | 'decision' | 'merged' | 'cluster';
export type MarkerPlacement = 'buy' | 'sell';

export type VisualSignalMarker = {
  id: string;
  marker: SignalMarker;
  kind: VisualMarkerKind;
  anchorCloseTime: number;
  pairCloseTime?: number;
  pairInRange?: boolean;
  pairOutOfRange?: boolean;
  label: string;
  placement: MarkerPlacement;
  stackKey: string;
  clusterCount?: number;
  clusterItems?: VisualSignalMarker[];
};

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
  switch (marker.display_category) {
    case 'structure_background':
      return `${label} 结构`;
    case 'preview_watch':
      return `${label} 预览`;
    case 'entry_trigger':
      return `${label} 触发`;
    case 'trade_action':
      return intent ? `${label} ${intent}${compactMarkerStatus(marker.status)}` : `${label} 动作${compactMarkerStatus(marker.status)}`;
    case 'invalid_rejected':
      return `${label} ${compactMarkerStatus(marker.status) || '失效'}`;
    case 'position_management':
      return `PM ${intent || label}`;
    default:
      break;
  }
  return intent ? `${label} · ${intent}` : label;
}

export function resolveSignalCloseTime(marker: Pick<SignalMarker, 'signal_close_time' | 'close_time'>): number {
  return normalizeEpochMs(marker.signal_close_time || marker.close_time);
}

export function resolveDecisionCloseTime(marker: Pick<SignalMarker, 'decision_close_time'>): number {
  return normalizeEpochMs(marker.decision_close_time);
}

export function resolveEvaluationCloseTime(marker: Pick<SignalMarker, 'evaluation_close_time' | 'decision_close_time'>): number {
  return normalizeEpochMs(marker.evaluation_close_time || marker.decision_close_time);
}

export function resolveActionTimestamp(marker: Pick<SignalMarker, 'action_timestamp'>): number {
  return normalizeEpochMs(marker.action_timestamp);
}

export function isActionMarker(marker: Partial<Pick<SignalMarker, 'trade_intent' | 'action' | 'final_action' | 'status'>>): boolean {
  if ((marker.trade_intent || marker.action || marker.final_action || '').trim() !== '') return true;
  const status = (marker.status || '').trim().toLowerCase();
  return status === 'rejected' || status === 'executed' || status === 'failed';
}

export function resolveDisplayCloseTime(marker: Partial<SignalMarker> & Pick<SignalMarker, 'close_time'>): number {
  const displayClose = normalizeEpochMs(marker.display_close_time);
  if (displayClose > 0) return displayClose;
  const decisionClose = resolveDecisionCloseTime(marker);
  if (isActionMarker(marker) && decisionClose > 0) return decisionClose;
  return resolveSignalCloseTime(marker);
}

export function markerBelongsToKlineAt(anchorCloseTime: number | string | undefined | null, kline: Pick<MarketKline, 'close_time'>): boolean {
  const markerClose = normalizeEpochMs(anchorCloseTime);
  const klineClose = normalizeEpochMs(kline.close_time);
  return markerClose > 0 && klineClose > 0 && Math.abs(markerClose - klineClose) <= 1000;
}

export function markerBelongsToKline(marker: Partial<SignalMarker> & Pick<SignalMarker, 'close_time'>, kline: Pick<MarketKline, 'close_time'>): boolean {
  return markerBelongsToKlineAt(resolveDisplayCloseTime(marker), kline);
}

export function markerPlacement(marker: Pick<SignalMarker, 'signal_type' | 'direction' | 'trade_intent' | 'action' | 'final_action' | 'position_side'>): MarkerPlacement {
  const signalType = (marker.signal_type || '').toLowerCase();
  if (signalType.startsWith('buy')) return 'buy';
  if (signalType.startsWith('sell')) return 'sell';

  switch (resolveTradeIntent(marker)) {
    case 'open_long':
    case 'add_long':
    case 'reduce_short':
    case 'close_short':
      return 'buy';
    case 'open_short':
    case 'add_short':
    case 'reduce_long':
    case 'close_long':
      return 'sell';
    default:
      return marker.direction === 'short' ? 'sell' : 'buy';
  }
}

export function expandVisualMarkers(markers: SignalMarker[], klines: Pick<MarketKline, 'close_time'>[]): VisualSignalMarker[] {
  const result: VisualSignalMarker[] = [];
  for (const marker of markers) {
    const signalClose = resolveSignalCloseTime(marker);
    const decisionClose = resolveDecisionCloseTime(marker);
    const hasAction = isActionMarker(marker);
    const signalVisible = signalClose > 0 && klines.some((kline) => markerBelongsToKlineAt(signalClose, kline));
    const decisionVisible = decisionClose > 0 && klines.some((kline) => markerBelongsToKlineAt(decisionClose, kline));

    if (!hasAction) {
      if (signalVisible) result.push(buildVisualMarker(marker, 'signal', signalClose));
      continue;
    }

    if (signalClose > 0 && decisionClose > 0 && Math.abs(signalClose - decisionClose) <= 1000) {
      if (signalVisible || decisionVisible) {
        result.push(buildVisualMarker(marker, 'merged', decisionClose || signalClose, signalClose));
      }
      continue;
    }

    if (signalVisible) {
      result.push(buildVisualMarker(marker, 'signal', signalClose, decisionClose, decisionVisible, decisionClose > 0 && !decisionVisible));
    }
    if (decisionVisible) {
      result.push(buildVisualMarker(marker, 'decision', decisionClose, signalClose, signalVisible, signalClose > 0 && !signalVisible));
    }
    if (!signalVisible && !decisionVisible && signalClose === 0 && decisionClose > 0) {
      result.push(buildVisualMarker(marker, 'decision', decisionClose));
    }
  }
  return result.sort(compareVisualMarkers);
}

export function clusterVisualMarkers(markers: VisualSignalMarker[], maxLabelsPerSide = 2): VisualSignalMarker[] {
  const groups = new Map<string, VisualSignalMarker[]>();
  for (const marker of markers) {
    const group = groups.get(marker.stackKey) ?? [];
    group.push(marker);
    groups.set(marker.stackKey, group);
  }
  const out: VisualSignalMarker[] = [];
  for (const group of groups.values()) {
    const sorted = [...group].sort(compareVisualMarkers);
    if (sorted.length <= maxLabelsPerSide) {
      out.push(...sorted);
      continue;
    }
    const representative = sorted[0];
    const side = representative.placement === 'sell' ? 'S' : 'B';
    out.push({
      ...representative,
      id: `${representative.stackKey}-cluster-${sorted.length}`,
      kind: 'cluster',
      label: `${side} x${sorted.length}`,
      clusterCount: sorted.length,
      clusterItems: sorted,
    });
  }
  return compactAdjacentVisualMarkers(out.sort(compareVisualMarkers));
}

function compactAdjacentVisualMarkers(markers: VisualSignalMarker[]): VisualSignalMarker[] {
  const out: VisualSignalMarker[] = [];
  const used = new Set<number>();
  for (let i = 0; i < markers.length; i++) {
    if (used.has(i)) continue;
    const current = markers[i];
    const nextIndex = markers.findIndex((candidate, index) => (
      index > i
      && !used.has(index)
      && candidate.placement === current.placement
      && candidate.kind !== 'cluster'
      && current.kind !== 'cluster'
      && candidate.marker.signal_id !== current.marker.signal_id
      && Math.abs(normalizeEpochMs(candidate.anchorCloseTime) - normalizeEpochMs(current.anchorCloseTime)) <= 4 * 60 * 60 * 1000
      && current.label.length + candidate.label.length >= 10
    ));
    if (nextIndex === -1) {
      out.push(current);
      continue;
    }
    const pair = [current, markers[nextIndex]].sort(compareVisualMarkers);
    used.add(nextIndex);
    const representative = pair[0];
    const side = representative.placement === 'sell' ? 'S' : 'B';
    out.push({
      ...representative,
      id: `${representative.stackKey}-adjacent-cluster-${pair.map((item) => item.id).join('-')}`,
      kind: 'cluster',
      label: `${side} x${pair.length}`,
      clusterCount: pair.length,
      clusterItems: pair,
    });
  }
  return out.sort(compareVisualMarkers);
}

export function compareVisualMarkers(a: VisualSignalMarker, b: VisualSignalMarker): number {
  return visualMarkerSortKey(a).localeCompare(visualMarkerSortKey(b));
}

function buildVisualMarker(
  marker: SignalMarker,
  kind: VisualMarkerKind,
  anchorCloseTime: number,
  pairCloseTime = 0,
  pairInRange = false,
  pairOutOfRange = false,
): VisualSignalMarker {
  const placement = markerPlacement(marker);
  return {
    id: `${marker.signal_id || 'signal'}-${kind}-${anchorCloseTime}`,
    marker,
    kind,
    anchorCloseTime,
    pairCloseTime: pairCloseTime || undefined,
    pairInRange,
    pairOutOfRange,
    label: visualMarkerLabel(marker, kind),
    placement,
    stackKey: `${normalizeEpochMs(anchorCloseTime)}-${placement}`,
  };
}

function visualMarkerLabel(marker: SignalMarker, kind: VisualMarkerKind): string {
  if (kind === 'signal' && isActionMarker(marker)) return `${signalLabel(marker.signal_type)} · 结构点`;
  const base = markerDisplayLabel(marker);
  const status = markerStatusLabel(marker.status);
  if (kind === 'decision' || kind === 'merged') {
    return marker.display_category ? base : status && status !== '--' ? `${base} · ${status}` : base;
  }
  return base;
}

function visualMarkerSortKey(visual: VisualSignalMarker): string {
  const marker = visual.marker;
  const placementRank = visual.placement === 'sell' ? '0' : '1';
  const actionRank = isActionMarker(marker) && visual.kind !== 'signal' ? '0' : '1';
  const kindRank = visual.kind === 'decision' ? '0' : visual.kind === 'merged' ? '1' : '2';
  return [
    normalizeEpochMs(visual.anchorCloseTime).toString().padStart(16, '0'),
    placementRank,
    actionRank,
    kindRank,
    String(99 - (marker.display_priority ?? 0)).padStart(3, '0'),
    signalLabel(marker.signal_type),
    resolveTradeIntent(marker),
    marker.signal_id || '',
  ].join('|');
}

function compactMarkerStatus(status?: string): string {
  switch ((status || '').toLowerCase()) {
    case 'executed':
      return '成';
    case 'rejected':
      return '拒';
    case 'failed':
      return '败';
    case 'invalidated':
      return '失效';
    case 'ready':
      return '备';
    default:
      return '';
  }
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
