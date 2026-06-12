export const MIN_POSITIVE_RATE_MULTIPLIER = 0.01;
export const MAX_RATE_MULTIPLIER = 1000;
export const RATE_MULTIPLIER_STEP = '0.01';

export function parseRateMultiplier(raw: string): number | null {
  if (raw.trim() === '') return null;
  const value = Number(raw);
  return Number.isFinite(value) ? value : null;
}

export function isEmptyRateMultiplierInput(raw: string): boolean {
  return raw.trim() === '';
}

export function isValidRateMultiplierValue(value: number | null): value is number {
  return value != null
    && Number.isFinite(value)
    && value >= 0
    && (value === 0 || (value >= MIN_POSITIVE_RATE_MULTIPLIER && value <= MAX_RATE_MULTIPLIER));
}

export function isValidRateMultiplierInput(raw: string): boolean {
  return isValidRateMultiplierValue(parseRateMultiplier(raw));
}

export function formatRateMultiplier(value: number | null | undefined): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '-';
  return value.toLocaleString(undefined, {
    maximumFractionDigits: 3,
    minimumFractionDigits: 0,
    useGrouping: false,
  });
}
