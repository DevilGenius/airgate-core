import { beforeEach, describe, expect, it, vi } from 'vitest';
import { accountsApi } from './accounts';
import { apikeysApi } from './apikeys';
import { authApi } from './auth';
import { del, get, patch, post, put } from './client';

vi.mock('./client', () => ({
  del: vi.fn(),
  get: vi.fn(),
  patch: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
}));

describe('authApi', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('maps auth methods to backend endpoints', () => {
    authApi.login({ email: 'a@example.com', password: 'pw' });
    authApi.loginByAPIKey({ key: 'sk-test' });
    authApi.register({ email: 'b@example.com', password: 'secret', username: 'bee' });
    authApi.refresh();
    authApi.sendVerifyCode('c@example.com');
    authApi.verifyCode('c@example.com', '123456');

    expect(post).toHaveBeenNthCalledWith(1, '/api/v1/auth/login', { email: 'a@example.com', password: 'pw' });
    expect(post).toHaveBeenNthCalledWith(2, '/api/v1/auth/login-apikey', { key: 'sk-test' });
    expect(post).toHaveBeenNthCalledWith(3, '/api/v1/auth/register', { email: 'b@example.com', password: 'secret', username: 'bee' });
    expect(post).toHaveBeenNthCalledWith(4, '/api/v1/auth/refresh');
    expect(post).toHaveBeenNthCalledWith(5, '/api/v1/auth/send-verify-code', { email: 'c@example.com' });
    expect(post).toHaveBeenNthCalledWith(6, '/api/v1/auth/verify-code', { email: 'c@example.com', code: '123456' });
  });
});

describe('apikeysApi', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('maps user and admin API key operations', () => {
    const signal = new AbortController().signal;
    apikeysApi.list({ page: 1, page_size: 20, include_usage: true }, { signal });
    apikeysApi.create({ group_id: 3, name: 'key', quota_usd: 10 });
    apikeysApi.update(4, { name: 'renamed' });
    apikeysApi.delete(5);
    apikeysApi.reveal(6);
    apikeysApi.adminList({ page: 2, page_size: 50, search_scope: 'api_key' }, { signal });
    apikeysApi.adminUpdate(7, { status: 'disabled' });
    apikeysApi.adminResetUsage(8);

    expect(get).toHaveBeenNthCalledWith(1, '/api/v1/api-keys', { page: 1, page_size: 20, include_usage: true }, { signal });
    expect(post).toHaveBeenNthCalledWith(1, '/api/v1/api-keys', { group_id: 3, name: 'key', quota_usd: 10 });
    expect(put).toHaveBeenNthCalledWith(1, '/api/v1/api-keys/4', { name: 'renamed' });
    expect(del).toHaveBeenNthCalledWith(1, '/api/v1/api-keys/5');
    expect(get).toHaveBeenNthCalledWith(2, '/api/v1/api-keys/6/reveal');
    expect(get).toHaveBeenNthCalledWith(3, '/api/v1/admin/api-keys', { page: 2, page_size: 50, search_scope: 'api_key' }, { signal });
    expect(put).toHaveBeenNthCalledWith(2, '/api/v1/admin/api-keys/7', { status: 'disabled' });
    expect(post).toHaveBeenNthCalledWith(2, '/api/v1/admin/api-keys/8/reset-usage');
  });
});

describe('accountsApi', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('maps account list, export, mutation and runtime endpoints', () => {
    accountsApi.list({ page: 1, page_size: 10, platform: 'openai', sort_by: 'priority', sort_dir: 'desc' });
    accountsApi.export({ ids: [3, 4], keyword: 'prod', platform: 'claude' });
    accountsApi.import([{ credentials: { api_key: 'sk' }, max_concurrency: 10, name: 'one', platform: 'openai', priority: 50 }]);
    accountsApi.create({ credentials: {}, name: 'new', platform: 'openai' });
    accountsApi.update(2, { name: 'updated' });
    accountsApi.delete(3);
    accountsApi.toggleScheduling(4);
    accountsApi.clearFamilyCooldowns(5);
    accountsApi.bulkClearFamilyCooldowns([6, 7]);
    accountsApi.models(8);
    accountsApi.usage('openai', [9, 10], { refresh: true });
    accountsApi.capacity([11, 12]);
    accountsApi.usageOne(13, { signal: new AbortController().signal });
    accountsApi.credentialsSchema('claude');
    accountsApi.refreshQuota(14);
    accountsApi.bulkUpdate({ account_ids: [15], priority: 20 });
    accountsApi.bulkDelete([16]);
    accountsApi.stats(17, { end_date: '2026-06-20', start_date: '2026-06-01' });

    expect(get).toHaveBeenNthCalledWith(1, '/api/v1/admin/accounts', { page: 1, page_size: 10, platform: 'openai', sort_by: 'priority', sort_dir: 'desc' });
    expect(get).toHaveBeenNthCalledWith(2, '/api/v1/admin/accounts/export', { ids: '3,4', keyword: 'prod', platform: 'claude' });
    expect(post).toHaveBeenNthCalledWith(1, '/api/v1/admin/accounts/import', { version: 2, accounts: [{ credentials: { api_key: 'sk' }, max_concurrency: 10, name: 'one', platform: 'openai', priority: 50 }] });
    expect(post).toHaveBeenNthCalledWith(2, '/api/v1/admin/accounts', { credentials: {}, name: 'new', platform: 'openai' });
    expect(put).toHaveBeenNthCalledWith(1, '/api/v1/admin/accounts/2', { name: 'updated' });
    expect(del).toHaveBeenNthCalledWith(1, '/api/v1/admin/accounts/3');
    expect(patch).toHaveBeenNthCalledWith(1, '/api/v1/admin/accounts/4/toggle');
    expect(del).toHaveBeenNthCalledWith(2, '/api/v1/admin/accounts/5/family-cooldowns');
    expect(post).toHaveBeenNthCalledWith(3, '/api/v1/admin/accounts/bulk-clear-family-cooldowns', { account_ids: [6, 7] });
    expect(get).toHaveBeenNthCalledWith(3, '/api/v1/admin/accounts/8/models');
    expect(get).toHaveBeenNthCalledWith(4, '/api/v1/admin/accounts/usage', { ids: '9,10', platform: 'openai', refresh: 'true' });
    expect(get).toHaveBeenNthCalledWith(5, '/api/v1/admin/accounts/capacity', { ids: '11,12' });
    expect(get).toHaveBeenNthCalledWith(6, '/api/v1/admin/accounts/13/usage', undefined, expect.any(Object));
    expect(get).toHaveBeenNthCalledWith(7, '/api/v1/admin/accounts/credentials-schema/claude');
    expect(post).toHaveBeenNthCalledWith(4, '/api/v1/admin/accounts/14/refresh-quota');
    expect(post).toHaveBeenNthCalledWith(5, '/api/v1/admin/accounts/bulk-update', { account_ids: [15], priority: 20 });
    expect(post).toHaveBeenNthCalledWith(6, '/api/v1/admin/accounts/bulk-delete', { account_ids: [16] });
    expect(get).toHaveBeenNthCalledWith(8, '/api/v1/admin/accounts/17/stats', { end_date: '2026-06-20', start_date: '2026-06-01' });
    expect(accountsApi.testUrl(18)).toBe('/api/v1/admin/accounts/18/test');
    expect(accountsApi.bulkRefreshQuotaUrl()).toBe('/api/v1/admin/accounts/bulk-refresh-quota');
  });
});
