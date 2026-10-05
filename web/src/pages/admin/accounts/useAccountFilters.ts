import { useCallback, useEffect, useRef, useState } from 'react';
import { STORAGE_KEYS } from '../../../shared/storageKeys';
import { normalizeAccountFilters, type AccountFilters } from './accountFilters';

const STORAGE_KEY = STORAGE_KEYS.ui.adminAccountsFilters;

function readAccountFilters(): AccountFilters {
  try {
    const parsed: unknown = JSON.parse(window.localStorage.getItem(STORAGE_KEY) ?? 'null');
    if (parsed && typeof parsed === 'object' && 'version' in parsed && parsed.version === 1) {
      return normalizeAccountFilters(parsed);
    }
  } catch { /* Invalid or unavailable storage falls back to default filters. */ }
  return normalizeAccountFilters({});
}

function persistFilters(filters: AccountFilters) {
  try {
    window.localStorage.setItem(STORAGE_KEY, JSON.stringify(filters));
  } catch { /* In-memory filters remain usable when storage is blocked. */ }
}

type Update = Partial<AccountFilters> | ((current: AccountFilters) => Partial<AccountFilters>);

export function useAccountFilters() {
  const [filters, setFilters] = useState(readAccountFilters);
  const current = useRef(filters);
  const updateFilters = useCallback((update: Update) => {
    const next = normalizeAccountFilters({ ...current.current, ...(typeof update === 'function' ? update(current.current) : update) });
    if (JSON.stringify(next) === JSON.stringify(current.current)) return;
    current.current = next;
    setFilters(next);
    persistFilters(next);
  }, []);

  useEffect(() => {
    persistFilters(current.current);
  }, []);

  return { filters, updateFilters };
}
