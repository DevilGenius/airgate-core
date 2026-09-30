import type { AccountListFilter } from '../../../shared/api/accounts';
import { compareAccountFilterPlatforms } from './accountFilterOrder';

export const ACCOUNT_FILTER_STATES = ['working', 'active', 'family_limited', 'rate_limited', 'degraded', 'disabled'] as const;
export type AccountFilters = {
  version: 1;
  keyword: string;
  platforms: string[];
  accountTypes: string[];
  plans: { platform: string; key: string }[];
  groupIds: number[];
  ungrouped: boolean;
  states: string[];
  proxyIds: number[];
  prioritySort: '' | 'asc' | 'desc';
};

const strings = (value: unknown): string[] => Array.isArray(value)
  ? [...new Set(value.filter((item): item is string => typeof item === 'string').map((item) => item.trim()).filter(Boolean))] : [];
const ids = (value: unknown): number[] => Array.isArray(value)
  ? [...new Set(value.filter((item): item is number => typeof item === 'number' && Number.isSafeInteger(item) && item > 0))].sort((a, b) => a - b) : [];

export function normalizeAccountFilters(value: unknown): AccountFilters {
  const raw = value && typeof value === 'object' ? value as Record<string, unknown> : {};
  const plans = new Map<string, AccountFilters['plans'][number]>();
  if (Array.isArray(raw.plans)) {
    for (const plan of raw.plans) {
      if (!plan || typeof plan !== 'object' || typeof plan.platform !== 'string' || typeof plan.key !== 'string') continue;
      const platform = plan.platform.trim();
      const key = plan.key.trim();
      if (platform && key && !/[,:]/.test(platform) && !/[,:]/.test(key)) plans.set(`${platform}:${key}`, { platform, key });
    }
  }
  const types = new Set(strings(raw.accountTypes));
  const states = new Set(strings(raw.states));
  return {
    version: 1,
    keyword: typeof raw.keyword === 'string' ? raw.keyword : '',
    platforms: strings(raw.platforms).filter((key) => !/[,:]/.test(key)).sort(compareAccountFilterPlatforms),
    accountTypes: ['oauth', 'apikey'].filter((key) => types.has(key)),
    plans: [...plans.values()].sort((a, b) => compareAccountFilterPlatforms(a.platform, b.platform) || a.key.localeCompare(b.key, 'en')),
    groupIds: ids(raw.groupIds),
    ungrouped: raw.ungrouped === true,
    states: ACCOUNT_FILTER_STATES.filter((key) => states.has(key)),
    proxyIds: ids(raw.proxyIds),
    prioritySort: raw.prioritySort === 'asc' || raw.prioritySort === 'desc' ? raw.prioritySort : '',
  };
}

export function accountPlanFilterId(plan: AccountFilters['plans'][number]): string {
  return `oauth_plan:${plan.platform}:${plan.key}`;
}

export function parseAccountPlanFilterIds(values: readonly string[]): AccountFilters['plans'] {
  return values.flatMap((value) => {
    const parts = value.split(':');
    return parts.length === 3 && parts[0] === 'oauth_plan' && parts[1] && parts[2]
      ? [{ platform: parts[1], key: parts[2] }] : [];
  });
}

// Both the list and export use this encoder. Ordinary auth filters use the
// established account_type contract; plan intersections additionally use auth_type.
export function accountFiltersToQuery(filters: AccountFilters): AccountListFilter {
  const types = filters.accountTypes.join(',');
  const plans = filters.plans.map(accountPlanFilterId).join(',');
  return {
    platform: filters.platforms.join(',') || undefined,
    state: filters.states.join(',') || undefined,
    account_type: plans || types || undefined,
    auth_type: plans && types ? types : undefined,
    group_id: filters.groupIds.join(',') || undefined,
    ungrouped: filters.ungrouped || undefined,
    proxy_id: filters.proxyIds.join(',') || undefined,
    sort_by: filters.prioritySort ? 'priority' : undefined,
    sort_dir: filters.prioritySort || undefined,
  };
}
