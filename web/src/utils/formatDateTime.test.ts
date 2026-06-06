import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';
import { formatDateTime } from './formatDateTime';

// Feature: quant-trading-system, Property 50: 时间格式化一致性
// 对任意有效的 ISO 8601 时间字符串，formatDateTime 应产生长度为 19 的字符串，
// 包含 `-`、空格和 `:` 分隔符，格式为 `YYYY-MM-DD HH:mm:ss`
// 验证: 需求 13.12

describe('Property 50: 时间格式化一致性', () => {
  it('对任意有效 ISO 8601 时间字符串，输出格式为 YYYY-MM-DD HH:mm:ss', () => {
    fc.assert(
      fc.property(
        fc.date({
          min: new Date('1970-01-01T00:00:00Z'),
          max: new Date('2099-12-31T23:59:59Z'),
        }).filter((date) => !Number.isNaN(date.getTime())),
        (date: Date) => {
          const iso = date.toISOString();
          const result = formatDateTime(iso);

          // 长度应为 19
          expect(result).toHaveLength(19);

          // 格式匹配 YYYY-MM-DD HH:mm:ss
          expect(result).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);

          // 分隔符位置正确
          expect(result[4]).toBe('-');
          expect(result[7]).toBe('-');
          expect(result[10]).toBe(' ');
          expect(result[13]).toBe(':');
          expect(result[16]).toBe(':');
        },
      ),
      { numRuns: 200 },
    );
  });

  it('对无效输入返回 "-"', () => {
    fc.assert(
      fc.property(
        fc.oneof(
          fc.constant(undefined as string | undefined),
          fc.constant(''),
          fc.constant('not-a-date'),
          fc.constant('abc123'),
        ),
        (input: string | undefined) => {
          expect(formatDateTime(input)).toBe('-');
        },
      ),
      { numRuns: 100 },
    );
  });
});
