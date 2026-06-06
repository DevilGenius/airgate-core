import { startTransition, useCallback, useEffect, useMemo, useRef, useState, type SetStateAction } from 'react';
import { useRouterState } from '@tanstack/react-router';
import { storagePageSizeKey } from '../storageKeys';
import { normalizePaginationPageSize } from '../utils/pagination';

type UrlPrimitive = string | number | boolean | null | undefined;
type UrlUpdates = Record<string, UrlPrimitive>;

function getSearchString(searchStr?: string) {
  if (typeof window === 'undefined') return searchStr ?? '';
  return searchStr ?? window.location.search;
}

function readPositiveInt(params: URLSearchParams, key: string, fallback: number) {
  const parsed = Number(params.get(key));
  return Number.isFinite(parsed) && parsed > 0 ? Math.floor(parsed) : fallback;
}

function readStoredPageSize(storageKey: string | undefined, fallback: number) {
  if (!storageKey || typeof window === 'undefined') return fallback;

  try {
    const raw = window.localStorage.getItem(storagePageSizeKey(storageKey));
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
    // Storage can be unavailable; URL state remains authoritative for this session.
  }
}

function applySearchUpdates(pathname: string, updates: UrlUpdates) {
  if (typeof window === 'undefined') return;

  const params = new URLSearchParams(window.location.search);
  Object.entries(updates).forEach(([key, value]) => {
    if (value == null || value === '' || value === false) {
      params.delete(key);
      return;
    }
    params.set(key, String(value));
  });

  const nextSearch = params.toString();
  const nextUrl = `${pathname}${nextSearch ? `?${nextSearch}` : ''}${window.location.hash}`;
  if (nextUrl === `${window.location.pathname}${window.location.search}${window.location.hash}`) return;

  window.history.replaceState(window.history.state, '', nextUrl);
  window.dispatchEvent(new PopStateEvent('popstate', { state: window.history.state }));
}

export function useUrlQueryParam(key: string, defaultValue = '') {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const searchStr = useRouterState({ select: (state) => state.location.searchStr });
  const valueFromUrl = useMemo(() => {
    const params = new URLSearchParams(getSearchString(searchStr));
    return params.get(key) ?? defaultValue;
  }, [defaultValue, key, searchStr]);
  const [value, setValue] = useState(valueFromUrl);
  const valueRef = useRef(valueFromUrl);

  useEffect(() => {
    valueRef.current = valueFromUrl;
    setValue(valueFromUrl);
  }, [valueFromUrl]);

  const setUrlValue = useCallback((nextValue: SetStateAction<string>, options?: { resetPage?: boolean }) => {
    const resolved = typeof nextValue === 'function' ? nextValue(valueRef.current) : nextValue;
    valueRef.current = resolved;
    setValue(resolved);
    startTransition(() => {
      applySearchUpdates(pathname, {
        [key]: resolved === defaultValue ? '' : resolved,
        ...(options?.resetPage === false ? {} : { page: null }),
      });
    });
  }, [defaultValue, key, pathname]);

  return [value, setUrlValue] as const;
}

export function useUrlPagination(defaultPageSize = 20, storageKey?: string) {
  const pathname = useRouterState({ select: (state) => state.location.pathname });
  const searchStr = useRouterState({ select: (state) => state.location.searchStr });
  const safeDefaultPageSize = normalizePaginationPageSize(defaultPageSize);
  const snapshot = useMemo(() => {
    const params = new URLSearchParams(getSearchString(searchStr));
    const storedPageSize = readStoredPageSize(storageKey, safeDefaultPageSize);
    return {
      page: readPositiveInt(params, 'page', 1),
      pageSize: normalizePaginationPageSize(params.get('page_size'), storedPageSize),
    };
  }, [safeDefaultPageSize, searchStr, storageKey]);

  const [page, setPageState] = useState(snapshot.page);
  const [pageSize, setPageSizeState] = useState(snapshot.pageSize);
  const pageRef = useRef(snapshot.page);

  useEffect(() => {
    pageRef.current = snapshot.page;
    startTransition(() => {
      setPageState(snapshot.page);
      setPageSizeState(snapshot.pageSize);
    });
  }, [snapshot.page, snapshot.pageSize]);

  const setPage = useCallback((nextPage: SetStateAction<number>) => {
    const resolved = typeof nextPage === 'function' ? nextPage(pageRef.current) : nextPage;
    const safePage = Number.isFinite(resolved) && resolved > 1 ? Math.floor(resolved) : 1;
    pageRef.current = safePage;
    setPageState(safePage);
    startTransition(() => {
      applySearchUpdates(pathname, { page: safePage > 1 ? safePage : null });
    });
  }, [pathname]);

  const setPageSize = useCallback((nextPageSize: number) => {
    const safePageSize = normalizePaginationPageSize(nextPageSize, safeDefaultPageSize);
    writeStoredPageSize(storageKey, safePageSize);
    pageRef.current = 1;
    startTransition(() => {
      setPageState(1);
      setPageSizeState(safePageSize);
      applySearchUpdates(pathname, {
        page: null,
        page_size: safePageSize === safeDefaultPageSize ? null : safePageSize,
      });
    });
  }, [pathname, safeDefaultPageSize, storageKey]);

  return { page, pageSize, setPage, setPageSize };
}
