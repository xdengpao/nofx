import type { SignalDisplayCategory, SignalMarker, StrategySignalReport } from '../types';
import {
  markerStatusLabel,
  normalizeEpochMs,
  resolveDecisionCloseTime,
  resolveDisplayCloseTime,
  resolveSignalCloseTime,
  resolveTradeIntent,
  signalLabel,
  tradeIntentLabel,
} from './strategyMarkers';

export type SignalDisplayTone = 'buy' | 'sell' | 'muted' | 'warning' | 'action';

export interface SignalDisplayItem {
  id: string;
  marker: SignalMarker;
  category: SignalDisplayCategory;
  title: string;
  shortLabel: string;
  tone: SignalDisplayTone;
  priority: number;
  defaultVisible: boolean;
  summary: string;
  tooltipRows: Array<{ label: string; value: string }>;
}

export interface SignalDisplayFilters {
  layers?: string[];
  statuses?: string[];
  audit?: boolean;
}

export interface SignalDisplayModel {
  latest?: SignalDisplayItem;
  chartMarkers: SignalMarker[];
  hiddenCount: number;
  collapsedCount: number;
  items: SignalDisplayItem[];
}

export function buildSignalDisplayModel(report?: StrategySignalReport, filters: SignalDisplayFilters = {}): SignalDisplayModel {
  const markers = report?.signal_markers ?? [];
  const layerSet = toSet(filters.layers);
  const statusSet = toSet(filters.statuses);
  const items = markers
    .map((marker) => buildSignalDisplayItem(marker, Boolean(filters.audit)))
    .filter((item) => matchesDisplayFilters(item, layerSet, statusSet))
    .sort(compareDisplayItems);
  const visible = items.filter((item) => item.defaultVisible || filters.audit);
  const latest = visible[0] ?? items[0];
  return {
    latest,
    chartMarkers: visible.map((item) => item.marker),
    hiddenCount: Math.max(report?.marker_summary?.hidden_by_default ?? 0, items.length - visible.length),
    collapsedCount: report?.marker_summary?.collapsed_lifecycle ?? items.reduce((sum, item) => sum + (item.marker.collapsed_count ?? 0), 0),
    items,
  };
}

export function buildSignalDisplayItem(marker: SignalMarker, audit = false): SignalDisplayItem {
  const category = deriveSignalCategory(marker);
  const priority = marker.display_priority ?? categoryPriority(category, marker);
  const shortLabel = compactMarkerLabel(marker, category);
  const title = categoryTitle(category, marker);
  const defaultVisible = audit || !marker.hidden_by_default;
  return {
    id: marker.lifecycle_key || `${marker.signal_id}-${marker.source_layer}-${marker.close_time}`,
    marker,
    category,
    title,
    shortLabel,
    tone: markerToneForDisplay(marker, category),
    priority,
    defaultVisible,
    summary: markerSummary(marker, category),
    tooltipRows: tooltipRows(marker, category),
  };
}

export function deriveSignalCategory(marker: SignalMarker): SignalDisplayCategory {
  if (marker.display_category) return marker.display_category;
  if (marker.source_layer === 'position_management') return 'position_management';
  if (marker.source_layer === 'preview_signal') return 'preview_watch';
  if (hasTradeAction(marker)) return 'trade_action';
  if (marker.source_layer === 'entry_trigger' || marker.entry_trigger_id) return 'entry_trigger';
  const status = (marker.status || '').toLowerCase();
  if (['invalidated', 'rejected', 'failed', 'expired', 'deduped', 'suppressed'].includes(status)) return 'invalid_rejected';
  return 'structure_background';
}

function categoryPriority(category: SignalDisplayCategory, marker: SignalMarker) {
  const status = (marker.status || '').toLowerCase();
  if (category === 'trade_action') {
    if (status === 'executed' || status === 'failed') return 100;
    if (status === 'rejected') return 80;
    return 85;
  }
  if (category === 'entry_trigger') return status === 'ready' ? 90 : 75;
  if (category === 'position_management') return 70;
  if (category === 'structure_background') return 60;
  if (category === 'invalid_rejected') return 55;
  return 40;
}

function compactMarkerLabel(marker: SignalMarker, category: SignalDisplayCategory) {
  const label = signalLabel(marker.signal_type);
  const intent = tradeIntentLabel(resolveTradeIntent(marker));
  const status = compactStatus(marker.status);
  switch (category) {
    case 'structure_background':
      return `${label} 结构`;
    case 'preview_watch':
      return `${label} 预览`;
    case 'entry_trigger':
      return `${label} 触发`;
    case 'trade_action':
      return `${label} ${intent || '动作'}${status}`;
    case 'position_management':
      return `PM ${intent || label}`;
    case 'invalid_rejected':
      return `${label} ${status || '失效'}`;
    default:
      return label;
  }
}

