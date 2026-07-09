export const DEFAULT_ACCOUNT_MAX_CONCURRENCY = 10;
export const DEFAULT_ACCOUNT_PRIORITY = 50;
export const ACCOUNT_PRIORITY_MIN = -99999;
export const ACCOUNT_PRIORITY_MAX = 99999;
export const ACCOUNT_PRIORITY_OFFSET_MIN = ACCOUNT_PRIORITY_MIN - ACCOUNT_PRIORITY_MAX;
export const ACCOUNT_PRIORITY_OFFSET_MAX = ACCOUNT_PRIORITY_MAX - ACCOUNT_PRIORITY_MIN;
export const ACCOUNT_MSG_LOCK_EXTRA_KEY = 'msg_lock_enabled';
export const ACCOUNT_GROUP_PRIORITIES_EXTRA_KEY = 'group_priorities';

export function clampAccountPriority(value: number) {
  if (!Number.isFinite(value)) return DEFAULT_ACCOUNT_PRIORITY;
  return Math.max(ACCOUNT_PRIORITY_MIN, Math.min(ACCOUNT_PRIORITY_MAX, value));
}

export function isAccountPriorityDraft(value: string) {
  return /^-?\d*$/.test(value);
}

export function parseAccountPriorityInput(value: string) {
  if (value === '' || value === '-') return null;
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return null;
  return clampAccountPriority(Math.round(parsed));
}

export function commitAccountPriorityInput(value: string, fallback = DEFAULT_ACCOUNT_PRIORITY) {
  return parseAccountPriorityInput(value) ?? clampAccountPriority(fallback);
}

export function parseAccountPriorityOffsetInput(value: string) {
  if (value === '' || value === '-') return null;
  const parsed = Number(value);
  if (!Number.isSafeInteger(parsed)) return null;
  return parsed;
}

export function getAccountPriorityOffsetRange(minPriority?: number, maxPriority?: number) {
  if (minPriority == null || maxPriority == null) {
    return { min: ACCOUNT_PRIORITY_OFFSET_MIN, max: ACCOUNT_PRIORITY_OFFSET_MAX };
  }
  return {
    min: ACCOUNT_PRIORITY_MIN - clampAccountPriority(minPriority),
    max: ACCOUNT_PRIORITY_MAX - clampAccountPriority(maxPriority),
  };
}

export function commitAccountPriorityOffsetInput(value: string, min: number, max: number) {
  const parsed = parseAccountPriorityOffsetInput(value);
  if (parsed == null) return null;
  return Math.max(min, Math.min(max, parsed));
}

export function getAccountMessageLockEnabled(extra?: Record<string, unknown>) {
  const value = extra?.[ACCOUNT_MSG_LOCK_EXTRA_KEY];
  if (typeof value === 'string') {
    const normalized = value.trim().toLowerCase();
    return normalized === '1' || normalized === 'true';
  }
  return value === true || value === 1;
}

export function setAccountMessageLockEnabled(
  extra: Record<string, unknown> | undefined,
  enabled: boolean,
) {
  return {
    ...(extra ?? {}),
    [ACCOUNT_MSG_LOCK_EXTRA_KEY]: enabled,
  };
}

export function getAccountGroupPriorities(extra?: Record<string, unknown>) {
  const raw = extra?.[ACCOUNT_GROUP_PRIORITIES_EXTRA_KEY];
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return {};
  const result: Record<number, number> = {};
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    const groupID = Number(key);
    if (!Number.isInteger(groupID) || groupID <= 0) continue;
    if (typeof value !== 'number' || !Number.isFinite(value)) continue;
    result[groupID] = clampAccountPriority(Math.round(value));
  }
  return result;
}

export function setAccountGroupPriorities(
  extra: Record<string, unknown> | undefined,
  priorities: Record<number, number | null | undefined>,
) {
  const normalized: Record<string, number> = {};
  for (const [key, value] of Object.entries(priorities)) {
    const groupID = Number(key);
    if (!Number.isInteger(groupID) || groupID <= 0) continue;
    if (value == null || !Number.isFinite(value)) continue;
    normalized[String(groupID)] = clampAccountPriority(Math.round(value));
  }

  const next = { ...(extra ?? {}) };
  if (Object.keys(normalized).length === 0) {
    delete next[ACCOUNT_GROUP_PRIORITIES_EXTRA_KEY];
  } else {
    next[ACCOUNT_GROUP_PRIORITIES_EXTRA_KEY] = normalized;
  }
  return next;
}
