export const DEFAULT_ACCOUNT_MAX_CONCURRENCY = 10;
export const DEFAULT_ACCOUNT_PRIORITY = 50;
export const ACCOUNT_PRIORITY_MIN = -999;
export const ACCOUNT_PRIORITY_MAX = 999;

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
