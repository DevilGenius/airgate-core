export const DEFAULT_PAGINATION_PAGE_SIZE_OPTIONS = [20, 50, 100] as const;
export const DEFAULT_PAGINATION_PAGE_SIZE = DEFAULT_PAGINATION_PAGE_SIZE_OPTIONS[0];

export type PaginationItem = number | '...';

export function normalizePaginationPageSize(
  value: unknown,
  fallback: number = DEFAULT_PAGINATION_PAGE_SIZE,
  options: readonly number[] = DEFAULT_PAGINATION_PAGE_SIZE_OPTIONS,
): number {
  const safeFallback = options.includes(fallback) ? fallback : DEFAULT_PAGINATION_PAGE_SIZE;
  const parsed = typeof value === 'number' ? value : Number(value);
  if (!Number.isFinite(parsed)) return safeFallback;

  const size = Math.floor(parsed);
  return options.includes(size) ? size : safeFallback;
}

export function getTotalPages(total: number, pageSize: number): number {
  return Math.max(1, Math.ceil(total / pageSize));
}

export function getPaginationItems(current: number, total: number): PaginationItem[] {
  if (total <= 7) return Array.from({ length: total }, (_, index) => index + 1);
  // Always occupy seven slots so the navigation controls do not move on paging.
  if (current <= 4) return [1, 2, 3, 4, 5, '...', total];
  if (current >= total - 3) return [1, '...', total - 4, total - 3, total - 2, total - 1, total];
  return [1, '...', current - 1, current, current + 1, '...', total];
}
