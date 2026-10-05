import type { ReactNode } from 'react';
import { fireEvent, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import UsagePage from './UsagePage';

const { queryMock, refetchUsage, refetchSummary } = vi.hoisted(() => ({
  queryMock: vi.fn(),
  refetchUsage: vi.fn(),
  refetchSummary: vi.fn(),
}));
const t = (key: string) => key;
vi.mock('react-i18next', () => ({
  initReactI18next: { init: () => {}, type: '3rdParty' },
  useTranslation: () => ({ t }),
}));
vi.mock('@heroui/react', async () => import('../../test/herouiMock'));
vi.mock('@tanstack/react-query', () => ({
  keepPreviousData: (data: unknown) => data,
  useQuery: queryMock,
}));
vi.mock('../../shared/hooks/useCursorPagination', () => ({
  useCursorPagination: () => ({ page: 1, pageSize: 20, setPage: vi.fn(), setPageSize: vi.fn(), resetCursorPagination: vi.fn() }),
}));
vi.mock('../../shared/hooks/useUsagePageIndex', () => ({
  isUsagePaginationExpired: () => false,
  useUsagePageIndex: () => ({ refresh: vi.fn() }),
}));
vi.mock('../../shared/hooks/usePlatforms', () => ({
  usePlatforms: () => ({ platforms: [], platformName: (name: string) => name }),
}));
vi.mock('../../shared/components/TablePage', () => ({
  TablePage: ({ children }: { children: ReactNode }) => <div>{children}</div>,
}));
vi.mock('../../shared/components/RecordsTable', () => ({
  RecordsTable: () => <div role="table" aria-label="usage-records" />,
}));
vi.mock('../../shared/components/TablePaginationFooter', () => ({ TablePaginationFooter: () => null }));
vi.mock('../../shared/components/UsageDateRangeFilter', () => ({ UsageDateRangeFilter: () => <span>date-filter</span> }));
vi.mock('../../shared/components/UsageModelFilterInput', () => ({ UsageModelFilterInput: () => <span>model-filter</span> }));
vi.mock('../../shared/components/UserOrAPIKeySearchFilterComboBox', () => ({
  UserOrAPIKeySearchFilterComboBox: () => <span>user-key-filter</span>,
}));
vi.mock('../../shared/components/RefreshControl', () => ({
  RefreshControl: ({ onRefresh }: { onRefresh: () => void }) => <button onClick={onRefresh}>refresh-usage</button>,
}));

beforeEach(() => {
  Object.defineProperty(window, 'matchMedia', {
    configurable: true,
    value: vi.fn(() => ({ matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() })),
  });
  queryMock.mockClear();
  refetchUsage.mockClear();
  refetchSummary.mockClear();
  queryMock.mockImplementation(({ queryKey }: { queryKey: unknown[] }) => (
    queryKey[0] === 'admin-usage-stats'
      ? {
          data: { total_requests: 8, total_tokens: 123, total_actual_cost: 1.25, total_cost: 2.5 },
          refetch: refetchSummary,
        }
      : { data: { list: [], total: 0 }, refetch: refetchUsage }
  ));
});

describe('admin usage page without collapsible analytics', () => {
  it('keeps summary, filters and records without analytics cards or visibility toggle', () => {
    render(<UsagePage />);
    expect(screen.getByText('usage.total_requests')).toBeInTheDocument();
    expect(screen.getByText('usage.total_tokens')).toBeInTheDocument();
    expect(screen.getByText('date-filter')).toBeInTheDocument();
    expect(screen.getByText('model-filter')).toBeInTheDocument();
    expect(screen.getByText('user-key-filter')).toBeInTheDocument();
    expect(screen.getByRole('table', { name: 'usage-records' })).toBeInTheDocument();
    for (const key of ['usage.model_distribution', 'usage.group_distribution', 'usage.token_trend', 'usage.group_stats', 'usage.show_analysis_cards', 'usage.hide_analysis_cards']) {
      expect(screen.queryByText(key)).not.toBeInTheDocument();
    }
    expect(new Set(queryMock.mock.calls.map(([options]) => options.queryKey[0]))).toEqual(new Set(['admin-usage', 'admin-usage-stats']));
    for (const [options] of queryMock.mock.calls) {
      if (options.queryKey[0] === 'admin-usage-stats') expect(options.queryKey[1]).toBe('summary');
    }
  });

  it('refreshes only records and the retained summary', () => {
    render(<UsagePage />);
    fireEvent.click(screen.getByRole('button', { name: 'refresh-usage' }));
    expect(refetchUsage).toHaveBeenCalledOnce();
    expect(refetchSummary).toHaveBeenCalledOnce();
  });
});
