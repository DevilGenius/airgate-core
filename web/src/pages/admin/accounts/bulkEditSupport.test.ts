import { describe, expect, it } from 'vitest';
import type { AccountResp } from '../../../shared/types';
import { getBulkEditInitialValues } from './bulkEditSupport';

function account(input: Partial<AccountResp> & Pick<AccountResp, 'id'>): AccountResp {
  return {
    id: input.id,
    name: input.name ?? `account-${input.id}`,
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

describe('getBulkEditInitialValues', () => {
  it('prefills common group priorities only when every selected account matches', () => {
    const rows = [
      account({
        id: 1,
        group_ids: [10, 20],
        extra: { group_priorities: { 10: 80, 20: 30 } },
      }),
      account({
        id: 2,
        group_ids: [10, 20, 30],
        extra: { group_priorities: { 10: 80, 20: 40, 30: 90 } },
      }),
    ];

    expect(getBulkEditInitialValues(rows, [1, 2])).toMatchObject({
      groupIds: [10, 20],
      groupPriorities: { 10: 80 },
    });
  });

  it('leaves group priorities empty when the common group has no shared override', () => {
    const rows = [
      account({ id: 1, group_ids: [10], extra: { group_priorities: { 10: 80 } } }),
      account({ id: 2, group_ids: [10], extra: {} }),
    ];

    expect(getBulkEditInitialValues(rows, [1, 2]).groupPriorities).toEqual({});
  });
});
