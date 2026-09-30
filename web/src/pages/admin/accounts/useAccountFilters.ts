import { useCallback, useEffect, useRef, useState } from 'react';
import { STORAGE_KEYS } from '../../../shared/storageKeys';
import { UNGROUPED_GROUP_FILTER } from './accountFilterConstants';
import { normalizeAccountFilters, parseAccountPlanFilterIds, type AccountFilters } from './accountFilters';

const STORAGE_KEY = STORAGE_KEYS.ui.adminAccountsFilters;
const LEGACY_KEYS = ['q', 'platform', 'state', 'type', 'group', 'proxy'] as const;
const split = (value: string) => value.split(',').map((part) => part.trim()).filter(Boolean);

function readStorage(key: string): string | null {
  try { return window.localStorage.getItem(key); } catch { return null; }
}

function parseSaved(value: string | null): AccountFilters | undefined {
  if (!value) return undefined;
  try {
    const parsed: unknown = JSON.parse(value);
    if (!parsed || typeof parsed !== 'object' || !('version' in parsed) || parsed.version !== 1) return undefined;
    return normalizeAccountFilters(parsed);
  } catch { return undefined; }
}

export function readAccountFilters(search: string): AccountFilters {
  const params = new URLSearchParams(search);
  const fromUrl = parseSaved(params.get('filters'));
  if (fromUrl) return fromUrl;
  const saved = parseSaved(readStorage(STORAGE_KEY));
  const next = saved ?? normalizeAccountFilters({});
  // Consume old bookmarks and per-field storage once, then persist one schema.
  for (const key of LEGACY_KEYS) {
    const value = params.get(key) ?? (saved ? null : readStorage(`${STORAGE_KEY}:${key}`));
    if (value == null) continue;
    const values = split(value);
    switch (key) {
      case 'q': next.keyword = value; break;
      case 'platform': next.platforms = values; break;
      case 'state': next.states = values; break;
      case 'type':
        next.accountTypes = values.filter((item) => !item.startsWith('oauth_plan:'));
        next.plans = parseAccountPlanFilterIds(values);
        break;
      case 'group':
        next.groupIds = values.filter((item) => item !== UNGROUPED_GROUP_FILTER).map(Number);
        next.ungrouped = values.includes(UNGROUPED_GROUP_FILTER);
        break;
      case 'proxy': next.proxyIds = values.map(Number); break;
    }
  }
  return normalizeAccountFilters(next);
}

function persistFilters(filters: AccountFilters, pathname: string) {
  const serialized = JSON.stringify(filters);
  try {
    window.localStorage.setItem(STORAGE_KEY, serialized);
    for (const key of LEGACY_KEYS) window.localStorage.removeItem(`${STORAGE_KEY}:${key}`);
  } catch { /* In-memory and URL filters remain usable when storage is blocked. */ }
  if (window.location.pathname !== pathname) return;
  const params = new URLSearchParams(window.location.search);
  for (const key of LEGACY_KEYS) params.delete(key);
  params.set('filters', serialized);
  const next = `${pathname}?${params}${window.location.hash}`;
  if (next === `${pathname}${window.location.search}${window.location.hash}`) return;
  window.history.replaceState(window.history.state, '', next);
  window.dispatchEvent(new PopStateEvent('popstate', { state: window.history.state }));
}

type Update = Partial<AccountFilters> | ((current: AccountFilters) => Partial<AccountFilters>);

export function useAccountFilters() {
  const [filters, setFilters] = useState(() => readAccountFilters(window.location.search));
  const current = useRef(filters);
  const pathname = useRef(window.location.pathname);
  const updateFilters = useCallback((update: Update) => {
    const next = normalizeAccountFilters({ ...current.current, ...(typeof update === 'function' ? update(current.current) : update) });
    if (JSON.stringify(next) === JSON.stringify(current.current)) return;
    current.current = next;
    setFilters(next);
    persistFilters(next, pathname.current);
  }, []);

  useEffect(() => {
    persistFilters(current.current, pathname.current);
    const restore = () => {
      if (window.location.pathname !== pathname.current) return;
      const next = readAccountFilters(window.location.search);
      if (JSON.stringify(next) === JSON.stringify(current.current)) return;
      current.current = next;
      setFilters(next);
      persistFilters(next, pathname.current);
    };
    window.addEventListener('popstate', restore);
    return () => window.removeEventListener('popstate', restore);
  }, []);

  return { filters, updateFilters };
}
