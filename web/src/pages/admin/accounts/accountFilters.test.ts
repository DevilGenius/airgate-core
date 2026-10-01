import { expect, it } from 'vitest';
import { accountFiltersToQuery, normalizeAccountFilters, parseAccountPlanFilterIds } from './accountFilters';
import { NO_ACCOUNT_FILTER } from './accountFilterConstants';

it('filters OAuth by plan while preserving the selected API Key branch', () => {
  const filters = normalizeAccountFilters({ accountTypes: ['apikey', 'oauth'],
    plans: [{ platform: 'openai', key: 'plus' }, { platform: 'openai', key: 'team' }],
    groupIds: [3, 1], ungrouped: true, states: ['active', 'working'], proxyIds: [9], platforms: ['openai'], prioritySort: 'asc' });
  expect(accountFiltersToQuery(filters)).toEqual({ platform: 'openai', state: 'working,active',
    account_type: 'oauth_plan:openai:plus,oauth_plan:openai:team,apikey', auth_type: undefined,
    group_id: '1,3', ungrouped: true, proxy_id: '9', sort_by: 'priority', sort_dir: 'asc' });
  const clearedPlan = accountFiltersToQuery({ ...filters, plans: [] });
  expect(clearedPlan.account_type).toBe('oauth,apikey');
  expect(clearedPlan.auth_type).toBeUndefined();
  expect(filters.plans).toHaveLength(2);
});

it('applies plans only when OAuth is enabled and preserves explicit empty selections', () => {
  const filters = normalizeAccountFilters({ plans: [{ platform: 'openai', key: 'plus' }] });
  expect(accountFiltersToQuery(filters).account_type).toBe('oauth_plan:openai:plus,apikey');
  expect(accountFiltersToQuery({ ...filters, accountTypes: ['apikey'] }).account_type).toBe('apikey');
  expect(accountFiltersToQuery({ ...filters, accountTypes: ['oauth'] }).account_type).toBe('oauth_plan:openai:plus');
  const empty = normalizeAccountFilters({ ...filters, platforms: [NO_ACCOUNT_FILTER], accountTypes: [NO_ACCOUNT_FILTER] });
  expect(accountFiltersToQuery(empty)).toMatchObject({ platform: NO_ACCOUNT_FILTER, account_type: NO_ACCOUNT_FILTER });
  expect(normalizeAccountFilters(JSON.parse(JSON.stringify(empty)))).toEqual(empty);
});

it('validates persisted values and keeps unknown but well-formed plugin plans', () => {
  const filters = normalizeAccountFilters({ platforms: ['kiro', 'openai', 'openai', null, 'bad,platform'],
    accountTypes: ['oauth', 'oauth', 'invalid'], groupIds: [1, 1, -1, '3'], states: ['unknown', 'disabled'],
    proxyIds: [2, 1.2, null], prioritySort: 'invalid', plans: [null, { platform: 'custom', key: 'future' }, { platform: '', key: 'x' }] });
  expect(filters).toEqual({ version: 1, keyword: '', platforms: ['openai', 'kiro'], accountTypes: ['oauth'],
    plans: [{ platform: 'custom', key: 'future' }], groupIds: [1], ungrouped: false, states: ['disabled'], proxyIds: [2], prioritySort: '' });
  expect(parseAccountPlanFilterIds(['oauth', 'oauth_plan:kiro:pro', 'oauth_plan::bad', 'oauth_plan:kiro:'])).toEqual([{ platform: 'kiro', key: 'pro' }]);
});
