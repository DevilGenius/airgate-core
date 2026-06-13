export function totalForCursorPage(page: number, pageSize: number, rows: number, hasMore?: boolean): number {
  return Math.max(0, (page - 1) * pageSize + rows + (hasMore ? 1 : 0));
}
