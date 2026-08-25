import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { ReactNode } from 'react';
import { isObservedToday, useAccountTableColumns } from './useAccountTableColumns';
import { AccountCapacityStore, type AccountUsageData } from './AccountPageSupport';
import { parseAccountPoolAdjustmentPlans } from './accountPoolAdjustment';
import { queryKeys } from '../../../shared/queryKeys';
import type { AccountResp } from '../../../shared/types';

const mocks = vi.hoisted(() => ({
  refreshToken: vi.fn(),
  toast: vi.fn(),
  usageOne: vi.fn(),
}));

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string, fallback?: string | Record<string, unknown>) => (
      typeof fallback === 'string' ? fallback : key
    ),
  }),
}));

vi.mock('../../../shared/ui', () => ({
  useToast: () => ({ toast: mocks.toast }),
}));

vi.mock('../../../shared/api/accounts', () => ({
  accountsApi: {
    refreshToken: mocks.refreshToken,
    usageOne: mocks.usageOne,
  },
}));

const account: AccountResp = {
  id: 1,
  name: 'oauth-account',
  email: null,
  platform: 'openai',
  type: 'oauth',
  credentials: { access_token: 'token', plan_type: 'free' },
  model_policy: {},
  state: 'active',
  priority: 0,
  max_concurrency: 4,
  current_concurrency: 0,
  rate_multiplier: 1,
  upstream_is_pool: false,
  group_ids: [],
  created_at: '',
  updated_at: '',
};

function Harness({ usageData }: { usageData: AccountUsageData }) {
  const { columns, rowMetaById } = useAccountTableColumns({
    accountPoolAdjustmentPlans: parseAccountPoolAdjustmentPlans(''),
    showAccountPoolAdjustedBaseFiveHour: false,
    capacityStore: new AccountCapacityStore(),
    groupMap: new Map(),
    onClearRateLimitMarkers: vi.fn(),
    onDeleteAccount: vi.fn(),
    onEditAccount: vi.fn(),
    onRefreshToken: vi.fn(),
    onStatsAccount: vi.fn(),
    onTestAccount: vi.fn(),
    onToggleScheduling: vi.fn(),
    platformFilter: 'openai',
    platformName: (platform) => platform,
    platformsKey: 'openaiopenai',
    rows: [account],
    usageData,
  });
  const usageColumn = columns.find((column) => column.key === 'usage_window');
  if (!usageColumn) throw new Error('usage column not found');
  return usageColumn.render(account, rowMetaById.get(account.id)) as ReactNode;
}

function RecentUsageHarness({ row }: { row: AccountResp }) {
  const { columns } = useAccountTableColumns({
    accountPoolAdjustmentPlans: parseAccountPoolAdjustmentPlans(''),
    showAccountPoolAdjustedBaseFiveHour: false,
    capacityStore: new AccountCapacityStore(),
    groupMap: new Map(),
    onClearRateLimitMarkers: vi.fn(),
    onDeleteAccount: vi.fn(),
    onEditAccount: vi.fn(),
    onRefreshToken: vi.fn(),
    onStatsAccount: vi.fn(),
    onTestAccount: vi.fn(),
    onToggleScheduling: vi.fn(),
    platformFilter: 'openai',
    platformName: (platform) => platform,
    platformsKey: 'openaiopenai',
    rows: [row],
    usageData: { accounts: {} },
  });
  const recentUsageColumn = columns.find((column) => column.key === 'last_used_at');
  if (!recentUsageColumn) throw new Error('recent usage column not found');
  return recentUsageColumn.render(row) as ReactNode;
}

describe('useAccountTableColumns usage refresh', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.usageOne.mockResolvedValue({
      windows: [{ key: '5h', label: '5h', used_percent: 75, reset_after_seconds: 1800 }],
    });
  });

  it('compares observed ISO timestamps in the browser local timezone', () => {
    const browserNow = new Date(2026, 7, 25, 0, 30);
    const observedToday = new Date(2026, 7, 25, 0, 30).toISOString();
    const observedYesterday = new Date(2026, 7, 24, 23, 30).toISOString();
    expect(isObservedToday(observedToday, browserNow)).toBe(true);
    expect(isObservedToday(observedYesterday, browserNow)).toBe(false);
  });

  it('refreshes usage without invoking the token refresh flow', async () => {
    const client = new QueryClient();
    const usageKey = queryKeys.accountUsage('openai', '1');
    client.setQueryData(usageKey, {
      accounts: {
        '1': { windows: [{ key: '5h', label: '5h', used_percent: 10 }] },
      },
    });

    render(
      <QueryClientProvider client={client}>
        <Harness
          usageData={{
            accounts: {
              '1': { windows: [{ key: '5h', label: '5h', used_percent: 10 }] },
            },
          }}
        />
      </QueryClientProvider>,
    );

    fireEvent.click(screen.getByTitle('点击刷新用量'));

    await waitFor(() => expect(mocks.usageOne).toHaveBeenCalledWith(1, { refresh: true }));
    expect(mocks.refreshToken).not.toHaveBeenCalled();
    expect(mocks.toast).toHaveBeenCalledWith('success', '用量刷新成功');
    expect(client.getQueryData<AccountUsageData>(usageKey)?.accounts?.['1']?.windows?.[0]?.used_percent).toBe(75);
  });

  it('shows daily 5h and 7d growth without the probe timestamp', () => {
    const now = new Date();
    const pad = (part: number) => String(part).padStart(2, '0');
    const today = `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
    const row: AccountResp = {
      ...account,
      last_used_at: now.toISOString(),
      last_probe_at: new Date(now.getTime() - 60_000).toISOString(),
      usage_5h_growth_date: today,
      usage_5h_daily_growth: 65,
      usage_5h_observed_at: now.toISOString(),
      usage_7d_growth_date: today,
      usage_7d_daily_growth: 16,
      usage_7d_observed_at: now.toISOString(),
    };

    const { container } = render(
      <QueryClientProvider client={new QueryClient()}>
        <RecentUsageHarness row={row} />
      </QueryClientProvider>,
    );

    expect(screen.getByText('+65%')).toBeInTheDocument();
    expect(screen.getByText('+16%')).toBeInTheDocument();
    expect(container.querySelectorAll('time')).toHaveLength(1);
  });
});
