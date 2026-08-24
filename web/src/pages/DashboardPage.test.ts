import { describe, expect, it } from 'vitest';
import { fmtCostPerMinute, fmtUsageEstimateCost, fmtUsageEstimateDuration, shouldHideUsageEstimateIcon } from './DashboardPage';

describe('dashboard usage estimate formatting', () => {
  it('formats account cost rates compactly', () => {
    expect(fmtCostPerMinute(15)).toBe('$15');
    expect(fmtCostPerMinute(13.2)).toBe('$13.2');
  });

  it('formats estimated minutes as compact hours and minutes', () => {
    expect(fmtUsageEstimateDuration(85)).toBe('1h25min');
    expect(fmtUsageEstimateDuration(334)).toBe('5h34min');
    expect(fmtUsageEstimateDuration(undefined)).toBe('');
  });

  it('formats the estimated 100 percent window cost', () => {
    expect(fmtUsageEstimateCost(1999.6)).toBe('$2000');
  });

  it('hides the icon when both windows and plans need the available width', () => {
    expect(shouldHideUsageEstimateIcon(30, 2, 2)).toBe(true);
    expect(shouldHideUsageEstimateIcon(30, 1, 2)).toBe(false);
    expect(shouldHideUsageEstimateIcon(48, 1, 1)).toBe(true);
  });
});
