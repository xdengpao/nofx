import { describe, it, expect } from 'vitest';
import * as fc from 'fast-check';
import { paginate } from './pagination';

// Feature: quant-trading-system, Property 49: 分页正确性
// 对任意长度为 N（N > 50）的成交记录列表和页码 P（1 ≤ P ≤ ceil(N/20)），
// 分页后当前页应包含最多 20 条记录，且所有页的记录总数等于 N
// 验证: 需求 13.15

describe('Property 49: 分页正确性', () => {
  const PAGE_SIZE = 20;

  it('对任意 N > 50 的列表和有效页码，当前页最多 20 条且所有页总数等于 N', () => {
    fc.assert(
      fc.property(
        // N ∈ [51, 500]
        fc.integer({ min: 51, max: 500 }),
        (n) => {
          const items = Array.from({ length: n }, (_, i) => i);
          const totalPages = Math.ceil(n / PAGE_SIZE);

          // 验证所有页的记录总数等于 N
          let totalItems = 0;
          for (let page = 1; page <= totalPages; page++) {
            const result = paginate(items, page, PAGE_SIZE);

            // 应启用分页
            expect(result.enablePagination).toBe(true);
            expect(result.totalPages).toBe(totalPages);

            // 当前页最多 20 条
            expect(result.displayItems.length).toBeLessThanOrEqual(PAGE_SIZE);
            // 当前页至少 1 条
            expect(result.displayItems.length).toBeGreaterThanOrEqual(1);

            totalItems += result.displayItems.length;
          }

          // 所有页的记录总数等于 N
          expect(totalItems).toBe(n);
        },
      ),
      { numRuns: 100 },
    );
  });

  it('对任意有效页码 P，当前页包含正确的切片数据', () => {
    fc.assert(
      fc.property(
        fc.integer({ min: 51, max: 300 }),
        fc.integer({ min: 1, max: 15 }),
        (n, rawPage) => {
          const items = Array.from({ length: n }, (_, i) => i);
          const totalPages = Math.ceil(n / PAGE_SIZE);
          const page = Math.min(rawPage, totalPages);

          const result = paginate(items, page, PAGE_SIZE);
          const expectedStart = (page - 1) * PAGE_SIZE;
          const expectedEnd = Math.min(page * PAGE_SIZE, n);
          const expectedSlice = items.slice(expectedStart, expectedEnd);

          expect(result.displayItems).toEqual(expectedSlice);
        },
      ),
      { numRuns: 100 },
    );
  });

  it('记录数 ≤ 50 时不启用分页，返回全部记录', () => {
    fc.assert(
      fc.property(
        fc.integer({ min: 0, max: 50 }),
        (n) => {
          const items = Array.from({ length: n }, (_, i) => i);
          const result = paginate(items, 1, PAGE_SIZE);

          expect(result.enablePagination).toBe(false);
          expect(result.totalPages).toBe(1);
          expect(result.displayItems).toEqual(items);
          expect(result.displayItems.length).toBe(n);
        },
      ),
      { numRuns: 100 },
    );
  });

  it('最后一页的记录数等于 N mod pageSize（非整除时）', () => {
    fc.assert(
      fc.property(
        fc.integer({ min: 51, max: 500 }).filter((n) => n % PAGE_SIZE !== 0),
        (n) => {
          const items = Array.from({ length: n }, (_, i) => i);
          const totalPages = Math.ceil(n / PAGE_SIZE);
          const result = paginate(items, totalPages, PAGE_SIZE);

          expect(result.displayItems.length).toBe(n % PAGE_SIZE);
        },
      ),
      { numRuns: 100 },
    );
  });

  it('各页数据互不重叠且完整覆盖', () => {
    fc.assert(
      fc.property(
        fc.integer({ min: 51, max: 300 }),
        (n) => {
          const items = Array.from({ length: n }, (_, i) => i);
          const totalPages = Math.ceil(n / PAGE_SIZE);

          const allCollected: number[] = [];
          for (let page = 1; page <= totalPages; page++) {
            const result = paginate(items, page, PAGE_SIZE);
            allCollected.push(...result.displayItems);
          }

          // 拼接后应与原始数组完全一致（顺序、内容）
          expect(allCollected).toEqual(items);
        },
      ),
      { numRuns: 100 },
    );
  });
});
