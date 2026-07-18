import { replaceEqualDeep } from '@tanstack/react-query';
import type { PagedData } from '../types';

type RowWithID = { id: number | string };

/**
 * React Query 默认按数组下标做结构共享。实时列表在顶部插入记录后，后续所有行都会错位，
 * 即使记录内容没有变化也会得到新对象，导致按行 memo 失效。
 *
 * 这里先按 ID 找回旧对象，再对同一条记录做深度结构共享；分页元数据仍沿用默认逻辑。
 */
export function sharePagedRowsByID<T extends RowWithID>(
  previous: PagedData<T> | undefined,
  next: PagedData<T>,
): PagedData<T> {
  if (!previous) return next;

  const sameOrder = previous.list.length === next.list.length
    && next.list.every((row, index) => previous.list[index]?.id === row.id);
  if (sameOrder) return replaceEqualDeep(previous, next);

  const previousRowsByID = new Map(previous.list.map((row) => [row.id, row]));
  const list = next.list.map((row) => {
    const previousRow = previousRowsByID.get(row.id);
    return previousRow ? replaceEqualDeep(previousRow, row) : row;
  });

  // 先共享列表之外的分页字段，再覆盖为按 ID 合并后的列表，避免默认的下标比较破坏行引用。
  const sharedContainer = replaceEqualDeep(previous, {
    ...next,
    list: previous.list,
  });

  return {
    ...sharedContainer,
    list,
  };
}

/** 适配 React Query 使用 unknown 数据定义的 structuralSharing 回调签名。 */
export function createPagedRowsStructuralSharing<T extends RowWithID>() {
  return (previous: unknown | undefined, next: unknown): PagedData<T> => (
    sharePagedRowsByID(
      previous as PagedData<T> | undefined,
      next as PagedData<T>,
    )
  );
}
