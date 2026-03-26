/**
 * 分页工具函数
 * 当成交历史记录数量超过 50 条时，启用分页显示，每页默认展示 20 条记录
 */

export interface PaginationResult<T> {
  /** 当前页的数据 */
  displayItems: T[];
  /** 总页数 */
  totalPages: number;
  /** 是否启用分页 */
  enablePagination: boolean;
}

const PAGINATION_THRESHOLD = 50;
const DEFAULT_PAGE_SIZE = 20;

/**
 * 对列表进行分页计算
 * - 记录数 ≤ 50 时不分页，直接返回全部记录
 * - 记录数 > 50 时启用分页，每页 pageSize 条
 */
export function paginate<T>(
  items: T[],
  currentPage: number,
  pageSize: number = DEFAULT_PAGE_SIZE,
): PaginationResult<T> {
  const totalRecords = items.length;
  const enablePagination = totalRecords > PAGINATION_THRESHOLD;

  if (!enablePagination) {
    return {
      displayItems: items,
      totalPages: 1,
      enablePagination: false,
    };
  }

  const totalPages = Math.ceil(totalRecords / pageSize);
  const safePage = Math.max(1, Math.min(currentPage, totalPages));
  const start = (safePage - 1) * pageSize;
  const end = start + pageSize;

  return {
    displayItems: items.slice(start, end),
    totalPages,
    enablePagination: true,
  };
}
