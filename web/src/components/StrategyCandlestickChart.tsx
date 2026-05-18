import { useMemo, useRef, useState, type MouseEvent, type ReactNode } from 'react';
import type { MarketKline, SignalMarker } from '../types';
import {
  compareVisualMarkers,
  clusterVisualMarkers,
  expandVisualMarkers,
  markerBelongsToKlineAt,
  markerStatusLabel,
  markerTone,
  normalizeEpochMs,
  resolveDecisionCloseTime,
  resolveSignalCloseTime,
  resolveTradeIntent,
  signalLabel,
  tradeIntentLabel,
  type VisualSignalMarker,
} from '../utils/strategyMarkers';

type StrategyCandlestickChartProps = {
  symbol: string;
  timeframe: string;
  klines: MarketKline[];
  markers: SignalMarker[];
  limit?: number;
  configuredLimit?: number;
  limitSource?: string;
  error?: string;
};

type HoverState = {
  x: number;
  y: number;
  kline: MarketKline;
  markers: VisualSignalMarker[];
};

const chartHeight = 420;
const topPad = 28;
const bottomPad = 52;
const leftPad = 66;
const rightPad = 24;

export function StrategyCandlestickChart({
  symbol,
  timeframe,
  klines,
  markers,
  limit,
  configuredLimit,
  limitSource,
  error,
}: StrategyCandlestickChartProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<HoverState | null>(null);

  const visualMarkers = useMemo(
    () => clusterVisualMarkers(expandVisualMarkers(markers, klines ?? []), 2),
    [markers, klines],
  );

  const chart = useMemo(() => {
    const candles = klines ?? [];
    const markerPrices = visualMarkers.map((visual) => visual.marker.price).filter((price): price is number => typeof price === 'number' && Number.isFinite(price) && price > 0);
    const lows = candles.map((kline) => kline.low);
    const highs = candles.map((kline) => kline.high);
    const minPrice = Math.min(...lows, ...markerPrices);
    const maxPrice = Math.max(...highs, ...markerPrices);
    const safeMin = Number.isFinite(minPrice) ? minPrice : 0;
    const safeMax = Number.isFinite(maxPrice) ? maxPrice : 1;
    const span = Math.max(safeMax - safeMin, Math.max(Math.abs(safeMax), 1) * 0.002);
    const paddedMin = safeMin - span * 0.08;
    const paddedMax = safeMax + span * 0.08;
    const barStep = candles.length > 500 ? 8 : candles.length > 240 ? 10 : 14;
    const candleWidth = Math.max(4, Math.min(9, barStep - 4));
    const width = Math.max(760, leftPad + rightPad + candles.length * barStep);
    const priceToY = (price: number) => {
      const ratio = (paddedMax - price) / Math.max(paddedMax - paddedMin, Number.EPSILON);
      return topPad + ratio * (chartHeight - topPad - bottomPad);
    };
    const points = candles.map((kline, index) => {
      const x = leftPad + index * barStep + barStep / 2;
      const rowMarkers = visualMarkers
        .filter((visual) => markerBelongsToKlineAt(visual.anchorCloseTime, kline))
        .sort(compareVisualMarkers);
      return { kline, index, x, markers: rowMarkers };
    });
    return { candleWidth, points, priceToY, width, min: paddedMin, max: paddedMax };
  }, [klines, visualMarkers]);

  if (error) {
    return <ChartShell symbol={symbol} timeframe={timeframe} meta="K线接口异常">
      <div className="h-80 flex items-center justify-center text-sm" style={{ color: '#F6465D' }}>{error}</div>
    </ChartShell>;
  }

  if (!klines || klines.length === 0) {
    return <ChartShell symbol={symbol} timeframe={timeframe} meta="暂无闭合K线">
      <div className="h-80 flex items-center justify-center text-sm" style={{ color: '#848E9C' }}>暂无主交易K线</div>
    </ChartShell>;
  }

  const gridTicks = Array.from({ length: 5 }, (_, index) => chart.min + ((chart.max - chart.min) * index) / 4).reverse();
  const timeLabelEvery = Math.max(1, Math.ceil(chart.points.length / 6));
  const meta = [
    `展示 ${klines.length}${limit ? `/${limit}` : ''} 根`,
    configuredLimit ? `配置 ${configuredLimit}` : '',
    limitSourceLabel(limitSource),
  ].filter(Boolean).join(' · ');

  const updateHover = (event: MouseEvent, kline: MarketKline, rowMarkers: VisualSignalMarker[]) => {
    const rect = containerRef.current?.getBoundingClientRect();
    if (!rect) return;
    setHover({
      x: event.clientX - rect.left + 12,
      y: event.clientY - rect.top + 12,
      kline,
      markers: rowMarkers,
    });
  };

  return (
    <ChartShell symbol={symbol} timeframe={timeframe} meta={meta}>
      <div ref={containerRef} className="relative">
        <div className="overflow-x-auto pb-2">
          <svg
            role="img"
            aria-label={`${symbol} ${timeframe} 主交易K线`}
            width={chart.width}
            height={chartHeight}
            className="block"
            onMouseLeave={() => setHover(null)}
          >
            <rect x={0} y={0} width={chart.width} height={chartHeight} fill="#0B0E11" />
            {gridTicks.map((price) => {
              const y = chart.priceToY(price);
              return (
                <g key={price}>
                  <line x1={leftPad} x2={chart.width - rightPad} y1={y} y2={y} stroke="#1E2329" strokeWidth={1} />
                  <text x={leftPad - 8} y={y + 4} textAnchor="end" fill="#848E9C" fontSize={11} fontFamily="monospace">
                    {formatPrice(price)}
                  </text>
                </g>
              );
            })}
            {chart.points.map(({ kline, index, x, markers: rowMarkers }) => {
              const openY = chart.priceToY(kline.open);
              const closeY = chart.priceToY(kline.close);
              const highY = chart.priceToY(kline.high);
              const lowY = chart.priceToY(kline.low);
              const up = kline.close >= kline.open;
              const color = up ? '#0ECB81' : '#F6465D';
              const bodyY = Math.min(openY, closeY);
              const bodyHeight = Math.max(1, Math.abs(closeY - openY));
              const showTimeLabel = index === 0 || index === chart.points.length - 1 || index % timeLabelEvery === 0;
              return (
                <g
                  key={kline.close_time}
                  onMouseMove={(event) => updateHover(event, kline, rowMarkers)}
                  onClick={(event) => updateHover(event, kline, rowMarkers)}
                >
                  <line x1={x} x2={x} y1={highY} y2={lowY} stroke={color} strokeWidth={1.2} />
                  <rect
                    x={x - chart.candleWidth / 2}
                    y={bodyY}
                    width={chart.candleWidth}
                    height={bodyHeight}
                    fill={up ? 'rgba(14, 203, 129, 0.22)' : 'rgba(246, 70, 93, 0.22)'}
                    stroke={color}
                    strokeWidth={1}
                  />
                  {showTimeLabel && (
                    <text x={x} y={chartHeight - 20} textAnchor="middle" fill="#848E9C" fontSize={10}>
                      {formatShortTime(kline.close_time)}
                    </text>
                  )}
                  {rowMarkers.map((visual, markerIndex) => (
                    <MarkerGlyph
                      key={`${visual.id}-${markerIndex}`}
                      visual={visual}
                      x={x}
                      y={markerY(visual, kline, chart.priceToY, markerStackIndex(rowMarkers, markerIndex))}
                      placement={visual.placement}
                      onMouseMove={(event) => updateHover(event, kline, rowMarkers)}
                    />
                  ))}
                </g>
              );
            })}
          </svg>
        </div>

        {hover && (
          <div
            className="pointer-events-none absolute z-20 w-72 rounded p-3 text-xs shadow-xl"
            style={{
              left: Math.min(hover.x, Math.max(12, (containerRef.current?.clientWidth ?? 320) - 300)),
              top: hover.y,
              background: '#1E2329',
              border: '1px solid #2B3139',
              color: '#EAECEF',
            }}
          >
            <div className="font-semibold mb-2">{new Date(normalizeEpochMs(hover.kline.close_time)).toLocaleString()}</div>
            <div className="grid grid-cols-2 gap-x-3 gap-y-1 font-mono">
              <span style={{ color: '#848E9C' }}>O</span><span>{formatPrice(hover.kline.open)}</span>
              <span style={{ color: '#848E9C' }}>H</span><span>{formatPrice(hover.kline.high)}</span>
              <span style={{ color: '#848E9C' }}>L</span><span>{formatPrice(hover.kline.low)}</span>
              <span style={{ color: '#848E9C' }}>C</span><span>{formatPrice(hover.kline.close)}</span>
              <span style={{ color: '#848E9C' }}>VOL</span><span>{formatCompact(hover.kline.volume)}</span>
            </div>
            {hover.markers.length > 0 && (
              <div className="mt-3 space-y-2">
                {expandedHoverMarkers(hover.markers).map((visual) => {
                  const marker = visual.marker;
                  const signalClose = resolveSignalCloseTime(marker);
                  const decisionClose = resolveDecisionCloseTime(marker);
                  return (
                  <div key={visual.id} className="border-t pt-2" style={{ borderColor: '#2B3139' }}>
                    <div className="font-semibold">{visual.label}</div>
                    <div style={{ color: '#848E9C' }}>
                      {visualKindLabel(visual.kind)} · {marker.level || marker.timeframe} · {marker.action || '--'}{marker.final_action ? ` -> ${marker.final_action}` : ''}
                    </div>
                    <div className="mt-1 grid grid-cols-[64px_1fr] gap-x-2 font-mono" style={{ color: '#848E9C' }}>
                      <span>结构</span><span>{formatFullTime(signalClose)}</span>
                      <span>决策</span><span>{decisionClose ? formatFullTime(decisionClose) : '缺少确认时间'}</span>
                      <span>年龄</span><span>{marker.age_candles ? `${marker.age_candles} 根` : '--'}</span>
                      <span>来源</span><span>{marker.source_layer || '--'}</span>
                      <span>状态</span><span>{marker.status || '--'}</span>
                      <span>原因</span><span>{marker.reason_code || marker.entry_invalidation_reason || '--'}</span>
                      <span>父级</span><span>{marker.parent_structure_key || marker.parent_signal_id || '--'}</span>
                      <span>触发</span><span>{marker.entry_trigger_id || '--'}</span>
                    </div>
                    {visual.pairOutOfRange && visual.pairCloseTime && (
                      <div className="mt-1" style={{ color: '#F0B90B' }}>配对时间 {formatFullTime(visual.pairCloseTime)} 不在当前图表范围内</div>
                    )}
                    {marker.reason && <div className="mt-1 break-words" style={{ color: '#B7BDC6' }}>{marker.reason}</div>}
                    <div className="mt-1 font-mono" style={{ color: '#848E9C' }}>{marker.lifecycle_key || marker.signal_id}</div>
                    <div className="mt-1 font-mono" style={{ color: '#848E9C' }}>{marker.signal_id}</div>
                  </div>
                  );
                })}
              </div>
            )}
          </div>
        )}
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2 text-[11px]" style={{ color: '#848E9C' }}>
        <LegendSwatch color="#0ECB81" label="B1/B2/B3 买点、开多/加多/减空/平空" />
        <LegendSwatch color="#F6465D" label="S1/S2/S3 卖点、开空/加空/减多/平多" />
        <LegendSwatch color="#F0B90B" label="金边为已执行" />
        <LegendSwatch color="#848E9C" label="灰色为拒绝/失败/跳过" />
      </div>
    </ChartShell>
  );
}

