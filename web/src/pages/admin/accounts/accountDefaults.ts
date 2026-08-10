export const DEFAULT_ACCOUNT_MAX_CONCURRENCY = 10;
export const DEFAULT_ACCOUNT_PRIORITY = 50;
export const DEFAULT_ACCOUNT_PRIORITY_SEQUENCE_INITIAL = 1000;
export const DEFAULT_ACCOUNT_PRIORITY_SEQUENCE_STEP = -1;
export const DEFAULT_ACCOUNT_PRIORITY_SEQUENCE_GROUP_SIZE = 5;
export const ACCOUNT_PRIORITY_MIN = -99999;
export const ACCOUNT_PRIORITY_MAX = 99999;
export const ACCOUNT_PRIORITY_OFFSET_MIN = ACCOUNT_PRIORITY_MIN - ACCOUNT_PRIORITY_MAX;
export const ACCOUNT_PRIORITY_OFFSET_MAX = ACCOUNT_PRIORITY_MAX - ACCOUNT_PRIORITY_MIN;
export const ACCOUNT_MSG_LOCK_EXTRA_KEY = 'msg_lock_enabled';

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

export type AccountPrioritySequencePreview = {
  initial: number;
  step: number;
  groupSize: number;
  levels: number;
  last: number;
  lastGroupSize: number;
};

export function getAccountPrioritySequencePreview(
  accountCount: number,
  initial: number | null,
  step: number | null,
  groupSize: number | null,
): AccountPrioritySequencePreview | null {
  if (!Number.isSafeInteger(accountCount) || accountCount <= 0) return null;
  if (
    initial == null
    || !Number.isSafeInteger(initial)
    || initial < ACCOUNT_PRIORITY_MIN
    || initial > ACCOUNT_PRIORITY_MAX
  ) {
    return null;
  }
  if (step == null || !Number.isSafeInteger(step) || step === 0) return null;
  if (groupSize == null || !Number.isSafeInteger(groupSize) || groupSize <= 0) return null;

  const levels = Math.ceil(accountCount / groupSize);
  const last = initial + (levels - 1) * step;
  if (!Number.isSafeInteger(last) || last < ACCOUNT_PRIORITY_MIN || last > ACCOUNT_PRIORITY_MAX) return null;

  return {
    initial,
    step,
    groupSize,
    levels,
    last,
    lastGroupSize: accountCount - (levels - 1) * groupSize,
  };
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
