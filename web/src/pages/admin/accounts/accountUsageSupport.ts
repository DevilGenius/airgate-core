import { useSyncExternalStore } from 'react';

export type AccountUsageTodayStats = { requests: number; tokens: number; account_cost: number; user_cost: number };
export type AccountUsageCredits = { balance: number; unlimited: boolean };
export type AccountUsageWindow = {
  key?: string;
  label: string;
  display_label?: string;
  slot?: string;
  group?: string;
  sort_order?: number;
  used_percent: number;
  reset_at?: string;
  reset_after_seconds?: number;
  reset_seconds?: number;
};
export type AccountUsageInfo = {
  windows?: AccountUsageWindow[];
  credits?: AccountUsageCredits | null;
  today_stats?: AccountUsageTodayStats | null;
  updated_at?: string;
};
export type AccountUsageData = { accounts?: Record<string, AccountUsageInfo>; refreshing?: boolean };
export type CachedUsageWindow = {
  resetAtMs: number;
  usedPercent: number;
  window: AccountUsageWindow;
};
export type AccountUsageWindowCache = Map<string, CachedUsageWindow>;
export type MergedAccountUsageData = {
  cache: AccountUsageWindowCache;
  data: AccountUsageData | undefined;
};

function getUsageWindowIdentity(window: AccountUsageWindow) {
  const normalized = normalizeUsageWindow(window);
  const group = normalized.group?.trim();
  const slot = normalizeUsageWindowSortToken(normalized.slot);
  if (group || slot) return `${group || 'base'}:${slot || normalized.display_label?.trim() || normalized.label.trim()}`;
  const key = normalized.key?.trim();
  if (key) return key;
  return normalized.label.trim();
}

function getUsageWindowCacheKey(accountId: string, window: AccountUsageWindow) {
  return `${accountId}:${getUsageWindowIdentity(window)}`;
}

function getUsageWindowResetAtMs(window: AccountUsageWindow, now: number) {
  if (window.reset_at) {
    const parsed = Date.parse(window.reset_at);
    if (Number.isFinite(parsed) && parsed > now) return parsed;
  }
  const resetSeconds = Number(window.reset_seconds ?? 0);
  if (resetSeconds > 0) return now + resetSeconds * 1000;
  const resetAfterSeconds = Number(window.reset_after_seconds ?? 0);
  if (resetAfterSeconds > 0) return now + resetAfterSeconds * 1000;
  return 0;
}

function getUsageWindowUsedPercent(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) return parsed;
  }
  return undefined;
}

function normalizeUsageWindowSortToken(value?: string) {
  return value?.trim().toLowerCase().replace(/_/g, '-') || '';
}

function inferUsageWindowSlot(window: AccountUsageWindow) {
  const slot = normalizeUsageWindowSortToken(window.slot);
  if (slot) return slot;

  const key = normalizeUsageWindowSortToken(window.key);
  const label = normalizeUsageWindowSortToken(window.label);
  if (key === '5h' || key.includes(':5h') || key.startsWith('5h-') || label.startsWith('5h')) {
    return '5h';
  }
  if (key === '7d' || key.includes(':7d') || key.startsWith('7d-') || label.startsWith('7d')) {
    return '7d';
  }
  if (key === 'monthly' || key.includes('monthly') || label.includes('monthly')) {
    return 'monthly';
  }
  if (key) return key;
  return label.split(/\s+/)[0] || '';
}

function usageWindowGroupSlug(value: string) {
  return value.trim().toLowerCase().replace(/_/g, '-').replace(/\s+/g, '-');
}

function usageWindowLabelSuffix(label: string, slot: string) {
  const parts = label.trim().split(/\s+/);
  if (parts.length <= 1 || normalizeUsageWindowSortToken(parts[0]) !== slot) return '';
  return parts.slice(1).join(' ').trim();
}

