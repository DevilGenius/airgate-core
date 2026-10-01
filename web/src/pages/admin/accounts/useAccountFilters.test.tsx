import { act, cleanup, render, renderHook, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import userEvent from '@testing-library/user-event';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { STORAGE_KEYS } from '../../../shared/storageKeys';
import { accountsApi } from '../../../shared/api/accounts';
import { accountFiltersToQuery, normalizeAccountFilters } from './accountFilters';
import { useAccountFilters } from './useAccountFilters';
import { AccountFilterIcons } from './AccountFilterIcons';
import { NO_ACCOUNT_FILTER } from './accountFilterConstants';

const api = vi.hoisted(() => ({ get: vi.fn(async () => ({ list: [], total: 0 })) }));
vi.mock('../../../shared/api/client', () => ({ get: api.get, post: vi.fn(), put: vi.fn(), patch: vi.fn(), del: vi.fn() }));
const storageKey = STORAGE_KEYS.ui.adminAccountsFilters;
beforeEach(() => { window.history.replaceState(null, '', '/admin/accounts'); api.get.mockClear(); });
afterEach(() => { cleanup(); window.localStorage.clear(); window.history.replaceState(null, '', '/'); vi.restoreAllMocks(); });

it('persists all structured filter dimensions and restores them without a URL', () => {
  const expected = normalizeAccountFilters({ keyword: 'alice', platforms: ['kiro', 'openai'], accountTypes: ['apikey'],
    plans: [{ platform: 'openai', key: 'plus' }], groupIds: [2, 1], ungrouped: true,
    states: ['disabled', 'working'], proxyIds: [8, 3], prioritySort: 'asc' });
  const first = renderHook(useAccountFilters);
  act(() => first.result.current.updateFilters(expected));
  expect(JSON.parse(window.localStorage.getItem(storageKey)!)).toEqual(expected);
  expect(JSON.parse(new URLSearchParams(window.location.search).get('filters')!)).toEqual(expected);
  first.unmount();
  window.history.replaceState(null, '', '/admin/accounts');
  const second = renderHook(useAccountFilters);
  expect(second.result.current.filters).toEqual(expected);
});

it('atomically merges rapid independent changes and persists explicit clearing', () => {
  const { result, unmount } = renderHook(useAccountFilters);
  act(() => {
    result.current.updateFilters({ accountTypes: ['oauth'] });
    result.current.updateFilters({ platforms: ['openai'], groupIds: [2] });
    result.current.updateFilters({ plans: [{ platform: 'openai', key: 'plus' }] });
  });
  expect(result.current.filters).toMatchObject({ accountTypes: ['oauth'], platforms: ['openai'], groupIds: [2], plans: [{ platform: 'openai', key: 'plus' }] });
  act(() => result.current.updateFilters({ accountTypes: [NO_ACCOUNT_FILTER], platforms: [NO_ACCOUNT_FILTER], plans: [], groupIds: [], states: [], proxyIds: [] }));
  unmount();
  window.history.replaceState(null, '', '/admin/accounts');
  const restored = renderHook(useAccountFilters);
  expect(restored.result.current.filters).toMatchObject({ accountTypes: [NO_ACCOUNT_FILTER], plans: [], groupIds: [], platforms: [NO_ACCOUNT_FILTER] });
  expect(accountFiltersToQuery(restored.result.current.filters)).toMatchObject({ account_type: NO_ACCOUNT_FILTER, platform: NO_ACCOUNT_FILTER });
});

it('migrates existing per-field selections without turning a plan into an OAuth selection', () => {
  window.localStorage.setItem(`${storageKey}:type`, 'oauth_plan:openai:plus,apikey');
  window.localStorage.setItem(`${storageKey}:group`, '3,__ungrouped__');
  window.history.replaceState(null, '', '/admin/accounts?q=alice&platform=openai&state=active&proxy=7');
  const { result } = renderHook(useAccountFilters);
  expect(result.current.filters).toMatchObject({ keyword: 'alice', platforms: ['openai'], accountTypes: ['apikey'],
    plans: [{ platform: 'openai', key: 'plus' }], groupIds: [3], ungrouped: true, states: ['active'], proxyIds: [7] });
  expect(window.localStorage.getItem(`${storageKey}:type`)).toBeNull();
  expect(new URLSearchParams(window.location.search).has('type')).toBe(false);
});

it('restores browser history as one object without losing unrelated filters', () => {
  const { result } = renderHook(useAccountFilters);
  const saved = normalizeAccountFilters({ accountTypes: ['apikey'], platforms: ['kiro'], proxyIds: [3] });
  act(() => {
    window.history.replaceState(null, '', `/admin/accounts?filters=${encodeURIComponent(JSON.stringify(saved))}`);
    window.dispatchEvent(new PopStateEvent('popstate'));
  });
  expect(result.current.filters).toEqual(saved);
  expect(JSON.parse(window.localStorage.getItem(storageKey)!)).toEqual(saved);
});

it('keeps filters operational if browser storage is unavailable', () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new Error('blocked'); });
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new Error('blocked'); });
  const { result } = renderHook(useAccountFilters);
  act(() => result.current.updateFilters({ accountTypes: ['oauth'] }));
  expect(accountFiltersToQuery(result.current.filters)).toMatchObject({ account_type: 'oauth', auth_type: undefined });
});

