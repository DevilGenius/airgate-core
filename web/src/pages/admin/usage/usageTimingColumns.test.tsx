import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import type { TFunction } from 'i18next';
import { formatTimingMs, usageLatencyTone, type UsageColumnConfig } from '../../../shared/columns/usageColumns';
import type { UsageLogResp } from '../../../shared/types';
import { combineUsageTimingColumns, createUsageTpsColumn, readUsageColumnSelection, usageTokensPerSecond } from './usageTimingColumns';

describe('admin usage timing columns', () => {
  it.each([
    [4999, 5000, 15000, 'normal'],
    [5000, 5000, 15000, 'slow'],
    [14999, 5000, 15000, 'slow'],
    [15000, 5000, 15000, 'critical'],
    [0, 5000, 15000, null],
  ] as const)('classifies latency boundaries: %s ms => %s', (value, warningAt, criticalAt, expected) => {
    expect(usageLatencyTone(value, warningAt, criticalAt)).toBe(expected);
  });

  it.each([
    [120, '0.12s'],
    [999, '0.99s'],
    [60_000, '1m'],
    [61_000, '1m 1s'],
    [119_600, '2m'],
    [59_999, '60.00s'],
  ] as const)('formats long latency %s ms as %s', (value, expected) => {
    expect(formatTimingMs(value)).toBe(expected);
  });

  const metric = (key: string, title: string): UsageColumnConfig<UsageLogResp> => ({
    key, title, render: () => <span>{key}</span>,
  });
  const columns = [
    metric('model', '模型'), metric('ws_dial_ms', '握手'), metric('first_event_ms', '响应'),
    metric('first_token_ms', '首字'), metric('duration_ms', '耗时'), metric('tps', 'TPS'),
  ];
  const row = {} as UsageLogResp;

  it('stacks handshake/response and first-token/duration in order, keeping TPS independent', () => {
    const result = combineUsageTimingColumns(columns);
    expect(result.map(({ key, title }) => [key, title])).toEqual([
      ['model', '模型'], ['first_event_ms', '握手/响应'], ['duration_ms', '首字/耗时'], ['tps', 'TPS'],
    ]);
    const response = renderToStaticMarkup(<div>{result[1]!.render(row)}</div>);
    const duration = renderToStaticMarkup(<div>{result[2]!.render(row)}</div>);
    expect(response.indexOf('ws_dial_ms')).toBeLessThan(response.indexOf('first_event_ms'));
    expect(duration.indexOf('first_token_ms')).toBeLessThan(duration.indexOf('duration_ms'));
  });

  it.each([
    [['ws_dial_ms', 'first_event_ms', 'first_token_ms', 'duration_ms'], ['握手/响应', '首字/耗时']],
    [['ws_dial_ms', 'first_event_ms', 'first_token_ms'], ['握手/响应', '首字']],
    [['ws_dial_ms', 'first_token_ms', 'duration_ms'], ['握手', '首字/耗时']],
    [['ws_dial_ms', 'first_token_ms'], ['握手', '首字']],
    [['first_event_ms', 'duration_ms'], ['响应', '耗时']],
    [['ws_dial_ms', 'duration_ms'], ['握手', '耗时']],
    [['first_event_ms', 'first_token_ms'], ['响应', '首字']],
  ])('uses the selected metrics for %j', (keys, titles) => {
    const result = combineUsageTimingColumns(columns.filter((column) => keys.includes(column.key)));
    expect(result.map((column) => column.title)).toEqual(titles);
    const html = result.map((column) => renderToStaticMarkup(<div>{column.render(row)}</div>)).join('');
    for (const key of ['ws_dial_ms', 'first_event_ms', 'first_token_ms', 'duration_ms']) {
      expect(html.includes(key)).toBe(keys.includes(key));
    }
  });

  it('omits both timing columns when all time metrics are hidden', () => {
    expect(combineUsageTimingColumns(columns.filter((column) => column.key === 'tps')).map((column) => column.key)).toEqual(['tps']);
  });
});

