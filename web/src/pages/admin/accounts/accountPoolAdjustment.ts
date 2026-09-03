import type { AccountResp } from '../../../shared/types';
import type { AccountUsageWindow } from './accountUsageSupport';
import { getWindowSlot } from './accountUsageRows';

export const OPENAI_PLUGIN_ID = 'gateway-openai';
export const ACCOUNT_POOL_ADJUSTMENT_CONFIG_KEY = 'account_pool_adjustment_plans';
export const LEGACY_ACCOUNT_POOL_ADJUSTMENT_CONFIG_KEY = 'account_pool_adjustment';
export const ALL_ACCOUNT_POOL_ADJUSTMENT_PLANS = 'plus,team,k12,prolite,free';
export const ACCOUNT_POOL_ADJUSTMENT_SHOW_5H_CONFIG_KEY = 'account_pool_adjustment_show_5h';
export const ACCOUNT_POOL_ADJUSTMENT_SHOW_5H_LABEL = '显示';
export const ADJUSTED_USAGE_MODEL = 'gpt-5.3-codex-spark';

export type AccountPoolAdjustmentPlan = 'plus' | 'team' | 'k12' | 'prolite' | 'free';
type AccountPlan = AccountPoolAdjustmentPlan | 'pro';

type AccountDisplaySource = Pick<AccountResp, 'name' | 'platform' | 'type' | 'credentials'>;

const PLAN_NAME_TOKENS = new Set(['free', 'plus', 'pro', 'team', 'k12', 'prolite', 'oauth']);
const ADJUSTABLE_PLANS = new Set<AccountPoolAdjustmentPlan>(['plus', 'team', 'k12', 'prolite', 'free']);
const KNOWN_PLANS = new Set<AccountPlan>(['plus', 'team', 'k12', 'prolite', 'pro', 'free']);
const RESET_MODULO_SECONDS = 7 * 24 * 60 * 60;

export function parseAccountPoolAdjustmentPlans(value: unknown): ReadonlySet<AccountPoolAdjustmentPlan> {
  if (typeof value !== 'string') return new Set();
  if (value.trim().toLowerCase() === 'oauth_pro') {
    return new Set(ADJUSTABLE_PLANS);
  }
  const plans = value
    .split(',')
    .map((plan) => plan.trim().toLowerCase())
    .filter((plan): plan is AccountPoolAdjustmentPlan => ADJUSTABLE_PLANS.has(plan as AccountPoolAdjustmentPlan));
  return new Set(plans);
}

export function shouldShowAccountPoolAdjustedBaseFiveHour(value: unknown) {
  return typeof value === 'string' && value.trim().toLowerCase() === 'true';
}

function accountPlan(account: AccountDisplaySource): AccountPlan | '' {
  const tokens = (account.credentials.plan_type || '')
    .trim()
    .toLowerCase()
    .split(/[^a-z0-9]+/)
    .filter(Boolean);
  const plan = tokens.find((token): token is AccountPlan => (
    KNOWN_PLANS.has(token as AccountPlan)
  ));
  const subscriptionUntil = account.credentials.subscription_active_until;
  const subscriptionExpired = subscriptionUntil ? new Date(subscriptionUntil) < new Date() : false;
  if (plan && subscriptionExpired && (plan === 'plus' || plan === 'pro')) return 'free';
  if (plan) return plan;
  const hasQuotaMetadata = account.credentials.plan_type !== undefined
    || account.credentials.email !== undefined
    || subscriptionUntil !== undefined;
  return hasQuotaMetadata ? 'free' : '';
}

export function isAccountPoolProAdjusted(
  account: AccountDisplaySource,
  plans: ReadonlySet<AccountPoolAdjustmentPlan>,
) {
  if (account.platform !== 'openai' || account.type !== 'oauth') return false;
  const plan = accountPlan(account);
  return plan !== '' && plan !== 'pro' && plans.has(plan);
}

