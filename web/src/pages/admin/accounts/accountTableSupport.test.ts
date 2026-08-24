import { describe, expect, it } from 'vitest';
import type { AccountResp } from '../../../shared/types';
import { accountTableCellRowsEqual } from './accountTableSupport';

const account: AccountResp = {
  id: 1,
  name: 'oauth-account',
  email: null,
  platform: 'openai',
  type: 'oauth',
  credentials: { access_token: 'token' },
  model_policy: {},
  state: 'active',
  priority: 0,
  max_concurrency: 4,
  current_concurrency: 0,
  rate_multiplier: 1,
  upstream_is_pool: false,
  group_ids: [],
  last_used_at: '2026-08-25T01:00:00Z',
  usage_5h_growth_date: '2026-08-25',
  usage_5h_daily_growth: 10,
  usage_5h_observed_at: '2026-08-25T01:00:00Z',
  usage_7d_growth_date: '2026-08-25',
  usage_7d_daily_growth: 20,
  usage_7d_observed_at: '2026-08-25T01:00:00Z',
  created_at: '',
  updated_at: '',
};

describe('accountTableCellRowsEqual', () => {
  it('invalidates recent usage when only the 7d observation timestamp changes', () => {
    expect(accountTableCellRowsEqual('last_used_at', account, {
      ...account,
      usage_7d_observed_at: '2026-08-26T01:00:00Z',
    })).toBe(false);
  });
});