it('issues new list requests for OAuth, API Key, both, and clearing the actual icon controls', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  function Harness() {
    const { filters, updateFilters } = useAccountFilters();
    const query = accountFiltersToQuery(filters);
    useQuery({ queryKey: ['accounts', query], queryFn: () => accountsApi.list({ page: 1, page_size: 20, ...query }) });
    return <AccountFilterIcons platforms={[{ key: 'openai', label: 'OpenAI' }]} selectedPlatforms={filters.platforms}
      selectedTypes={filters.accountTypes} platformLabel="平台" typeLabel="账号类型"
      onPlatformsChange={(platforms) => updateFilters({ platforms })} onTypesChange={(accountTypes) => updateFilters({ accountTypes })} />;
  }
  const user = userEvent.setup();
  render(<QueryClientProvider client={client}><Harness /></QueryClientProvider>);
  await waitFor(() => expect(api.get).toHaveBeenCalledTimes(1));
  expect(screen.getAllByRole('button', { pressed: true })).toHaveLength(3);
  for (const [name, expected] of [['OAuth', 'apikey'], ['API Key', NO_ACCOUNT_FILTER], ['OAuth', 'oauth'], ['API Key', undefined]] as const) {
    const calls = api.get.mock.calls.length;
    await user.click(screen.getByRole('button', { name }));
    await waitFor(() => expect(api.get.mock.calls.length).toBeGreaterThan(calls));
    expect(api.get).toHaveBeenLastCalledWith('/api/v1/admin/accounts', expect.objectContaining({ account_type: expected, auth_type: undefined }));
  }
  expect(screen.getAllByRole('button', { pressed: true })).toHaveLength(3);
  expect(JSON.parse(window.localStorage.getItem(storageKey)!).accountTypes).toEqual([]);
  await user.click(screen.getByRole('button', { name: 'OpenAI' }));
  await waitFor(() => expect(api.get).toHaveBeenLastCalledWith('/api/v1/admin/accounts', expect.objectContaining({ platform: NO_ACCOUNT_FILTER })));
  expect(screen.getByRole('button', { name: 'OpenAI' })).toHaveAttribute('aria-pressed', 'false');
  await user.click(screen.getByRole('button', { name: 'OpenAI' }));
  await waitFor(() => expect(api.get).toHaveBeenLastCalledWith('/api/v1/admin/accounts', expect.objectContaining({ platform: undefined })));
  client.clear();
});
