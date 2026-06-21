import { startTransition, useCallback, useEffect, useMemo, useRef, useState, type SetStateAction } from 'react';
import { useRouterState } from '@tanstack/react-router';

type UrlPrimitive = string | number | boolean | null | undefined;
type UrlUpdates = Record<string, UrlPrimitive>;

function getSearchString(searchStr?: string) {
  if (typeof window === 'undefined') return searchStr ?? '';
  return searchStr ?? window.location.search;
}

function readStoredString(storageKey: string | undefined) {
  if (!storageKey || typeof window === 'undefined') return null;

  try {
    return window.localStorage.getItem(storageKey);
  } catch {
    return null;
  }
}

function writeStoredString(storageKey: string | undefined, value: string) {
  if (!storageKey || typeof window === 'undefined') return;

  try {
    if (value) {
      window.localStorage.setItem(storageKey, value);
    } else {
      window.localStorage.removeItem(storageKey);
    }
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

  const setUrlValue = useCallback((nextValue: SetStateAction<string>) => {
    const resolved = typeof nextValue === 'function' ? nextValue(valueRef.current) : nextValue;
    valueRef.current = resolved;
    setValue(resolved);
    startTransition(() => {
      applySearchUpdates(pathname, {
        [key]: resolved === defaultValue ? '' : resolved,
      });
    });
  }, [defaultValue, key, pathname]);

  return [value, setUrlValue] as const;
}

export function usePersistentUrlQueryParam(key: string, storageKey: string, defaultValue = '') {
  const searchStr = useRouterState({ select: (state) => state.location.searchStr });
  const [value, setUrlValue] = useUrlQueryParam(key, defaultValue);
  const valueRef = useRef(value);
  const restoredRef = useRef(false);
  const hasUrlValue = useMemo(() => {
    const params = new URLSearchParams(getSearchString(searchStr));
    return params.has(key);
  }, [key, searchStr]);

  useEffect(() => {
    valueRef.current = value;
  }, [value]);

  useEffect(() => {
    if (restoredRef.current) return;
    restoredRef.current = true;

    if (hasUrlValue) {
      writeStoredString(storageKey, value);
      return;
    }

    const storedValue = readStoredString(storageKey);
    if (storedValue == null || storedValue === value) return;

    valueRef.current = storedValue;
    setUrlValue(storedValue);
  }, [hasUrlValue, setUrlValue, storageKey, value]);

  useEffect(() => {
    if (!restoredRef.current || !hasUrlValue) return;
    writeStoredString(storageKey, value);
  }, [hasUrlValue, storageKey, value]);

  const setPersistentUrlValue = useCallback((nextValue: SetStateAction<string>) => {
    const resolved = typeof nextValue === 'function' ? nextValue(valueRef.current) : nextValue;
    valueRef.current = resolved;
    writeStoredString(storageKey, resolved);
    setUrlValue(resolved);
  }, [setUrlValue, storageKey]);

  return [value, setPersistentUrlValue] as const;
}
