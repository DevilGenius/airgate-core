import { describe, expect, it } from 'vitest';
import { renderToStaticMarkup } from 'react-dom/server';
import type { UsageColumnConfig } from '../../../shared/columns/usageColumns';
import type { UsageLogResp } from '../../../shared/types';
import { combineUsageTimingColumns, readUsageColumnSelection, usageTokensPerSecond } from './usageTimingColumns';

describe('admin usage timing columns', () => {
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
    expect(usageTokensPerSecond({ output_tokens, duration_ms, first_token_ms })).toBe(expected);
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

