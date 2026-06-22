import { describe, expect, it } from 'vitest';
import type { AccountUsageWindow } from './accountUsageSupport';
import { buildWindowRows, countUsageWindowGroups, getWindowSlot, shouldExpandUsageWindows } from './accountUsageRows';

// Build a normalized usage window the way the backend / mergeCachedUsageWindows do
// (explicit slot + group), so buildWindowRows is exercised deterministically.
function win(slot: string, group: string, usedPercent = 10): AccountUsageWindow {
  const key = group === 'base' ? slot : `${group}:${slot}`;
  return { key, label: slot, slot, group, used_percent: usedPercent };
}

describe('buildWindowRows group ordering', () => {
  it('pairs 5h/7d per group with the base group first, then model groups', () => {
    // Flat order as produced by sortUsageWindows: all 5h, then all 7d.
    const rows = buildWindowRows([
      win('5h', 'base'),
      win('5h', 'model:opus'),
      win('7d', 'base'),
      win('7d', 'model:opus'),
    ]);
    expect(rows.map((r) => r.id)).toEqual([
      'base:5h',
      'base:7d',
      'model:opus:5h',
      'model:opus:7d',
    ]);
  });

  it('keeps base 7d at the top when base 5h has expired but a model 5h survives (regression)', () => {
    // base 5h dropped; flat order = remaining 5h (opus) first, then 7d windows.
    const rows = buildWindowRows([
      win('5h', 'model:opus'),
      win('7d', 'base'),
      win('7d', 'model:opus'),
    ]);
    const ids = rows.map((r) => r.id);
    // The bug: base:7d used to be pushed to the very bottom.
    expect(ids[0]).toBe('base:7d');
    expect(ids[ids.length - 1]).not.toBe('base:7d');
    expect(ids).toEqual(['base:7d', 'model:opus:5h', 'model:opus:7d']);
  });

  it('is independent of input order', () => {
    const scrambled = buildWindowRows([
      win('7d', 'model:opus'),
      win('5h', 'base'),
      win('7d', 'base'),
      win('5h', 'model:opus'),
    ]);
    expect(scrambled.map((r) => r.id)).toEqual([
      'base:5h',
      'base:7d',
      'model:opus:5h',
      'model:opus:7d',
    ]);
  });

  it('handles a base-only account that loses its 5h window without crashing', () => {
    const rows = buildWindowRows([win('7d', 'base')]);
    expect(rows.map((r) => r.id)).toEqual(['base:7d']);
  });

  it('orders multiple model groups alphabetically after base', () => {
    const rows = buildWindowRows([
      win('5h', 'model:sonnet'),
      win('5h', 'model:opus'),
      win('7d', 'base'),
    ]);
    expect(rows.map((r) => r.id)).toEqual([
      'base:7d',
      'model:opus:5h',
      'model:sonnet:5h',
    ]);
  });
});

describe('shouldExpandUsageWindows / countUsageWindowGroups', () => {
  it('counts distinct window groups', () => {
    expect(countUsageWindowGroups([win('5h', 'base'), win('7d', 'base')])).toBe(1);
    expect(countUsageWindowGroups([win('7d', 'base'), win('7d', 'model:opus')])).toBe(2);
  });

  it('keeps a Pro account expanded when only the two 7d bars remain after 5h expiry', () => {
    // 5h windows expired/dropped; base 7d + model 7d survive -> still 2 groups.
    const remaining = [win('7d', 'base'), win('7d', 'model:opus')];
    expect(remaining.length).toBe(2);
    expect(shouldExpandUsageWindows(remaining)).toBe(true);
  });

  it('does not expand a free single-group account with only 5h + 7d', () => {
    expect(shouldExpandUsageWindows([win('5h', 'base'), win('7d', 'base')])).toBe(false);
  });

  it('expands a single-group account that has more than two windows', () => {
    expect(shouldExpandUsageWindows([
      win('5h', 'base'),
      win('7d', 'base'),
      { key: 'monthly', label: 'monthly', slot: 'monthly', group: 'base', used_percent: 5 },
    ])).toBe(true);
  });

  it('handles missing / non-array input', () => {
    expect(shouldExpandUsageWindows(undefined)).toBe(false);
    expect(shouldExpandUsageWindows([])).toBe(false);
  });
});

describe('getWindowSlot', () => {
  it('honors explicit slot/group', () => {
    expect(getWindowSlot({ label: '5h', slot: '5h', group: 'base', used_percent: 0 })).toEqual({
      slot: '5h',
      group: 'base',
    });
  });

  it('falls back to inferring slot/group from key and label', () => {
    expect(getWindowSlot({ label: '7d', key: '7d', used_percent: 0 }).slot).toBe('7d');
    expect(getWindowSlot({ label: 'Monthly', key: 'monthly', used_percent: 0 }).slot).toBe('monthly');
    expect(getWindowSlot({ label: 'x', key: 'model:5h:opus', used_percent: 0 }).group).toBe('model:opus');
    expect(getWindowSlot({ label: 'Monthly', key: 'model:monthly:opus', used_percent: 0 })).toEqual({
      slot: 'monthly',
      group: 'model:opus',
    });
    // Unknown patterns default to the 5h slot and base group.
    expect(getWindowSlot({ label: 'whatever', used_percent: 0 })).toEqual({ slot: '5h', group: 'base' });
  });
});