function ChartShell({ symbol, timeframe, meta, children }: { symbol: string; timeframe: string; meta: string; children: ReactNode }) {
  return (
    <div>
      <div className="flex flex-col gap-1 sm:flex-row sm:items-end sm:justify-between mb-3">
        <div>
          <div className="text-xs" style={{ color: '#848E9C' }}>{timeframe} 主交易K线</div>
          <div className="text-lg font-bold font-mono" style={{ color: '#EAECEF' }}>{symbol || '--'}</div>
        </div>
        <div className="text-xs" style={{ color: '#848E9C' }}>{meta}</div>
      </div>
      {children}
    </div>
  );
}

function MarkerGlyph({
  visual,
  x,
  y,
  placement,
  onMouseMove,
}: {
  visual: VisualSignalMarker;
  x: number;
  y: number;
  placement: 'buy' | 'sell';
  onMouseMove: (event: MouseEvent) => void;
}) {
  const marker = visual.marker;
  const label = visual.label;
  const tone = markerTone(marker);
  const fill = tone === 'buy' ? '#0ECB81' : tone === 'sell' ? '#F6465D' : '#848E9C';
  const textWidth = Math.max(34, label.length * 9 + 16);
  const status = (marker.status || '').toLowerCase();
  const stroke = visual.kind === 'cluster' || status === 'executed' ? '#F0B90B' : fill;
  const intent = resolveTradeIntent(marker);
  const title = [
    `${signalLabel(marker.signal_type)} ${tradeIntentLabel(intent)}`,
    markerStatusLabel(marker.status),
    marker.action,
    marker.final_action,
    marker.reason,
  ].filter(Boolean).join(' · ');
  if (visual.kind === 'cluster') {
    return (
      <g transform={`translate(${x}, ${y})`} onMouseMove={onMouseMove}>
        <title>{`${visual.clusterCount || 0} 条信号`}</title>
        <rect
          x={-textWidth / 2}
          y={-11}
          width={textWidth}
          height={22}
          rx={4}
          fill="#1E2329"
          stroke={stroke}
          strokeWidth={1.4}
        />
        <text x={0} y={4} textAnchor="middle" fill="#EAECEF" fontSize={10} fontWeight={800}>
          {label}
        </text>
      </g>
    );
  }
  return (
    <g transform={`translate(${x}, ${y})`} onMouseMove={onMouseMove}>
      <title>{title}</title>
      <path
        d={placement === 'buy' ? 'M -5 -10 L 5 -10 L 0 -16 Z' : 'M -5 10 L 5 10 L 0 16 Z'}
        fill={fill}
        opacity={0.95}
      />
      <rect
        x={-textWidth / 2}
        y={-10}
        width={textWidth}
        height={20}
        rx={3}
        fill={`${fill}22`}
        stroke={stroke}
        strokeWidth={status === 'executed' ? 1.6 : 1}
      />
      <circle cx={-textWidth / 2 + 8} cy={0} r={3} fill={fill} />
      <text x={-textWidth / 2 + 15} y={4} fill="#EAECEF" fontSize={10} fontWeight={700}>
        {label}
      </text>
    </g>
  );
}