function inferUsageWindowGroup(window: AccountUsageWindow, slot: string) {
  const group = window.group?.trim();
  if (group) return group;

  const key = window.key?.trim() || '';
  if (key.startsWith('model:')) {
    const rest = key.slice('model:'.length);
    const prefix = `${slot}:`;
    const suffix = `:${slot}`;
    if (slot && rest.startsWith(prefix)) return `model:${rest.slice(prefix.length)}`;
    if (slot && rest.endsWith(suffix)) return `model:${rest.slice(0, -suffix.length)}`;
    return key.replace(/^model::/, 'model:');
  }

  const suffix = usageWindowLabelSuffix(window.label || '', slot);
  if (suffix) return `model:${usageWindowGroupSlug(suffix)}`;

  const normalizedKey = normalizeUsageWindowSortToken(key);
  if (normalizedKey.startsWith('5h-') || normalizedKey.startsWith('7d-')) {
    const suffixStart = normalizedKey.indexOf('-') + 1;
    const keySuffix = normalizedKey.slice(suffixStart);
    if (keySuffix) return `model:${usageWindowGroupSlug(keySuffix)}`;
  }

  return 'base';
}

function inferUsageWindowDisplayLabel(window: AccountUsageWindow, slot: string) {
  const displayLabel = window.display_label?.trim();
  if (displayLabel) return displayLabel;

  const label = window.label?.trim() || '';
  if (slot === 'monthly' && label.toLowerCase().startsWith('cr ')) {
    return 'Cr';
  }
  return slot || window.key?.trim() || label;
}

function inferUsageWindowKey(window: AccountUsageWindow, group: string, slot: string, label: string) {
  const key = window.key?.trim();
  if (key) return key;
  if (!slot) return label.trim();
  if (!group || group === 'base') return slot;
  return `${group}:${slot}`;
}

function normalizeUsageWindow(window: AccountUsageWindow): AccountUsageWindow {
  const slot = inferUsageWindowSlot(window);
  const group = inferUsageWindowGroup(window, slot);
  const displayLabel = inferUsageWindowDisplayLabel(window, slot);
  const label = window.label?.trim() || displayLabel;
  return {
    ...window,
    key: inferUsageWindowKey(window, group, slot, label),
    label,
    display_label: displayLabel,
    slot,
    group,
  };
}

function usageWindowSlotSortRank(window: AccountUsageWindow) {
  switch (inferUsageWindowSlot(window)) {
    case '5h':
      return 0;
    case '7d':
      return 1;
    case 'monthly':
      return 2;
    default:
      return 3;
  }
}

function usageWindowSortOrder(window: AccountUsageWindow) {
  const value = Number(window.sort_order);
  return Number.isFinite(value) && value !== 0 ? value : Number.MAX_SAFE_INTEGER;
}

function sortUsageWindows(windows: AccountUsageWindow[]) {
  return [...windows].sort((left, right) => {
    const leftOrder = usageWindowSortOrder(left);
    const rightOrder = usageWindowSortOrder(right);
    if (leftOrder !== rightOrder) return leftOrder - rightOrder;

    const leftSlot = usageWindowSlotSortRank(left);
    const rightSlot = usageWindowSlotSortRank(right);
    if (leftSlot !== rightSlot) return leftSlot - rightSlot;

    const leftGroup = inferUsageWindowGroup(left, inferUsageWindowSlot(left));
    const rightGroup = inferUsageWindowGroup(right, inferUsageWindowSlot(right));
    if (leftGroup !== rightGroup) return leftGroup.localeCompare(rightGroup);

    const leftKey = left.key || '';
    const rightKey = right.key || '';
    if (leftKey !== rightKey) return leftKey.localeCompare(rightKey);

    return (left.label || '').localeCompare(right.label || '');
  });
}

function windowWithCachedReset(window: AccountUsageWindow, resetAtMs: number, now: number): AccountUsageWindow {
  if (resetAtMs <= now) {
    return {
      ...window,
      reset_seconds: 0,
    };
  }
  return {
    ...window,
    reset_at: new Date(resetAtMs).toISOString(),
    reset_seconds: Math.max(0, Math.ceil((resetAtMs - now) / 1000)),
  };
}