function proDisplayName(name: string) {
  const parts = name
    .split('-')
    .map((part) => part.trim())
    .filter((part) => part && !PLAN_NAME_TOKENS.has(part.toLowerCase()));
  return parts.length > 0 ? `Pro-${parts.join('-')}` : 'Pro';
}

function isAdjustedUsageModelGroup(group: string) {
  const model = group.replace(/^model:/, '').trim().toLowerCase();
  return model === 'spark' || model === ADJUSTED_USAGE_MODEL;
}

function moduloResetSeconds(seconds: number) {
  if (!Number.isFinite(seconds) || seconds <= RESET_MODULO_SECONDS) return seconds;
  return Math.max(0, Math.floor(seconds % RESET_MODULO_SECONDS));
}

function adjustedResetWindow(window: AccountUsageWindow): AccountUsageWindow {
  let resetSeconds: number | undefined;
  if (window.reset_at) {
    const resetAt = Date.parse(window.reset_at);
    if (Number.isFinite(resetAt)) {
      resetSeconds = Math.max(0, Math.floor((resetAt - Date.now()) / 1000));
    }
  }
  if (resetSeconds === undefined && typeof window.reset_seconds === 'number') {
    resetSeconds = window.reset_seconds;
  }
  if (resetSeconds === undefined && typeof window.reset_after_seconds === 'number') {
    resetSeconds = window.reset_after_seconds;
  }
  if (resetSeconds === undefined || resetSeconds <= RESET_MODULO_SECONDS) return window;

  const adjustedSeconds = moduloResetSeconds(resetSeconds);
  return {
    ...window,
    ...(window.reset_at ? { reset_at: new Date(Date.now() + adjustedSeconds * 1000).toISOString() } : {}),
    ...(window.reset_seconds !== undefined ? { reset_seconds: adjustedSeconds } : {}),
    ...(window.reset_after_seconds !== undefined ? { reset_after_seconds: adjustedSeconds } : {}),
  };
}

export function accountPoolDisplayName(
  account: AccountDisplaySource,
  plans: ReadonlySet<AccountPoolAdjustmentPlan>,
) {
  return isAccountPoolProAdjusted(account, plans)
    ? proDisplayName(account.name)
    : account.name;
}

export function accountPoolDisplayUsageWindows(
  account: AccountDisplaySource,
  windows: AccountUsageWindow[] | undefined,
  plans: ReadonlySet<AccountPoolAdjustmentPlan>,
  showBaseFiveHour: boolean,
): AccountUsageWindow[] | undefined {
  if (!Array.isArray(windows) || !isAccountPoolProAdjusted(account, plans)) return windows;

  const targetGroup = `model:${ADJUSTED_USAGE_MODEL}`;
  const baseWindows = new Map<string, AccountUsageWindow>();
  const resetAdjustedWindows = windows.map(adjustedResetWindow);
  for (const window of resetAdjustedWindows) {
    const { group, slot } = getWindowSlot(window);
    if (group === 'base' && (slot === '5h' || slot === '7d')) {
      baseWindows.set(slot, window);
    }
  }
  const adjustedWindows = resetAdjustedWindows.filter((window) => {
    const { group, slot } = getWindowSlot(window);
    if (group === 'base' && slot === '5h' && !showBaseFiveHour) return false;
    return !isAdjustedUsageModelGroup(group) || (slot !== '5h' && slot !== '7d');
  });

  const additions: AccountUsageWindow[] = [];
  for (const slot of ['5h', '7d'] as const) {
    const base = baseWindows.get(slot);
    additions.push({
      ...(base ?? {}),
      key: `model:${slot}:${ADJUSTED_USAGE_MODEL}`,
      label: `${slot} ${ADJUSTED_USAGE_MODEL}`,
      display_label: `${slot}S`,
      slot,
      group: targetGroup,
      used_percent: 0,
    });
  }

  return [...adjustedWindows, ...additions];
}
