import { describe, expect, it } from 'vitest';
import type { AccountResp } from '../../../shared/types';
import { accountChangeNeedsListRefresh, applyAccountChangeToRow } from './accountChangeEvents';

const row: AccountResp = {
  id: 7, state: 'active', priority: 50, scheduling_weight: 100,
  name: 'test', email: null, platform: 'openai', type: 'oauth',
  credentials: {}, model_policy: {}, max_concurrency: 4, current_concurrency: 0,
  rate_multiplier: 1, upstream_is_pool: false, group_ids: [], created_at: '', updated_at: '',
  extra: { other: 'kept', cognition_degraded: true },
};

describe('account change patches', () => {
  it('requests list recalculation only for threshold configuration changes, including zero', () => {
    const event = { type: 'account.changed', model_downgrade_threshold: 0 };
    expect(accountChangeNeedsListRefresh(event)).toBe(true);
    expect(applyAccountChangeToRow(row, event).model_downgrade_threshold).toBe(0);
    expect(accountChangeNeedsListRefresh({ type: 'account.changed', priority: 1 })).toBe(false);
    expect(accountChangeNeedsListRefresh({ type: 'account.changed', model_downgrade_threshold: NaN })).toBe(false);
  });
  it('updates capacity without replacing model statistics from the list', () => {
    const demotions = [{ model: 'gpt', success_rate: 0.2, valid_requests: 10 }];
    const next = applyAccountChangeToRow({ ...row, model_demotions: demotions }, { type: 'account.changed', max_concurrency: 0 });
    expect(next.max_concurrency).toBe(0);
    expect(next.model_demotions).toBe(demotions);
    expect(next.state).toBe(row.state);
  });
  it('applies zero priority/weight and false cognition without losing unrelated metadata', () => {
    const next = applyAccountChangeToRow(row, {
      type: 'account.changed', priority: 0, scheduling_weight: 0, cognition: { degraded: false },
    });
    expect(next).toMatchObject({ priority: 0, scheduling_weight: 0, state: 'active', extra: { other: 'kept', cognition_degraded: false } });
    expect(row.extra?.cognition_degraded).toBe(true);
  });
  it('distinguishes absent cognition from an explicit clear', () => {
    expect(applyAccountChangeToRow(row, { type: 'account.changed', priority: 1 }).extra).toBe(row.extra);
    expect(applyAccountChangeToRow(row, { type: 'account.changed', cognition: { degraded: null } }).extra).toEqual({ other: 'kept' });
  });
  it('merges state, family cooldown and config updates in order', () => {
    const until = new Date(Date.now() + 60000).toISOString();
    const events = [
      { type: 'account.changed', state: 'disabled', state_until: '', error_msg: 'manual' },
      { type: 'account.changed', priority: 10 },
      { type: 'account.changed', family_cooldown_action: 'clear' as const },
      { type: 'account.changed', family_cooldown_action: 'upsert' as const, family: 'gpt', family_until: until },
    ];
    const next = events.reduce(applyAccountChangeToRow, row);
    expect(next).toMatchObject({ state: 'disabled', error_msg: 'manual', priority: 10, family_cooldowns: [{ family: 'gpt', until }] });
    expect(next.state_until).toBeUndefined();
  });
});
