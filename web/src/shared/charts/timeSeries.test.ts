import { describe, expect, it } from 'vitest';
import { buildAPIKeyTrendModel, sortTopAPIKeysByTokens, topAPIKeyTotalTokens } from './apiKeyTrend';
import { buildTokenTrendModel } from './tokenTrend';
import { chartTimeLabel, formatTimeSeriesTooltip, timeSeriesOption, tooltipIndex, type TimeSeriesModel } from './timeSeries';
import type { ChartTheme } from './BaseChart';

const theme: ChartTheme = { text: '#111', muted: '#666', surface: '#fff', grid: '#eee', pointer: '#aaa', font: 'sans-serif', reducedMotion: false, color: value => value };
const tokenData = [
  { time: '2026-08-16 10:00', input: 100, output: 100, cacheRead: 50, cacheCreation: 50, actualCost: 12.5, standardCost: 20 },
  { time: '2026-08-16 11:00', input: 300, output: 0, cacheRead: 0, cacheCreation: 0, actualCost: 0, standardCost: 0 },
];
const labels = { input: 'Input', output: 'Output', cacheRead: 'Cache read', cacheCreation: 'Cache creation', cacheRatio: 'Ratio', cacheCumulativeRatio: 'Cumulative', actual: 'Actual', standard: 'Standard' };

describe('token trend data contract', () => {
  it('preserves token categories and calculates per-bucket and weighted cumulative ratios', () => {
    const model = buildTokenTrendModel(tokenData, labels);
    expect(model.series.map(series => series.key)).toEqual(['input', 'output', 'cacheCreation', 'cacheRead', 'cacheRatio', 'cacheCumulativeRatio']);
    expect(model.series[4]?.values[0]).toBeCloseTo(100 / 3);
    expect(model.series[4]?.values[1]).toBe(0);
    expect(model.series[5]?.values[1]).toBeCloseTo(100 / 6);
    expect(model.axes?.[1]?.max).toBe(100);
    expect(model.details?.(0)).toEqual([{ label: 'Actual', value: '$12.50', costTone: 'actual' }, { label: 'Standard', value: '$20.00', costTone: 'standard' }]);
  });

  it('keeps user overview limited to its three metrics without a redundant ratio axis', () => {
    const model = buildTokenTrendModel([{ time: '2026-08-16', input: 0, output: 0, cacheRead: 0 }], labels, ['input', 'output', 'cacheRead']);
    expect(model.axes).toHaveLength(1);
    expect(model.series).toHaveLength(3);
    expect(model.details?.(0)).toEqual([]);
    expect(buildTokenTrendModel([{ time: 'empty', input: 0, output: 0, cacheRead: 0 }], labels).series[4]?.values).toEqual([0]);
  });
});

describe('API key trend data contract', () => {
  const keys = [
    { api_key_id: 7, name: 'prod', trend: [{ time: '2026-08-16 10:00', tokens: 1200, billed_cost: 1.25 }] },
    { api_key_id: 9, name: 'dev', trend: [{ time: '2026-08-16 11:00', tokens: 800, billed_cost: 0.5 }] },
  ];

  it('keeps the union of buckets and aligns tokens with billed costs', () => {
    const model = buildAPIKeyTrendModel(keys, 'Plugin');
    expect(model.times).toEqual(['2026-08-16 10:00', '2026-08-16 11:00']);
    expect(model.series[0]?.values).toEqual([1200, 0]);
    expect(model.series[0]?.costs).toEqual([1.25, 0]);
    expect(model.series[1]?.values).toEqual([0, 800]);
    expect(model.series[1]?.costs).toEqual([0, 0.5]);
    expect(topAPIKeyTotalTokens(keys[0]!)).toBe(1200);
    expect(buildAPIKeyTrendModel([], 'Plugin').times).toEqual([]);
  });

  it('uses total tokens then ID for ranking without mutating source arrays', () => {
    const source = [keys[1]!, { ...keys[0]!, api_key_id: 10 }, keys[0]!];
    expect(sortTopAPIKeysByTokens(source).map(key => key.api_key_id)).toEqual([7, 10, 9]);
    expect(source.map(key => key.api_key_id)).toEqual([9, 10, 7]);
    const before = buildAPIKeyTrendModel(keys, 'Plugin');
    const after = buildAPIKeyTrendModel([{ ...keys[1]!, trend: [{ time: 'new', tokens: 5000, billed_cost: 2 }] }, keys[0]!], 'Plugin');
    expect(before.series[0]?.key).toBe(after.series[1]?.key);
  });
});

