import { startTransition, useCallback, useEffect, useState, type SetStateAction } from 'react';
import { storagePageSizeKey } from '../storageKeys';
import { normalizePaginationPageSize } from '../utils/pagination';

function readStoredPageSize(storageKey: string | undefined, fallback: number) {
  if (!storageKey || typeof window === 'undefined') return fallback;

  try {
    const raw = window.localStorage.getItem(storagePageSizeKey(storageKey));
    if (raw == null) return fallback;
    return normalizePaginationPageSize(raw, fallback);
  } catch {
    return fallback;
  }
}

function writeStoredPageSize(storageKey: string | undefined, pageSize: number) {
  if (!storageKey || typeof window === 'undefined') return;

  try {
    window.localStorage.setItem(storagePageSizeKey(storageKey), String(pageSize));
  } catch {
    // localStorage may be unavailable in private mode; pagination should still work.
  }
}

export function usePagination(defaultPageSize = 20, storageKey?: string) {
  const safeDefaultPageSize = normalizePaginationPageSize(defaultPageSize);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(() => readStoredPageSize(storageKey, safeDefaultPageSize));

  useEffect(() => {
    startTransition(() => {
      setPageSize(readStoredPageSize(storageKey, safeDefaultPageSize));
      setPage(1);
    });
  }, [safeDefaultPageSize, storageKey]);

  const handlePageChange = useCallback((nextPage: SetStateAction<number>) => {
    startTransition(() => {
      setPage(nextPage);
    });
  }, []);

  const handlePageSizeChange = useCallback((size: number) => {
    const safePageSize = normalizePaginationPageSize(size, safeDefaultPageSize);
    writeStoredPageSize(storageKey, safePageSize);
    startTransition(() => {
      setPageSize(safePageSize);
      setPage(1);
    });
  }, [safeDefaultPageSize, storageKey]);

  return { page, setPage: handlePageChange, pageSize, setPageSize: handlePageSizeChange };
}