describe('shared TPS column', () => {
  const t = ((key: string) => key) as unknown as TFunction;
  const column = createUsageTpsColumn<UsageLogResp>(t);
  const renderTps = (row: Partial<UsageLogResp>) => renderToStaticMarkup(<div>{column.render(row as UsageLogResp)}</div>);

  it.each([999, 1000, 9999, 10000, 20000, 1000000])('keeps a fixed right-aligned slot at %s TPS', (rate) => {
    const html = renderTps({ output_tokens: rate, duration_ms: 1000, first_token_ms: 0, stream: true });
    expect(html).toContain('w-[4ch] shrink-0 justify-end whitespace-nowrap text-right');
    const label = rate >= 10000 ? `${Math.round(rate / 1000)}k` : String(rate);
    expect(html).toContain(`>${label}</span>`);
  });

  it('is shared by the admin and the user usage table with the same metadata', () => {
    expect([column.key, column.title, column.width, column.hideOnMobile])
      .toEqual(['tps', 'usage.tps', '72px', true]);
  });

  it('right-aligns the value in a non-shrinking four-character slot placed before the dot', () => {
    const html = renderTps({ output_tokens: 300, duration_ms: 3000, first_token_ms: 1000, stream: true });
    expect(html).toContain('w-[4ch] shrink-0 justify-end whitespace-nowrap text-right');
    expect(html.indexOf('150')).toBeGreaterThan(-1);
    expect(html.indexOf('150')).toBeLessThan(html.indexOf('rounded-full'));
  });

  it.each([
    [5, 'bg-danger'],
    [9, 'bg-danger'],
    [10, 'bg-warning'],
    [29, 'bg-warning'],
    [30, 'bg-success'],
    [120, 'bg-success'],
  ])('marks %s tps with the %s dot', (rate, expected) => {
    const html = renderTps({ output_tokens: rate * 1000, duration_ms: 1_000_000, first_token_ms: 0, stream: true });
    expect(html).toContain(expected);
  });

  it('falls back to a dash and a neutral dot when no sample is available', () => {
    const html = renderTps({ output_tokens: 0, duration_ms: 0, stream: true });
    expect(html).toContain('>-<');
    expect(html).toContain('bg-border');
  });

  it('compacts rates at or above 10000 tps into k units', () => {
    const html = renderTps({ output_tokens: 20_000, duration_ms: 1000, first_token_ms: 0, stream: true });
    expect(html).toContain('20');
    expect(html).toContain('k');
    expect(html).not.toContain('20000');
  });
});

describe('tokens per second', () => {
  it.each([
    [100, 3000, 1000, 50],
    [100, 2000, 0, 50],
    [100, 2000, Number.NaN, 50],
    [0, 2000, 1000, null],
    [100, 0, 0, null],
    [100, 1000, 1000, null],
    [100, 1000, 2000, null],
    [Number.NaN, 1000, 0, null],
    [100, Number.POSITIVE_INFINITY, 0, null],
  ])('tokens=%s duration=%s firstToken=%s => %s', (output_tokens, duration_ms, first_token_ms, expected) => {
    expect(usageTokensPerSecond({ output_tokens, duration_ms, first_token_ms, stream: true })).toBe(expected);
  });

  it.each(['apikey', 'api_key', ' APIKey '])('omits misleading TPS for synchronous %s upstreams', (account_type) => {
    expect(usageTokensPerSecond({
      output_tokens: 1000, duration_ms: 10001, first_token_ms: 10000, stream: false, account_type,
    })).toBeNull();
  });

  it.each([
    ['apikey', true],
    ['api_key', true],
    ['oauth', false],
    [undefined, false],
  ])('preserves TPS for account type %s with stream=%s', (account_type, stream) => {
    expect(usageTokensPerSecond({
      output_tokens: 1000, duration_ms: 10001, first_token_ms: 10000, stream, account_type,
    })).toBe(1000000);
  });
});

describe('column preferences', () => {
  const defaults = ['model', 'first_event_ms', 'duration_ms', 'tps'];
  it('adds TPS once to legacy preferences without enabling other hidden metrics', () => {
    expect([...readUsageColumnSelection(['model', 'ws_dial_ms'], defaults)]).toEqual(['model', 'ws_dial_ms', 'tps']);
  });
  it('keeps TPS hidden after a user disables it in the new format', () => {
    expect([...readUsageColumnSelection({ version: 2, keys: ['model', 'ws_dial_ms'] }, defaults)]).toEqual(['model', 'ws_dial_ms']);
  });
  it('uses defaults for invalid preferences', () => {
    expect([...readUsageColumnSelection(null, defaults)]).toEqual(defaults);
    expect([...readUsageColumnSelection([], defaults)]).toEqual(defaults);
  });
});

