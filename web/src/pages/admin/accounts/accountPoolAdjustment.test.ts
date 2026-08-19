import { describe, expect, it } from 'vitest';
import {
  accountPoolDisplayName,
  accountPoolDisplayUsageWindows,
  isAccountPoolProAdjusted,
  parseAccountPoolAdjustmentPlans,
} from './accountPoolAdjustment';

const oauthAccount = {
  name: '0819-Plus-1',
  platform: 'openai',
  type: 'oauth',
  credentials: { plan_type: 'ChatGPT Plus' },
};
const adjustedPlans = parseAccountPoolAdjustmentPlans('plus');

describe('account pool adjustment', () => {
  it('parses the selected adjustment plans', () => {
    expect([...parseAccountPoolAdjustmentPlans('plus, team,invalid,k12')]).toEqual(['plus', 'team', 'k12']);
    expect([...parseAccountPoolAdjustmentPlans('oauth_pro')]).toEqual(['plus', 'team', 'k12', 'free']);
    expect([...parseAccountPoolAdjustmentPlans('')]).toEqual([]);
  });

  it('only adjusts selected OpenAI OAuth plans', () => {
    expect(isAccountPoolProAdjusted(oauthAccount, adjustedPlans)).toBe(true);
    expect(isAccountPoolProAdjusted(
      { ...oauthAccount, type: 'apikey' },
      adjustedPlans,
    )).toBe(false);
    expect(isAccountPoolProAdjusted(
      { ...oauthAccount, credentials: { plan_type: 'Team' } },
      adjustedPlans,
    )).toBe(false);
    expect(isAccountPoolProAdjusted(
      { ...oauthAccount, credentials: { email: 'free@example.com' } },
      parseAccountPoolAdjustmentPlans('free'),
    )).toBe(true);
  });

  it('moves the Pro marker to the front of the displayed account name', () => {
    expect(accountPoolDisplayName(oauthAccount, adjustedPlans)).toBe('Pro-0819-1');
    expect(accountPoolDisplayName(oauthAccount, parseAccountPoolAdjustmentPlans(''))).toBe('0819-Plus-1');
  });

  it('adds zero-percent Spark windows while reusing the base reset times', () => {
    const windows = [
      { key: '5h', label: '5h', slot: '5h', group: 'base', used_percent: 12, reset_seconds: 60 },
      { key: '7d', label: '7d', slot: '7d', group: 'base', used_percent: 34, reset_seconds: 120 },
    ];

    const displayed = accountPoolDisplayUsageWindows(
      oauthAccount,
      windows,
      adjustedPlans,
    );

    expect(displayed).toHaveLength(4);
    expect(displayed?.slice(2)).toEqual([
      expect.objectContaining({
        key: 'model:5h:gpt-5.3-codex-spark',
        group: 'model:gpt-5.3-codex-spark',
        slot: '5h',
        display_label: '5hS',
        used_percent: 0,
        reset_seconds: 60,
      }),
      expect.objectContaining({
        key: 'model:7d:gpt-5.3-codex-spark',
        group: 'model:gpt-5.3-codex-spark',
        slot: '7d',
        display_label: '7dS',
        used_percent: 0,
        reset_seconds: 120,
      }),
    ]);
    expect(windows).toHaveLength(2);
  });

  it('keeps Spark 5h visible without a base 5h window and leaves its reset empty', () => {
    const displayed = accountPoolDisplayUsageWindows(
      oauthAccount,
      [{ key: '7d', label: '7d', slot: '7d', group: 'base', used_percent: 34 }],
      adjustedPlans,
    );

    expect(displayed).toHaveLength(3);
    expect(displayed?.[1]).toEqual(expect.objectContaining({
      key: 'model:5h:gpt-5.3-codex-spark',
      display_label: '5hS',
      used_percent: 0,
    }));
    expect(displayed?.[1]?.reset_at).toBeUndefined();
    expect(displayed?.[1]?.reset_seconds).toBeUndefined();
    expect(displayed?.[2]).toEqual(expect.objectContaining({
      key: 'model:7d:gpt-5.3-codex-spark',
      display_label: '7dS',
      used_percent: 0,
    }));
  });

  it('replaces existing Spark values with zero and the matching base reset times', () => {
    const windows = [
      { key: '5h', label: '5h', slot: '5h', group: 'base', used_percent: 12, reset_seconds: 60 },
      { key: '7d', label: '7d', slot: '7d', group: 'base', used_percent: 34, reset_seconds: 120 },
      {
        key: 'model:5h:spark',
        label: '5h spark',
        slot: '5h',
        group: 'model:spark',
        used_percent: 56,
        reset_seconds: 999,
      },
    ];

    const displayed = accountPoolDisplayUsageWindows(
      oauthAccount,
      windows,
      adjustedPlans,
    );

    expect(displayed).toHaveLength(4);
    const spark5h = displayed?.filter((window) => window.slot === '5h' && window.group?.includes('spark'));
    expect(spark5h).toHaveLength(1);
    expect(spark5h?.[0]?.display_label).toBe('5hS');
    expect(spark5h?.[0]).toEqual(expect.objectContaining({ used_percent: 0, reset_seconds: 60 }));
    expect(displayed?.find((window) => window.slot === '7d' && window.group?.includes('spark'))).toEqual(
      expect.objectContaining({ display_label: '7dS', used_percent: 0, reset_seconds: 120 }),
    );
  });

  it('applies a seven-day modulo to long Free reset times', () => {
    const freeAccount = {
      ...oauthAccount,
      name: '0819-Free-1',
      credentials: { plan_type: 'free' },
    };
    const thirtyDaysAndOneHour = 30 * 24 * 60 * 60 + 60 * 60;
    const twoDaysAndOneHour = 2 * 24 * 60 * 60 + 60 * 60;
    const displayed = accountPoolDisplayUsageWindows(
      freeAccount,
      [{
        key: '7d',
        label: '7d',
        slot: '7d',
        group: 'base',
        used_percent: 34,
        reset_seconds: thirtyDaysAndOneHour,
      }],
      parseAccountPoolAdjustmentPlans('free'),
    );

    expect(displayed?.[0]?.reset_seconds).toBe(twoDaysAndOneHour);
    expect(displayed?.find((window) => window.display_label === '7dS')?.reset_seconds).toBe(twoDaysAndOneHour);
    expect(displayed?.find((window) => window.display_label === '5hS')?.reset_seconds).toBeUndefined();
  });
});
