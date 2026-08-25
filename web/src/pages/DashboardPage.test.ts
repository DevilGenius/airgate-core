import { describe, expect, it } from 'vitest';
import { fmtCostPerMinute, fmtUsageEstimateCost, fmtUsageEstimateDuration } from './DashboardPage';

describe('dashboard usage estimate formatting', () => {
  it('formats account cost rates compactly', () => {
    expect(fmtCostPerMinute(15)).toBe('$15');
    expect(fmtCostPerMinute(13.2)).toBe('$13.2');
  });

  it('formats estimated minutes as compact hours and minutes', () => {
    expect(fmtUsageEstimateDuration(30)).toBe('30m');
    expect(fmtUsageEstimateDuration(85)).toBe('1h25m');
    expect(fmtUsageEstimateDuration(334)).toBe('5h34m');
    expect(fmtUsageEstimateDuration(undefined)).toBe('');
  });

  it('formats the estimated 100 percent window cost', () => {
    expect(fmtUsageEstimateCost(1999.6)).toBe('$2000');
  });

});
