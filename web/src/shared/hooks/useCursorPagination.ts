import { useCallback, useState } from 'react';
import { usePagination } from './usePagination';
import type { UsagePaginationInfo } from '../types';

export function useCursorPagination(defaultPageSize = 20, storageKey?: string) {
  const { page, setPage: setBasePage, pageSize, setPageSize: setBasePageSize } = usePagination(defaultPageSize, storageKey);
  const [cursors, setCursors] = useState<Record<number, number | undefined>>({});
  const [activeSnapshot, setActiveSnapshot] = useState<UsagePaginationInfo | null>(null);
  const beforeId = page > 1 && !activeSnapshot ? cursors[page] : undefined;

  const resetCursorPagination = useCallback(() => {
    setCursors({});
    setActiveSnapshot(null);
    setBasePage(1);
  }, [setBasePage]);

  const setPage = useCallback((nextPage: number, nextCursor?: number | null, availableSnapshot?: UsagePaginationInfo) => {
    if (!Number.isSafeInteger(nextPage)) return;
    if (nextPage <= 1) {
      setActiveSnapshot(null);
      setCursors({});
      setBasePage(1);
      return;
    }

    const snapshot = activeSnapshot ?? availableSnapshot;
    if (snapshot?.status === 'ready' && snapshot.snapshot) {
      setActiveSnapshot(snapshot);
      setBasePage(Math.min(nextPage, Math.max(1, Math.ceil(snapshot.total / pageSize))));
      return;
    }

    if (nextPage === page + 1) {
      if (nextCursor == null || nextCursor <= 0) return;
      setCursors((current) => ({ ...current, [nextPage]: nextCursor }));
      setBasePage(nextPage);
      return;
    }

    if (nextPage < page || cursors[nextPage] != null) {
      setBasePage(nextPage);
    }
  }, [activeSnapshot, cursors, page, pageSize, setBasePage]);

  const setPageSize = useCallback((nextPageSize: number) => {
    setCursors({});
    setActiveSnapshot(null);
    setBasePage(1);
    setBasePageSize(nextPageSize);
  }, [setBasePage, setBasePageSize]);

  return {
    activeSnapshot,
    beforeId,
    page,
    pageSize,
    resetCursorPagination,
    setPage,
    setPageSize,
  };
}
