import { act, renderHook } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { useCursorPagination } from './useCursorPagination';
import type { UsagePaginationInfo } from '../types';

const snapshot: UsagePaginationInfo = { status: 'ready', snapshot: 'view-one', total: 5_851_096 };

describe('usage numbered pagination', () => {
  it('jumps to an unvisited deep page and pins the same view across navigation', () => {
    const { result } = renderHook(() => useCursorPagination(20));
    act(() => result.current.setPage(150001, undefined, snapshot));
    expect(result.current.page).toBe(150001);
    expect(result.current.beforeId).toBeUndefined();
    expect(result.current.activeSnapshot?.snapshot).toBe('view-one');
    act(() => result.current.setPage(250000, undefined, { ...snapshot, snapshot: 'view-two' }));
    expect(result.current.page).toBe(250000);
    expect(result.current.activeSnapshot?.snapshot).toBe('view-one');
    act(() => result.current.setPage(1));
    expect(result.current.page).toBe(1);
    expect(result.current.activeSnapshot).toBeNull();
  });

  it('keeps cursor navigation usable while page totals are being prepared', () => {
    const { result } = renderHook(() => useCursorPagination(20));
    act(() => result.current.setPage(500));
    expect(result.current.page).toBe(1);
    act(() => result.current.setPage(2, 90));
    expect(result.current.page).toBe(2);
    expect(result.current.beforeId).toBe(90);
    act(() => result.current.setPage(100, undefined, snapshot));
    expect(result.current.page).toBe(100);
    expect(result.current.beforeId).toBeUndefined();
  });

  it('clears pinned views and cursors for filters and page size changes', () => {
    const { result } = renderHook(() => useCursorPagination(20));
    act(() => result.current.setPage(150001, undefined, snapshot));
    act(() => result.current.setPageSize(100));
    expect(result.current.page).toBe(1);
    expect(result.current.pageSize).toBe(100);
    expect(result.current.activeSnapshot).toBeNull();
    act(() => result.current.setPage(2, undefined, snapshot));
    act(() => result.current.resetCursorPagination());
    expect(result.current.page).toBe(1);
    expect(result.current.activeSnapshot).toBeNull();
    act(() => result.current.setPage(Number.NaN, undefined, snapshot));
    expect(result.current.page).toBe(1);
  });
});
