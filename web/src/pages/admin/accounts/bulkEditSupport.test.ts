import { describe, expect, it } from 'vitest';
import type { AccountResp } from '../../../shared/types';
import { getBulkEditInitialValues, orderSelectedAccountIdsByCreatedAt } from './bulkEditSupport';

function account(input: Partial<AccountResp> & Pick<AccountResp, 'id'>): AccountResp {
  return {
    id: input.id,
    name: input.name ?? `account-${input.id}`,
    email: input.email ?? null,
    platform: input.platform ?? 'openai',
    type: input.type ?? 'oauth',
    credentials: input.credentials ?? {},
    model_policy: input.model_policy ?? {},
    state: input.state ?? 'active',
    priority: input.priority ?? 50,
    max_concurrency: input.max_concurrency ?? 10,
    current_concurrency: input.current_concurrency ?? 0,
    rate_multiplier: input.rate_multiplier ?? 1,
    upstream_is_pool: input.upstream_is_pool ?? false,
    extra: input.extra,
    group_ids: input.group_ids ?? [],
    created_at: input.created_at ?? '2026-01-01T00:00:00Z',
    updated_at: input.updated_at ?? '2026-01-01T00:00:00Z',
  };
}

describe('bulk edit support', () => {
  it('orders selected accounts from oldest to newest for priority sequences', () => {
    const rows = [
      account({ id: 3, created_at: '2026-01-03T00:00:00Z' }),
      account({ id: 2, created_at: '2026-01-02T00:00:00Z' }),
      account({ id: 1, created_at: '2026-01-01T00:00:00Z' }),
    ];

    expect(orderSelectedAccountIdsByCreatedAt(rows, [3, 1, 2])).toEqual([1, 2, 3]);
  });

  it('prefills only groups shared by every selected account', () => {
    const rows = [
      account({ id: 1, group_ids: [10, 20] }),
      account({ id: 2, group_ids: [10, 20, 30] }),
    ];

    expect(getBulkEditInitialValues(rows, [1, 2]).groupIds).toEqual([10, 20]);
  });

  it('captures the selected priority range for offset validation', () => {
    const rows = [
      account({ id: 1, priority: -25 }),
      account({ id: 2, priority: 80 }),
    ];

    expect(getBulkEditInitialValues(rows, [1, 2])).toMatchObject({
      priority: undefined,
      priorityMin: -25,
      priorityMax: 80,
    });
  });
});