function markerY(visual: VisualSignalMarker, kline: MarketKline, priceToY: (price: number) => number, index: number) {
  const placement = visual.placement;
  const offset = 22 + index * 24;
  const rawY = placement === 'buy'
    ? priceToY(kline.low) + offset
    : priceToY(kline.high) - offset;
  return Math.max(topPad + 10, Math.min(chartHeight - bottomPad - 8, rawY));
}

function markerStackIndex(markers: VisualSignalMarker[], markerIndex: number) {
  const placement = markers[markerIndex].placement;
  return markers
    .slice(0, markerIndex)
    .filter((marker) => marker.placement === placement)
    .length;
}

function expandedHoverMarkers(markers: VisualSignalMarker[]) {
  return markers.flatMap((marker) => marker.clusterItems ?? [marker]);
}

function LegendSwatch({ color, label }: { color: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      <span className="inline-block h-2.5 w-2.5 rounded-sm" style={{ background: color }} />
      {label}
    </span>
  );
}

function limitSourceLabel(source?: string) {
  switch (source) {
    case 'query':
      return 'query limit';
    case 'query_capped':
      return 'query limit已截断';
    case 'programmatic_history_depth':
      return 'history_depth';
    case 'programmatic_history_depth_capped':
      return 'history_depth已截断';
    case 'default':
      return '默认数量';
    default:
      return source || '';
  }
}

function visualKindLabel(kind: 'signal' | 'decision' | 'merged' | 'cluster') {
  switch (kind) {
    case 'signal':
      return '结构点';
    case 'decision':
      return '决策点';
    case 'merged':
      return '结构/决策';
    case 'cluster':
      return '信号簇';
    default:
      return '';
  }
}

function formatPrice(value: number) {
  const abs = Math.abs(value);
  if (abs >= 1000) return value.toFixed(2);
  if (abs >= 1) return value.toFixed(4);
  if (abs >= 0.01) return value.toFixed(6);
  return value.toFixed(8);
}

function formatCompact(value: number) {
  return new Intl.NumberFormat(undefined, { maximumFractionDigits: 2, notation: 'compact' }).format(value);
}

function formatShortTime(value: number) {
  const date = new Date(normalizeEpochMs(value));
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  const hour = String(date.getHours()).padStart(2, '0');
  const minute = String(date.getMinutes()).padStart(2, '0');
  return `${month}-${day} ${hour}:${minute}`;
}

function formatFullTime(value?: number) {
  if (!value) return '--';
  return new Date(normalizeEpochMs(value)).toLocaleString();
}