function categoryTitle(category: SignalDisplayCategory, marker: SignalMarker) {
  switch (category) {
    case 'structure_background':
      return '结构背景';
    case 'preview_watch':
      return `预览观察 ${marker.preview_phase || ''}`.trim();
    case 'entry_trigger':
      return '入场触发';
    case 'trade_action':
      return tradeIntentLabel(resolveTradeIntent(marker)) || '交易动作';
    case 'position_management':
      return '持仓管理';
    case 'invalid_rejected':
      return '失效/拒绝';
    default:
      return '信号';
  }
}

function markerSummary(marker: SignalMarker, category: SignalDisplayCategory) {
  const age = typeof marker.age_candles === 'number' && marker.age_candles > 0 ? ` · age ${marker.age_candles}` : '';
  const status = markerStatusLabel(marker.status);
  const reason = marker.reason_code || marker.entry_invalidation_reason || marker.entry_window_state;
  if (category === 'preview_watch') {
    return `${marker.preview_phase || 'preview'} · ${marker.preview_closed_components || '--'} components · ${status}`;
  }
  if (category === 'entry_trigger') {
    return `${marker.entry_trigger_type || 'trigger'} · ${marker.entry_trigger_timeframe || marker.timeframe} · ${status}`;
  }
  return `${status}${age}${reason ? ` · ${reason}` : ''}`;
}

function tooltipRows(marker: SignalMarker, category: SignalDisplayCategory) {
  const rows = [
    { label: '类别', value: categoryTitle(category, marker) },
    { label: '状态', value: markerStatusLabel(marker.status) },
    { label: '结构时间', value: formatTime(resolveSignalCloseTime(marker)) },
    { label: '决策时间', value: formatTime(resolveDecisionCloseTime(marker)) },
    { label: '年龄', value: marker.age_candles ? `${marker.age_candles} 根` : '--' },
    { label: '层级', value: marker.source_layer || '--' },
    { label: '原因', value: marker.reason_code || marker.reason || marker.entry_invalidation_reason || '--' },
    { label: '父结构', value: marker.parent_structure_key || marker.parent_signal_id || '--' },
    { label: '触发', value: marker.entry_trigger_id || '--' },
    { label: '生命周期', value: marker.lifecycle_key || '--' },
    { label: 'signal', value: marker.signal_id || '--' },
  ];
  if (marker.collapsed_count) rows.splice(2, 0, { label: '折叠', value: `${marker.collapsed_count}` });
  return rows;
}

function markerToneForDisplay(marker: SignalMarker, category: SignalDisplayCategory): SignalDisplayTone {
  if (category === 'trade_action') return 'action';
  if (category === 'invalid_rejected' || marker.status === 'rejected' || marker.status === 'failed') return 'warning';
  const signalType = (marker.signal_type || '').toLowerCase();
  if (signalType.startsWith('buy') || marker.direction === 'long') return 'buy';
  if (signalType.startsWith('sell') || marker.direction === 'short') return 'sell';
  return 'muted';
}

function compareDisplayItems(a: SignalDisplayItem, b: SignalDisplayItem) {
  if (a.priority !== b.priority) return b.priority - a.priority;
  return resolveDisplayCloseTime(b.marker) - resolveDisplayCloseTime(a.marker);
}

function matchesDisplayFilters(item: SignalDisplayItem, layerSet: Set<string>, statusSet: Set<string>) {
  if (layerSet.size > 0 && !layerSet.has(item.category) && !layerSet.has(item.marker.source_layer)) return false;
  if (statusSet.size > 0 && !statusSet.has((item.marker.status || '').toLowerCase())) return false;
  return true;
}

function hasTradeAction(marker: SignalMarker) {
  return Boolean(marker.trade_intent || marker.action || marker.final_action || ['executed', 'failed'].includes((marker.status || '').toLowerCase()));
}

function compactStatus(status?: string) {
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

function toSet(values?: string[]) {
  return new Set((values ?? []).map((value) => value.toLowerCase()).filter(Boolean));
}

function formatTime(value: number) {
  const normalized = normalizeEpochMs(value);
  return normalized > 0 ? new Date(normalized).toLocaleString() : '--';
}
