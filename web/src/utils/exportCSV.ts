// 成交历史 CSV 导出工具函数

interface TradeOutcome {
  symbol: string;
  side: string;
  quantity: number;
  leverage: number;
  open_price: number;
  close_price: number;
  position_value: number;
  margin_used: number;
  pn_l: number;
  pn_l_pct: number;
  duration: string;
  open_time: string;
  close_time: string;
  was_stop_loss: boolean;
}

export const CSV_HEADERS = [
  'Symbol', 'Side', 'Open Time', 'Close Time',
  'Open Price', 'Close Price', 'Quantity', 'Leverage',
  'Position Value', 'Margin Used', 'PnL', 'PnL %',
  'Duration', 'Close Reason',
] as const;

/**
 * 生成成交历史 CSV 字符串（纯函数，可测试）
 * - 14 列表头 + 数据行
 * - 数据行按平仓时间降序排列
 * - 价格保留 4 位小数，盈亏保留 2 位小数
 */
export function generateTradeHistoryCSV(trades: TradeOutcome[]): string {
  if (!trades || trades.length === 0) return '';

  // 按平仓时间降序排列
  const sorted = [...trades].sort((a, b) => {
    const ta = a.close_time ? new Date(a.close_time).getTime() : 0;
    const tb = b.close_time ? new Date(b.close_time).getTime() : 0;
    return tb - ta;
  });

  const rows = sorted.map((t) => [
    t.symbol,
    t.side,
    t.open_time || '',
    t.close_time || '',
    t.open_price.toFixed(4),
    t.close_price.toFixed(4),
    t.quantity ? t.quantity.toFixed(4) : '0',
    String(t.leverage || 0),
    t.position_value ? t.position_value.toFixed(2) : '0',
    t.margin_used ? t.margin_used.toFixed(2) : '0',
    t.pn_l.toFixed(2),
    t.pn_l_pct.toFixed(2),
    t.duration || '',
    t.was_stop_loss ? 'Stop Loss' : 'Take Profit / Manual',
  ]);

  return [
    CSV_HEADERS.join(','),
    ...rows.map((row) => row.map(escapeCSV).join(',')),
  ].join('\n');
}

/**
 * 导出成交历史为 CSV 文件并触发浏览器下载
 */
export function exportTradeHistoryCSV(trades: TradeOutcome[], traderId: string): void {
  const csvContent = generateTradeHistoryCSV(trades);
  if (!csvContent) return;

  const BOM = '\uFEFF';
  const blob = new Blob([BOM + csvContent], { type: 'text/csv;charset=utf-8;' });
  const url = URL.createObjectURL(blob);

  const now = new Date();
  const dateStr = [
    now.getFullYear(),
    String(now.getMonth() + 1).padStart(2, '0'),
    String(now.getDate()).padStart(2, '0'),
  ].join('');
  const filename = `trade_history_${traderId}_${dateStr}.csv`;

  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  document.body.removeChild(link);
  URL.revokeObjectURL(url);
}

/** CSV 字段转义：包含逗号、引号或换行时用双引号包裹 */
function escapeCSV(value: string): string {
  if (value.includes(',') || value.includes('"') || value.includes('\n')) {
    return `"${value.replace(/"/g, '""')}"`;
  }
  return value;
}