describe('shared ECharts options and tooltips', () => {
  it('keeps sparse points visible and does not connect missing observations', () => {
    const model: TimeSeriesModel = { times: ['a', 'b', 'c'], series: [{ key: 'one', label: 'One', color: '#000', values: [null, 5, null] }] };
    const option = timeSeriesOption(model, { ...theme, reducedMotion: true });
    expect(option.animation).toBe(false);
    expect(option.series).toEqual([expect.objectContaining({ id: 'one', data: [null, 5, null], showSymbol: true, connectNulls: false })]);
    expect(chartTimeLabel('2026-08-16 10:00')).toBe('08/16\n10:00');
    expect(chartTimeLabel('2026-08-16')).toBe('08/16');
    expect(tooltipIndex([{ dataIndex: 2 }])).toBe(2);
    expect(tooltipIndex([])).toBe(-1);
  });

  it('renders aligned values and costs in descending hovered-bucket token order', () => {
    const model: TimeSeriesModel = { times: ['2026-08-16'], sortTooltip: true, series: [
      { key: 'a', label: 'A', color: '#000', values: [10], costs: [1.25] },
      { key: 'b', label: 'B', color: '#111', values: [1_200_000], costs: [20] },
      { key: 'c', label: 'C', color: '#222', values: [10], costs: [0] },
    ] };
    const element = document.createElement('div');
    element.innerHTML = formatTimeSeriesTooltip(model, 0);
    const grid = element.children[1] as HTMLElement;
    expect(grid.style.gridTemplateColumns).toBe('7px minmax(0,1fr) auto auto');
    const cells = Array.from(grid.children);
    expect(cells.filter((_, index) => index % 4 === 1).map(cell => cell.textContent)).toEqual(['B', 'A', 'C']);
    expect(cells.filter((_, index) => index % 4 === 2).map(cell => cell.textContent)).toEqual(['1.20M', '10', '10']);
    expect(cells.filter((_, index) => index % 4 === 3).map(cell => cell.textContent)).toEqual(['$20.00', '$1.25', '$0.00']);
    expect(formatTimeSeriesTooltip(model, -1)).toBe('');
    expect(formatTimeSeriesTooltip(model, 2)).toBe('');
  });

  it('escapes API key names and shows only selected series', () => {
    const model: TimeSeriesModel = { times: ['<script>'], series: [
      { key: 'safe', label: '<img src=x onerror=alert(1)>', color: '#000', values: [5] },
      { key: 'hidden', label: 'Hidden', color: '#111', values: [10] },
    ] };
    const element = document.createElement('div');
    element.innerHTML = formatTimeSeriesTooltip(model, 0, new Set(['safe']));
    expect(element.querySelector('img, script')).toBeNull();
    expect(element.textContent).toContain('<img src=x onerror=alert(1)>');
    expect(element.textContent).not.toContain('Hidden');
  });

  it('keeps token tooltip rows in metric order and includes actual and standard cost', () => {
    const model = buildTokenTrendModel(tokenData, labels);
    const element = document.createElement('div');
    element.innerHTML = formatTimeSeriesTooltip(model, 0);
    const grid = element.children[1]!;
    expect(Array.from(grid.children).filter((_, index) => index % 3 === 1).map(cell => cell.textContent)).toEqual(Object.values(labels).slice(0, 2).concat(['Cache creation', 'Cache read', 'Ratio', 'Cumulative']));
    expect(element.textContent).toContain('33.3%');
    expect(element.textContent).toContain('Actual$12.50Standard$20.00');
  });
});
