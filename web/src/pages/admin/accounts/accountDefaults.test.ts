import { describe, expect, it } from 'vitest';
import {
  ACCOUNT_PRIORITY_MAX,
  ACCOUNT_PRIORITY_MIN,
  DEFAULT_ACCOUNT_PRIORITY,
  clampAccountPriority,
  commitAccountPriorityInput,
  getAccountGroupPriorities,
  getAccountMessageLockEnabled,
  isAccountPriorityDraft,
  parseAccountPriorityInput,
  setAccountGroupPriorities,
  setAccountMessageLockEnabled,
} from './accountDefaults';

describe('account default helpers', () => {
  it('parses, clamps and commits account priority input', () => {
    expect(clampAccountPriority(Number.NaN)).toBe(DEFAULT_ACCOUNT_PRIORITY);
    expect(clampAccountPriority(ACCOUNT_PRIORITY_MAX + 50)).toBe(ACCOUNT_PRIORITY_MAX);
    expect(clampAccountPriority(ACCOUNT_PRIORITY_MIN - 50)).toBe(ACCOUNT_PRIORITY_MIN);
    expect(isAccountPriorityDraft('-')).toBe(true);
    expect(isAccountPriorityDraft('-12')).toBe(true);
    expect(isAccountPriorityDraft('12.5')).toBe(false);
    expect(parseAccountPriorityInput('')).toBeNull();
    expect(parseAccountPriorityInput('-')).toBeNull();
    expect(parseAccountPriorityInput('12.6')).toBe(13);
    expect(commitAccountPriorityInput('bad', ACCOUNT_PRIORITY_MAX + 1)).toBe(ACCOUNT_PRIORITY_MAX);
  });

  it('reads and writes message lock flags in account extra data', () => {
    expect(getAccountMessageLockEnabled({ msg_lock_enabled: ' true ' })).toBe(true);
    expect(getAccountMessageLockEnabled({ msg_lock_enabled: '1' })).toBe(true);
    expect(getAccountMessageLockEnabled({ msg_lock_enabled: 1 })).toBe(true);
    expect(getAccountMessageLockEnabled({ msg_lock_enabled: true })).toBe(true);
    expect(getAccountMessageLockEnabled({ msg_lock_enabled: 'false' })).toBe(false);
    expect(setAccountMessageLockEnabled({ existing: 'value' }, true)).toEqual({
      existing: 'value',
      msg_lock_enabled: true,
    });
  });

  it('reads and writes per-group priority overrides in account extra data', () => {
    expect(getAccountGroupPriorities({
      group_priorities: {
        2: 88,
        3: 10000,
        bad: 1,
        4: '5',
      },
    })).toEqual({ 2: 88, 3: ACCOUNT_PRIORITY_MAX });

    expect(setAccountGroupPriorities({ existing: 'value' }, { 2: 10, 3: null })).toEqual({
      existing: 'value',
      group_priorities: { 2: 10 },
    });
    expect(setAccountGroupPriorities({ existing: 'value', group_priorities: { 2: 10 } }, {})).toEqual({
      existing: 'value',
    });
  });
});
