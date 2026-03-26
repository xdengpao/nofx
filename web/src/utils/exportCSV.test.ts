import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';
import { generateTradeHistoryCSV, CSV_HEADERS } from './exportCSV';

// Feature: quant-trading-system, Property 48: CSV 导出字段完整性
// 对任意非空的 TradeOutcome 列表，exportTradeHistoryCSV 生成的 CSV 字符串应包含 14 列表头，
// 且数据行数等于输入列表长度
// 验证: 需求 13.13

/** fast-check 生成器：随机 TradeOutcome */
const symbolArb = fc.constantFrom('BTCUSDT', 'ETHUSDT', 'SOLUSDT', 'DOGEUSDT', 'XRPUSDT', 'ADAUSDT', 'BNBUSDT');
const durationArb = fc.constantFrom('1h 30m', '45m', '2h 15m', '30m', '5h 0m', '12h 45m');

const tradeOutcomeArb = fc.record({
  symbol: symbolArb,
  side: fc.constantFrom('long', 'short'),
  quantity: fc.double({ min: 0.001, max: 10000, noNaN: true }),
  leverage: fc.integer({ min: 1, max: 125 }),
  open_price: fc.double({ min: 0.0001, max: 100000, noNaN: true }),
  close_price: fc.double({ min: 0.0001, max: 100000, noNaN: true }),
  position_value: fc.double({ min: 1, max: 1000000, noNaN: true }),
  margin_used: fc.double({ min: 1, max: 100000, noNaN: true }),
  pn_l: fc.double({ min: -50000, max: 50000, noNaN: true }),
  pn_l_pct: fc.double({ min: -100, max: 1000, noNaN: true }),
  duration: durationArb,
  open_time: fc.date({ min: new Date('2020-01-01'), max: new Date('2026-12-31') }).filter((d) => !isNaN(d.getTime())).map((d) => d.toISOString()),
  close_time: fc.date({ min: new Date('2020-01-01'), max: new Date('2026-12-31') }).filter((d) => !isNaN(d.getTime())).map((d) => d.toISOString()),
  was_stop_loss: fc.boolean(),
});

describe('Property 48: CSV 导出字段完整性', () => {
  it('对任意非空 TradeOutcome 列表，CSV 应包含 14 列表头且数据行数等于输入长度', () => {
    fc.assert(
      fc.property(
        fc.array(tradeOutcomeArb, { minLength: 1, maxLength: 50 }),
        (trades) => {
          const csv = generateTradeHistoryCSV(trades);

          // CSV 不应为空
          expect(csv).not.toBe('');

          const lines = csv.split('\n');

          // 第一行是表头
          const headerLine = lines[0];
          const headerColumns = headerLine.split(',');
          expect(headerColumns).toHaveLength(14);

          // 表头内容应与 CSV_HEADERS 一致
          for (let i = 0; i < 14; i++) {
            expect(headerColumns[i]).toBe(CSV_HEADERS[i]);
          }

          // 数据行数应等于输入列表长度
          const dataLines = lines.slice(1);
          expect(dataLines).toHaveLength(trades.length);

          // 每个数据行也应有 14 列（考虑 CSV 转义后的解析）
          for (const line of dataLines) {
            // 简单计数：未被引号包裹的逗号数 = 列数 - 1
            let cols = 0;
            let inQuotes = false;
            for (const ch of line) {
              if (ch === '"') inQuotes = !inQuotes;
              if (ch === ',' && !inQuotes) cols++;
            }
            expect(cols).toBe(13); // 13 个逗号 = 14 列
          }
        },
      ),
      { numRuns: 100 },
    );
  });

  it('空列表应返回空字符串', () => {
    expect(generateTradeHistoryCSV([])).toBe('');
  });
});
