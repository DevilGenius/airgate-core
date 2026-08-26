import { describe, expect, it } from 'vitest';
import { fmtCostPerMinute, fmtUsageEstimateCost, fmtUsageEstimateDuration, usageEstimateBadgeClass } from './DashboardPage';

describe('dashboard usage estimate formatting', () => {
  it('formats account cost rates compactly', () => {
    expect(fmtCostPerMinute(15)).toBe('$15');
    expect(fmtCostPerMinute(13.2)).toBe('$13.2');
  });

  it('formats estimated minutes as compact hours and minutes', () => {
    expect(fmtUsageEstimateDuration(30)).toBe('30m');
    expect(fmtUsageEstimateDuration(85)).toBe('1h25m');
    expect(fmtUsageEstimateDuration(334)).toBe('5h34m');
    expect(fmtUsageEstimateDuration(999 * 60)).toBe('999h');
    expect(fmtUsageEstimateDuration(59999)).toBe('999h59m');
    expect(fmtUsageEstimateDuration(1000 * 60)).toBe('1000h');
    expect(fmtUsageEstimateDuration(1000 * 60 + 1)).toBe('>1000h');
    expect(fmtUsageEstimateDuration(undefined)).toBe('');
  });

  it('formats the estimated window cost with K/M/B normalization', () => {
    expect(fmtUsageEstimateCost(999)).toBe('$999');
    expect(fmtUsageEstimateCost(1406)).toBe('$1.4K');
    expect(fmtUsageEstimateCost(1999.6)).toBe('$2K');
    expect(fmtUsageEstimateCost(1500)).toBe('$1.5K');
    expect(fmtUsageEstimateCost(7403)).toBe('$7.4K');
    expect(fmtUsageEstimateCost(999900)).toBe('$999.9K');
    expect(fmtUsageEstimateCost(99999)).toBe('$100K');
    expect(fmtUsageEstimateCost(2_500_000)).toBe('$2.5M');
    expect(fmtUsageEstimateCost(3_000_000_000)).toBe('$3B');
  });

  it('picks the usage estimate badge tone from the plus 5h remaining cost', () => {
    expect(usageEstimateBadgeClass(undefined)).toContain('cyan');
    expect(usageEstimateBadgeClass(300)).toContain('cyan');
    expect(usageEstimateBadgeClass(299)).toContain('amber');
    expect(usageEstimateBadgeClass(200)).toContain('amber');
    expect(usageEstimateBadgeClass(199)).toContain('red');
    expect(usageEstimateBadgeClass(100)).toContain('red');
    expect(usageEstimateBadgeClass(99)).toContain('zinc-900');
  });

});