export function mergeCachedUsageWindows(
  data: AccountUsageData | undefined,
  previousCache: AccountUsageWindowCache,
  now = Date.now(),
): MergedAccountUsageData {
  const cache: AccountUsageWindowCache = new Map(previousCache);
  if (!data?.accounts) return { cache, data };

  const accounts: Record<string, AccountUsageInfo> = {};
  const liveCacheKeys = new Set<string>();
  const cachedWindowsByAccount = new Map<string, Map<string, CachedUsageWindow>>();

  for (const entry of Array.from(cache.entries())) {
    const accountIdEnd = entry[0].indexOf(':');
    if (accountIdEnd <= 0) continue;
    const accountId = entry[0].slice(0, accountIdEnd);
    const normalizedCached: CachedUsageWindow = {
      ...entry[1],
      window: normalizeUsageWindow(entry[1].window),
    };
    const cacheKey = getUsageWindowCacheKey(accountId, normalizedCached.window);
    if (cacheKey !== entry[0]) {
      cache.delete(entry[0]);
      cache.set(cacheKey, normalizedCached);
    }
    let cachedWindows = cachedWindowsByAccount.get(accountId);
    if (!cachedWindows) {
      cachedWindows = new Map<string, CachedUsageWindow>();
      cachedWindowsByAccount.set(accountId, cachedWindows);
    }
    const existing = cachedWindows.get(cacheKey);
    if (!existing || normalizedCached.resetAtMs > existing.resetAtMs) {
      cachedWindows.set(cacheKey, normalizedCached);
    }
  }

  for (const [accountId, usage] of Object.entries(data.accounts)) {
    const rawWindows = Array.isArray(usage?.windows) ? usage.windows : [];
    const mergedWindows: AccountUsageWindow[] = [];

    for (const rawWindow of rawWindows) {
      const window = normalizeUsageWindow(rawWindow);
      const cacheKey = getUsageWindowCacheKey(accountId, window);
      const resetAtMs = getUsageWindowResetAtMs(window, now);
      const cached = cache.get(cacheKey);
      const usedPercent = getUsageWindowUsedPercent(window.used_percent)
        ?? cached?.usedPercent
        ?? 0;
      const effectiveResetAtMs = resetAtMs > now
        ? resetAtMs
        : cached && cached.resetAtMs > now
          ? cached.resetAtMs
          : 0;
      const windowWithCachedUsage = {
        ...window,
        used_percent: usedPercent,
      };
      const nextWindow = effectiveResetAtMs > now
        ? windowWithCachedReset(windowWithCachedUsage, effectiveResetAtMs, now)
        : {
            ...windowWithCachedUsage,
            reset_seconds: Number(window.reset_seconds ?? 0),
          };

      cache.set(cacheKey, {
        resetAtMs: effectiveResetAtMs > now ? effectiveResetAtMs : 0,
        usedPercent,
        window: nextWindow,
      });
      liveCacheKeys.add(cacheKey);
      mergedWindows.push(nextWindow);
    }

    for (const [cacheKey, cached] of cachedWindowsByAccount.get(accountId)?.entries() ?? []) {
      if (liveCacheKeys.has(cacheKey)) {
        continue;
      }
      if (cached.resetAtMs <= now) {
        continue;
      }
      const nextWindow = windowWithCachedReset(cached.window, cached.resetAtMs, now);
      cache.set(cacheKey, {
        ...cached,
        window: nextWindow,
      });
      liveCacheKeys.add(cacheKey);
      mergedWindows.push(nextWindow);
    }

    accounts[accountId] = {
      ...usage,
      windows: sortUsageWindows(mergedWindows),
    };
  }

  for (const cacheKey of cache.keys()) {
    if (!liveCacheKeys.has(cacheKey)) {
      cache.delete(cacheKey);
    }
  }

  return {
    cache,
    data: {
      ...data,
      accounts,
    },
  };
}

function subscribeIdleClock() {
  return () => {};
}

let usageResetClockNow = Date.now();
let usageResetClockTimer: number | null = null;
const usageResetClockListeners = new Set<() => void>();

function subscribeUsageResetClock(listener: () => void) {
  usageResetClockNow = Date.now();
  usageResetClockListeners.add(listener);
  if (usageResetClockTimer == null) {
    usageResetClockTimer = window.setInterval(() => {
      usageResetClockNow = Date.now();
      usageResetClockListeners.forEach((notify) => notify());
    }, 30_000);
  }

  return () => {
    usageResetClockListeners.delete(listener);
    if (usageResetClockListeners.size === 0 && usageResetClockTimer != null) {
      window.clearInterval(usageResetClockTimer);
      usageResetClockTimer = null;
    }
  };
}

function getUsageResetClockSnapshot() {
  return usageResetClockNow;
}

export function useUsageResetClock(enabled: boolean): number {
  return useSyncExternalStore(
    enabled ? subscribeUsageResetClock : subscribeIdleClock,
    getUsageResetClockSnapshot,
    getUsageResetClockSnapshot,
  );
}
