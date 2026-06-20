import { describe, expect, it } from 'vitest';
import { formatIpList, parseIpList } from './ip';
import {
  MAX_RATE_MULTIPLIER,
  MIN_POSITIVE_RATE_MULTIPLIER,
  formatRateMultiplier,
  isEmptyRateMultiplierInput,
  isValidRateMultiplierInput,
  isValidRateMultiplierValue,
  isValidSellRateInput,
  isValidSellRateValue,
  parseRateMultiplier,
} from './rateMultiplier';

describe('IP list helpers', () => {
  it('parses multi-line IP input and formats arrays', () => {
    expect(parseIpList('')).toBeUndefined();
    expect(parseIpList(' \n ')).toBeUndefined();
    expect(parseIpList(' 10.0.0.1 \n\n 2001:db8::1 ')).toEqual(['10.0.0.1', '2001:db8::1']);
    expect(formatIpList(['10.0.0.1', '2001:db8::1'])).toBe('10.0.0.1\n2001:db8::1');
    expect(formatIpList()).toBe('');
  });
});

describe('rate multiplier helpers', () => {
  it('parses and validates account and sell rate multipliers', () => {
    expect(parseRateMultiplier('')).toBeNull();
    expect(parseRateMultiplier('not-a-number')).toBeNull();
    expect(parseRateMultiplier('1.25')).toBe(1.25);
    expect(isEmptyRateMultiplierInput('  ')).toBe(true);
    expect(isValidRateMultiplierValue(MIN_POSITIVE_RATE_MULTIPLIER)).toBe(true);
    expect(isValidRateMultiplierValue(MAX_RATE_MULTIPLIER)).toBe(true);
    expect(isValidRateMultiplierValue(0)).toBe(false);
    expect(isValidRateMultiplierInput('0.5')).toBe(true);
    expect(isValidRateMultiplierInput('0')).toBe(false);
    expect(isValidSellRateValue(0)).toBe(true);
    expect(isValidSellRateValue(0.005)).toBe(false);
    expect(isValidSellRateInput('0')).toBe(true);
    expect(formatRateMultiplier(1.23456)).toBe('1.235');
    expect(formatRateMultiplier(Number.NaN)).toBe('-');
    expect(formatRateMultiplier(undefined)).toBe('-');
  });
});
