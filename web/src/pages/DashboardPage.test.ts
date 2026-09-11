import { describe, expect, it } from 'vitest';
import {
  apiKeyBilledCostDataKey,
  buildTopAPIKeyChartData,
  chartTooltipBilledCost,
  fmtCostPerMinute,
  fmtUsageEstimateCost,
  fmtUsageEstimateDuration,
  sortTooltipPayloadByTokenUsage,
  sortTopAPIKeysByTokens,
  topAPIKeyTotalTokens,
  usageEstimateBadgeClass,
} from './DashboardPage';

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
    expect(usageEstimateBadgeClass(undefined)).toContain('violet');
    expect(usageEstimateBadgeClass(300)).toContain('violet');
    expect(usageEstimateBadgeClass(299)).toContain('amber');
    expect(usageEstimateBadgeClass(200)).toContain('amber');
    expect(usageEstimateBadgeClass(199)).toContain('red');
    expect(usageEstimateBadgeClass(100)).toContain('red');
    expect(usageEstimateBadgeClass(99)).toContain('zinc-900');
  });

});

describe('dashboard key top 12 billed cost', () => {
  const topAPIKeys = [
    { api_key_id: 7, name: 'prod', trend: [{ time: '2026-04-01 10:00', tokens: 1200, billed_cost: 1.25 }] },
    { api_key_id: 9, name: 'dev', trend: [{ time: '2026-04-01 11:00', tokens: 800, billed_cost: 0.5 }] },
  ];

  it('keeps each key billed cost on the same row as its token usage', () => {
    const rows = buildTopAPIKeyChartData(topAPIKeys, ['api_key_7', 'api_key_9']);

    expect(rows).toHaveLength(2);
    expect(rows[0]?.['api_key_7']).toBe(1200);
    expect(rows[0]?.[apiKeyBilledCostDataKey('api_key_7')]).toBe(1.25);
    // 该时间桶内没有用量的 Key 补 0，避免折线断裂。
    expect(rows[0]?.['api_key_9']).toBe(0);
    expect(rows[0]?.[apiKeyBilledCostDataKey('api_key_9')]).toBe(0);
    expect(rows[1]?.['api_key_9']).toBe(800);
    expect(rows[1]?.[apiKeyBilledCostDataKey('api_key_9')]).toBe(0.5);
  });

  it('returns no rows without keys and resolves the tooltip cost from a chart row', () => {
    expect(buildTopAPIKeyChartData([], [])).toEqual([]);

    const rows = buildTopAPIKeyChartData(topAPIKeys, ['api_key_7', 'api_key_9']);
    expect(chartTooltipBilledCost('api_key_7', rows[0])).toBe(1.25);
    expect(chartTooltipBilledCost('api_key_9', rows[0])).toBe(0);
    expect(chartTooltipBilledCost(undefined, rows[0])).toBeUndefined();
    expect(chartTooltipBilledCost('api_key_7', undefined)).toBeUndefined();
  });
});

describe('dashboard key top 12 ordering', () => {
  const rankedKeys = [
    { api_key_id: 3, name: 'mid', trend: [{ time: '2026-04-01 10:00', tokens: 100, billed_cost: 1 }, { time: '2026-04-01 11:00', tokens: 50, billed_cost: 1 }] },
    { api_key_id: 1, name: 'top', trend: [{ time: '2026-04-01 10:00', tokens: 900, billed_cost: 1 }] },
    { api_key_id: 9, name: 'low', trend: [{ time: '2026-04-01 10:00', tokens: 20, billed_cost: 1 }] },
  ];

  it('sums a key token consumption across time buckets', () => {
    expect(topAPIKeyTotalTokens(rankedKeys[0]!)).toBe(150);
    expect(topAPIKeyTotalTokens({ api_key_id: 4, name: 'empty', trend: [] })).toBe(0);
  });

  it('orders keys from the most to the least token consumption', () => {
    expect(sortTopAPIKeysByTokens(rankedKeys).map((item) => item.api_key_id)).toEqual([1, 3, 9]);
  });

  it('breaks ties by api key id, matching the backend ranking', () => {
    const tiedKeys = [
      { api_key_id: 5, name: 'later', trend: [{ time: '2026-04-01 10:00', tokens: 10, billed_cost: 1 }] },
      { api_key_id: 2, name: 'earlier', trend: [{ time: '2026-04-01 10:00', tokens: 10, billed_cost: 1 }] },
    ];
    expect(sortTopAPIKeysByTokens(tiedKeys).map((item) => item.api_key_id)).toEqual([2, 5]);
  });

  it('keeps the input array untouched', () => {
    const input = [...rankedKeys];
    sortTopAPIKeysByTokens(input);
    expect(input.map((item) => item.api_key_id)).toEqual([3, 1, 9]);
  });

  it('orders tooltip rows by the hovered bucket token usage', () => {
    const payload: Array<{ dataKey?: string; value?: number }> = [
      { dataKey: 'api_key_1', value: 10_350_000_000 },
      { dataKey: 'api_key_2', value: 0 },
      { dataKey: 'api_key_3', value: 0 },
      { dataKey: 'api_key_4', value: 4_140_000 },
      { dataKey: 'api_key_5', value: 64_940_000 },
      { dataKey: 'api_key_6', value: 0 },
      { dataKey: 'api_key_7', value: 0 },
      { dataKey: 'api_key_8', value: 471_600 },
      { dataKey: 'api_key_9', value: 0 },
      { dataKey: 'api_key_10', value: 0 },
      { dataKey: 'api_key_11', value: 318_320 },
      { dataKey: 'api_key_12', value: 28_490_000 },
    ];
    expect(sortTooltipPayloadByTokenUsage(payload, payload.map((item) => String(item.dataKey))).map((item) => item.value))
      .toEqual([10_350_000_000, 64_940_000, 28_490_000, 4_140_000, 471_600, 318_320, 0, 0, 0, 0, 0, 0]);
  });

  it('falls back to the card ranking for equal usage and keeps unknown entries last', () => {
    const payload: Array<{ dataKey?: string; value?: number }> = [
      { dataKey: 'api_key_5', value: 0 },
      { dataKey: 'api_key_1', value: 0 },
      { dataKey: 'unknown_b', value: 0 },
      { dataKey: 'api_key_3', value: 0 },
    ];
    expect(sortTooltipPayloadByTokenUsage(payload, ['api_key_1', 'api_key_3', 'api_key_5']).map((item) => item.dataKey))
      .toEqual(['api_key_1', 'api_key_3', 'api_key_5', 'unknown_b']);
  });

  it('treats missing and non finite tooltip values as zero usage', () => {
    const payload: Array<{ dataKey?: string; value?: number }> = [
      { dataKey: 'api_key_2' },
      { dataKey: 'api_key_1' },
      { dataKey: 'api_key_3', value: Number.NaN },
      { dataKey: 'api_key_4', value: 5 },
    ];
    expect(sortTooltipPayloadByTokenUsage(payload, ['api_key_1', 'api_key_2', 'api_key_3', 'api_key_4']).map((item) => item.dataKey))
      .toEqual(['api_key_4', 'api_key_1', 'api_key_2', 'api_key_3']);
  });
});
