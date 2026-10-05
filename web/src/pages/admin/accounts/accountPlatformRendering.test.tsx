import { useMemo } from 'react';
import { act, cleanup, render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AccountResp } from '../../../shared/types';
import * as registry from '../../../app/plugin-frontend-registry';
import { TablePage } from '../../../shared/components/TablePage';
import { AccountCapacityStore, AccountSelectionStore } from './accountRuntimeStores';
import { AccountTableRow } from './accountTableSupport';
import { AccountTypeIcon } from './AccountTypeIcon';
import { useAccountTableColumns } from './useAccountTableColumns';

const stable = vi.hoisted(() => ({ t: (key: string) => key, toast: () => {} }));
vi.mock('react-i18next', async (importOriginal) => ({
  ...(await importOriginal<typeof import('react-i18next')>()),
  useTranslation: () => ({ t: stable.t }),
}));
vi.mock('../../../shared/ui', () => ({ useToast: () => ({ toast: stable.toast }) }));
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

const noop = () => {};
const plans = new Set<never>();
const groups = new Map<number, string>();
const platformName = () => 'Test platform';

function Harness({ rows, selection, capacity, fetching = false }: {
  rows: AccountResp[]; selection: AccountSelectionStore; capacity: AccountCapacityStore; fetching?: boolean;
}) {
  const { columns } = useAccountTableColumns({
    accountPoolAdjustmentPlans: plans, showAccountPoolAdjustedBaseFiveHour: false,
    capacityStore: capacity, groupMap: groups, rows, usageData: undefined,
    platformFilter: '', platformName, platformsKey: 'icon-render-test',
    onClearRateLimitMarkers: noop, onDeleteAccount: noop, onEditAccount: noop,
    onRefreshToken: noop, onStatsAccount: noop, onTestAccount: noop, onToggleScheduling: noop,
  });
  const identityColumns = useMemo(() => columns.filter((column) => column.key === 'platform'), [columns]);
  return (
    <TablePage isFetching={fetching} toolbar={<span>{fetching ? 'Refreshing' : 'Ready'}</span>}>
      <table><tbody>{rows.map((row) => (
        <AccountTableRow key={row.id} row={row} columns={identityColumns} isUsageExpanded={false}
          selectionStore={selection} selectRowAriaLabel={`Select ${row.id}`} onSelectedChange={noop} />
      ))}</tbody></table>
    </TablePage>
  );
}

describe('100-row platform and authentication icons', () => {
  it('subscribes once and isolates selection, concurrency, and plan updates', async () => {
    const icon = vi.fn(() => <svg data-testid="platform-glyph" />);
    // 模拟线上仍返回类型文字的旧插件；表格不应再消费它的身份组件。
    const identity = vi.fn(() => <span>Legacy OAuth / API Key</span>);
    registry.registerPlatformIcon('icon-render-test', icon);
    registry.registerAccountIdentity('icon-render-test', identity);
    const subscribe = vi.spyOn(registry, 'onPlatformIconChange');
    const rows: AccountResp[] = Array.from({ length: 100 }, (_, index) => ({
      id: index + 1, name: `Account ${index + 1}`, email: null,
      platform: 'icon-render-test', type: index % 2 === 0 ? 'oauth' : 'apikey', credentials: { plan_type: 'Plus' },
      model_policy: {}, state: 'active', priority: 0, max_concurrency: 4, scheduling_weight: 100, current_concurrency: 0,
      rate_multiplier: 1, upstream_is_pool: false, group_ids: [], created_at: '', updated_at: '',
    }));
    const selection = new AccountSelectionStore();
    const capacity = new AccountCapacityStore();
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const page = (items: AccountResp[], fetching = false) => (
      <QueryClientProvider client={client}><Harness rows={items} selection={selection} capacity={capacity} fetching={fetching} /></QueryClientProvider>
    );
    const { rerender } = render(page(rows));
    expect(subscribe).toHaveBeenCalledTimes(1);
    expect(icon).toHaveBeenCalledTimes(100);
    expect(identity).not.toHaveBeenCalled();
    expect(screen.getAllByRole('img', { name: 'OAuth' })).toHaveLength(50);
    expect(screen.getAllByRole('img', { name: 'API Key' })).toHaveLength(50);
    expect(screen.queryByText('Legacy OAuth / API Key')).not.toBeInTheDocument();
    expect(screen.queryByText('OAuth')).not.toBeInTheDocument();
    expect(screen.queryByText('API Key')).not.toBeInTheDocument();
    expect(screen.getAllByText('Plus')).toHaveLength(100);

    await act(async () => {
      selection.setRows(rows.map((row) => row.id), true);
      await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    });
    rerender(page(rows.map((row) => ({ ...row, current_concurrency: 2, updated_at: '2026-10-01T00:00:00Z', last_used_at: '2026-10-01T00:00:00Z' })), true));
    expect(screen.getAllByRole('checkbox', { checked: true })).toHaveLength(100);
    expect(icon).toHaveBeenCalledTimes(100);
    expect(identity).not.toHaveBeenCalled();

    rerender(page(rows.map((row) => row.id === 1 ? { ...row, credentials: { plan_type: 'Pro' } } : row)));
    expect(identity).not.toHaveBeenCalled();
    expect(icon).toHaveBeenCalledTimes(101);
    expect(screen.getByText('Pro')).toBeInTheDocument();
    expect(screen.getAllByText('Plus')).toHaveLength(99);

    const replacement = vi.fn(() => <svg data-testid="replacement-glyph" />);
    act(() => registry.registerPlatformIcon('icon-render-test', replacement));
    expect(screen.getAllByTestId('replacement-glyph')).toHaveLength(100);
    expect(subscribe).toHaveBeenCalledTimes(1);
  });

  it.each([['apikey', 'API Key'], ['session_key', 'Session Key'], ['custom-auth', 'custom-auth']])('labels %s without a text badge', (type, label) => {
    render(<AccountTypeIcon type={type} />);
    expect(screen.getByRole('img', { name: label })).toHaveAttribute('title', label);
    expect(screen.queryByText(label)).not.toBeInTheDocument();
  });
});
