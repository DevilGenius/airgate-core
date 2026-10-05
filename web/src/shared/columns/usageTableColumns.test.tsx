import { renderHook } from '@testing-library/react';
import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it, vi } from 'vitest';
import type { UsageLogResp } from '../types';
import { ADMIN_USAGE_DEFAULT_COLUMN_KEYS, useUsageTableColumns, type UsageTableMode } from './usageTableColumns';
import { combineUsageTimingColumns, readUsageColumnSelection } from './usageTimingColumns';
import { UsageRichTooltipProvider } from './usageColumns';

vi.mock('react-i18next', () => ({
  initReactI18next: { init: () => {}, type: '3rdParty' },
  useTranslation: () => ({ t: (key: string) => key }),
}));

describe('shared usage table modes', () => {
  it.each<UsageTableMode>(['admin', 'user', 'apiKey'])('keeps common order and mode-specific fields for %s', (mode) => {
    const { result } = renderHook(() => useUsageTableColumns(mode));
    const keys = result.current.allColumns.map((column) => column.key);
    const common = ['created_at', 'model', 'first_event_ms', 'first_token_ms', 'duration_ms', 'tps', 'tokens', 'cost', 'stream', 'endpoint', 'client'];
    expect(keys.filter((key) => common.includes(key))).toEqual(common);
    expect(keys.includes('api_key')).toBe(mode !== 'apiKey');
    for (const key of ['user_id', 'account_name', 'ws_dial_ms']) {
      expect(keys.includes(key)).toBe(mode === 'admin');
    }
    if (mode !== 'apiKey') expect(keys.slice(keys.indexOf('created_at'), keys.indexOf('model') + 1)).toEqual(['created_at', 'api_key', 'model']);
    expect(new Set(keys).size).toBe(keys.length);
    expect(result.current.columns.some((column) => column.key === 'first_token_ms')).toBe(false);
    expect(result.current.columns.some((column) => column.key === 'duration_ms')).toBe(true);
  });

  it('keeps stored admin selections before merging timing columns', () => {
    const { result } = renderHook(() => useUsageTableColumns('admin'));
    const selected = readUsageColumnSelection({ version: 2, keys: ['created_at', 'first_event_ms', 'cost'] }, ADMIN_USAGE_DEFAULT_COLUMN_KEYS);
    const columns = combineUsageTimingColumns(result.current.allColumns.filter((column) => selected.has(column.key)));
    expect(columns.map((column) => column.key)).toEqual(['created_at', 'first_event_ms', 'cost']);
    expect(columns[1]?.title).toBe('usage.first_event');
    expect(ADMIN_USAGE_DEFAULT_COLUMN_KEYS).toContain('latency');
    expect(ADMIN_USAGE_DEFAULT_COLUMN_KEYS).not.toContain('duration_ms');
  });

  it('preserves API key customer billing instead of account costs', () => {
    const { result } = renderHook(() => useUsageTableColumns('apiKey'));
    const cost = result.current.columns.find((column) => column.key === 'cost');
    const row = { id: 1, platform: 'openai', cost: 1.234567, billed_cost: 9.876543, actual_cost: 8.765432 } as unknown as UsageLogResp;
    const html = renderToStaticMarkup(<UsageRichTooltipProvider>{cost?.render(row)}</UsageRichTooltipProvider>);
    expect(html).toContain('1.234567');
    expect(html).not.toContain('9.876543');
    expect(html).not.toContain('8.765432');
  });
});
