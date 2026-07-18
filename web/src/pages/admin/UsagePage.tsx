import { lazy, memo, startTransition, Suspense, useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Card, Skeleton, Tabs } from '@heroui/react';
import { usageApi } from '../../shared/api/usage';
import { useCursorPagination } from '../../shared/hooks/useCursorPagination';
import { usePlatforms } from '../../shared/hooks/usePlatforms';
import { Activity, ChevronDown, ChevronUp, Columns3, DollarSign, Sigma } from 'lucide-react';
import { useUsageColumns, fmtNum, type UsageColumnConfig } from '../../shared/columns/usageColumns';
import type { UsageLogResp, UsageQuery, UsageTrendBucket } from '../../shared/types';
import { CompactDataTable } from '../../shared/components/CompactDataTable';
import { RecordsTable } from '../../shared/components/RecordsTable';
import { TablePage } from '../../shared/components/TablePage';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { UsageDateRangeFilter } from '../../shared/components/UsageDateRangeFilter';
import { UsageModelFilterInput } from '../../shared/components/UsageModelFilterInput';
import { SearchFilterInput } from '../../shared/components/SearchFilterInput';
import {
  UserOrAPIKeySearchFilterComboBox,
  type UserOrAPIKeySearchSelection,
} from '../../shared/components/UserOrAPIKeySearchFilterComboBox';
import { DISTRIBUTION_COLORS, PAGE_SIZE_OPTIONS } from '../../shared/constants';
import { CostValue } from '../../shared/components/CostValue';
import { AutoRefreshControl } from '../../shared/components/AutoRefreshControl';
import { ToolbarMenu, ToolbarMenuItem } from '../../shared/components/ToolbarMenu';
import { SimpleSelect } from '../../shared/components/SimpleSelect';
import { usePersistentBoolean } from '../../shared/hooks/usePersistentBoolean';
import { ADMIN_AUTO_REFRESH_OPTIONS, usePersistentAutoRefresh } from '../../shared/hooks/usePersistentAutoRefresh';
import { STORAGE_KEYS } from '../../shared/storageKeys';
import { getTotalPages } from '../../shared/utils/pagination';
import { type MetricTone, METRIC_TONE_CLASSES, METRIC_TONE_STYLES } from '../../shared/ui/metricTones';

const UsageTokenTrendChart = lazy(() =>
  import('./usage/UsageCharts').then((m) => ({ default: m.UsageTokenTrendChart })),
);

const DISTRIBUTION_DOT_COLORS = DISTRIBUTION_COLORS;

interface ColumnVisibilityOption {
  key: string;
  label: string;
}

function SectionCard({
  children,
  extra,
  title,
}: {
  children: ReactNode;
  extra?: ReactNode;
  title: string;
}) {
  return (
    <Card className="ag-dashboard-panel">
      <div
        className="flex min-w-0 items-center justify-between gap-3 p-3 pb-2 2xl:p-4 2xl:pb-2"
      >
        <h3 className="min-w-0 truncate text-base font-semibold leading-none text-text">{title}</h3>
        {extra ? (
          <div className="min-w-0 shrink">{extra}</div>
        ) : null}
      </div>
      <Card.Content className="px-3 pb-3 2xl:px-4 2xl:pb-4">{children}</Card.Content>
    </Card>
  );
}

function StatCard({
  icon,
  tone,
  title,
  value,
}: {
  icon: ReactNode;
  tone: MetricTone;
  title: string;
  value: ReactNode;
}) {
  return (
    <Card className="ag-dashboard-metric min-h-[72px] 2xl:min-h-[78px]">
      <Card.Content className="ag-dashboard-metric-content p-3 2xl:p-3.5">
        <div className="ag-dashboard-metric-copy">
          <div className="truncate text-sm font-semibold tracking-normal text-text-tertiary">{title}</div>
          <div className="mt-1 flex min-w-0 items-baseline gap-2">
            <div className="min-w-0 truncate font-mono text-[22px] font-semibold leading-none text-text 2xl:text-2xl">{value}</div>
          </div>
        </div>
        <span
          className={`hidden h-11 w-11 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1 shadow-sm 2xl:flex ${METRIC_TONE_CLASSES[tone]}`}
          style={METRIC_TONE_STYLES[tone]}
        >
          {icon}
        </span>
      </Card.Content>
    </Card>
  );
}

function StatsSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4 2xl:gap-4">
      {Array.from({ length: 4 }).map((_, index) => (
        <Card className="ag-dashboard-metric min-h-[72px] 2xl:min-h-[78px]" key={index}>
          <Card.Content className="ag-dashboard-metric-content p-3 2xl:p-3.5">
            <div className="ag-dashboard-metric-copy space-y-2">
              <Skeleton className="h-3 w-24" />
              <Skeleton className="h-6 w-24" />
            </div>
            <Skeleton className="hidden h-11 w-11 shrink-0 rounded-[var(--field-radius)] 2xl:block" />
          </Card.Content>
        </Card>
      ))}
    </div>
  );
}

const ColumnVisibilityMenu = memo(function ColumnVisibilityMenu({
  label,
  onToggle,
  options,
  selectedCount,
  selectedKeys,
}: {
  label: string;
  onToggle: (key: string) => void;
  options: ColumnVisibilityOption[];
  selectedCount: number;
  selectedKeys: Set<string>;
}) {
  return (
    <ToolbarMenu
      ariaLabel={label}
      className="ag-page-toolbar-button button button--sm button--secondary inline-flex min-w-[8.5rem] items-center justify-center gap-2 whitespace-nowrap px-3"
      icon={<Columns3 className="h-4 w-4 shrink-0" aria-hidden="true" />}
      label={`${label} ${selectedCount}/${options.length}`}
      rootClassName="ag-column-visibility-menu"
    >
      {() => (
        <>
          {options.map((option) => (
            <ToolbarMenuItem
              key={option.key}
              isSelected={selectedKeys.has(option.key)}
              role="menuitemcheckbox"
              onSelect={() => onToggle(option.key)}
            >
              {option.label}
            </ToolbarMenuItem>
          ))}
        </>
      )}
    </ToolbarMenu>
  );
});

// 分组统计 key 映射
const groupByKeys: Record<string, string> = {
  model: 'usage.by_model',
  user: 'usage.by_user',
  account: 'usage.by_account',
  group: 'usage.by_group',
};

const groupByHeaderKeys: Record<string, string> = {
  model: 'usage.model',
  user: 'usage.user_id',
  account: 'usage.by_account',
  group: 'usage.by_group',
};

const ADMIN_USAGE_AUTO_UPDATE_STORAGE_KEY = STORAGE_KEYS.ui.adminUsageAutoRefresh;
const ADMIN_USAGE_CARDS_COLLAPSED_STORAGE_KEY = STORAGE_KEYS.ui.adminUsageCardsCollapsed;
const ADMIN_USAGE_COLUMN_STORAGE_KEY = STORAGE_KEYS.ui.adminUsageColumns;
const ADMIN_USAGE_FILTER_STORAGE_KEY = STORAGE_KEYS.ui.adminUsageFilters;
const ADMIN_USAGE_DEFAULT_COLUMN_KEYS = [
  'user_id',
  'created_at',
  'model',
  'stream',
  'ws_dial_ms',
  'first_event_ms',
  'first_token_ms',
  'duration_ms',
  'tokens',
  'cost',
  'endpoint',
  'api_key',
  'account_name',
  'ip_address',
  'user_agent',
] as const;

type StoredAdminUsageFilters = {
  account?: string;
  api_key_id?: number;
  api_key_label?: string;
  model?: string;
  platform?: string;
  user_id?: number;
  user_label?: string;
};

type AdminUsageFilterState = {
  apiKeyLabel: string;
  filters: Partial<UsageQuery>;
  userLabel: string;
};

function compactText(value: string | undefined, fallback = '-') {
  const trimmed = value?.trim();
  return trimmed || fallback;
}

function formatUsageTimingMs(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '-';
  return value >= 1000 ? `${(value / 1000).toFixed(2)}s` : `${value}ms`;
}

function readStoredPositiveID(value: unknown) {
  const parsed = typeof value === 'number' ? value : Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? Math.floor(parsed) : undefined;
}

function isStoredFilterRecord(value: unknown): value is Record<string, unknown> {
  return value != null && typeof value === 'object' && !Array.isArray(value);
}

function readAdminUsageFilterState(): AdminUsageFilterState {
  const fallback: AdminUsageFilterState = { apiKeyLabel: '', filters: {}, userLabel: '' };
  if (typeof window === 'undefined') return fallback;

  try {
    const raw = window.localStorage.getItem(ADMIN_USAGE_FILTER_STORAGE_KEY);
    if (!raw) return fallback;
    const parsed = JSON.parse(raw);
    if (!isStoredFilterRecord(parsed)) return fallback;

    const filters: Partial<UsageQuery> = {};
    const account = typeof parsed.account === 'string' ? parsed.account.trim() : '';
    const model = typeof parsed.model === 'string' ? parsed.model.trim() : '';
    const platform = typeof parsed.platform === 'string' ? parsed.platform.trim() : '';
    const userID = readStoredPositiveID(parsed.user_id);
    const apiKeyID = readStoredPositiveID(parsed.api_key_id);

    if (account) filters.account = account;
    if (model) filters.model = model;
    if (platform) filters.platform = platform;
    if (userID != null) filters.user_id = userID;
    if (apiKeyID != null) filters.api_key_id = apiKeyID;

    return {
      apiKeyLabel: apiKeyID != null ? (typeof parsed.api_key_label === 'string' && parsed.api_key_label ? parsed.api_key_label : `#${apiKeyID}`) : '',
      filters,
      userLabel: userID != null ? (typeof parsed.user_label === 'string' && parsed.user_label ? parsed.user_label : `#${userID}`) : '',
    };
  } catch {
    return fallback;
  }
}

function writeAdminUsageFilterState(filters: Partial<UsageQuery>, userLabel: string, apiKeyLabel: string) {
  if (typeof window === 'undefined') return;

  const stored: StoredAdminUsageFilters = {};
  const account = filters.account?.trim();
  const model = filters.model?.trim();
  if (account) stored.account = account;
  if (model) stored.model = model;
  if (filters.platform) stored.platform = filters.platform;
  if (filters.user_id != null && filters.user_id > 0) {
    stored.user_id = filters.user_id;
    if (userLabel) stored.user_label = userLabel;
  }
  if (filters.api_key_id != null && filters.api_key_id > 0) {
    stored.api_key_id = filters.api_key_id;
    if (apiKeyLabel) stored.api_key_label = apiKeyLabel;
  }

  try {
    if (Object.keys(stored).length > 0) {
      window.localStorage.setItem(ADMIN_USAGE_FILTER_STORAGE_KEY, JSON.stringify(stored));
    } else {
      window.localStorage.removeItem(ADMIN_USAGE_FILTER_STORAGE_KEY);
    }
  } catch {
    // localStorage may be unavailable in restricted browser modes.
  }
}

function readAdminUsageColumnKeys() {
  if (typeof window === 'undefined') return new Set<string>(ADMIN_USAGE_DEFAULT_COLUMN_KEYS);
  try {
    const raw = window.localStorage.getItem(ADMIN_USAGE_COLUMN_STORAGE_KEY);
    if (!raw) return new Set<string>(ADMIN_USAGE_DEFAULT_COLUMN_KEYS);
    const parsed = JSON.parse(raw);
    if (!Array.isArray(parsed)) return new Set<string>(ADMIN_USAGE_DEFAULT_COLUMN_KEYS);
    const keys = parsed.filter((key): key is string => typeof key === 'string' && key.length > 0);
    return keys.length > 0 ? new Set(keys) : new Set<string>(ADMIN_USAGE_DEFAULT_COLUMN_KEYS);
  } catch {
    return new Set<string>(ADMIN_USAGE_DEFAULT_COLUMN_KEYS);
  }
}

function writeAdminUsageColumnKeys(keys: Set<string>) {
  try {
    window.localStorage.setItem(ADMIN_USAGE_COLUMN_STORAGE_KEY, JSON.stringify(Array.from(keys)));
  } catch {
    // localStorage may be unavailable in restricted browser modes.
  }
}

function displayUserAgent(value: string | undefined) {
  const raw = compactText(value);
  if (raw === '-') return raw;

  const display = raw
    .replace(/^Mozilla\/5\.0\s*(?:\([^)]*\)\s*)?/i, '')
    .replace(/^AppleWebKit\/[\d.]+\s*(?:\([^)]*\)\s*)?/i, '')
    .replace(/\s+AppleWebKit\/[\d.]+\s*(?:\([^)]*\)\s*)?/gi, ' ')
    .replace(/\s+/g, ' ')
    .trim();

  return display || raw;
}

// ==================== 分布表格卡片 ====================

interface DistributionItem {
  name: string;
  requests: number;
  tokens: number;
  totalCost: number;
  actualCost: number;
}

function DistributionCard({
  title,
  data,
  firstColumnTitle,
  firstColumnWidth = '30%',
}: {
  title: string;
  data: DistributionItem[];
  firstColumnTitle: string;
  firstColumnWidth?: string;
}) {
  const { t } = useTranslation();

  return (
    <SectionCard title={title}>
      <div className="ag-distribution-table-scroll">
        <CompactDataTable
          ariaLabel={title}
          className="ag-compact-data-table--dense"
          emptyText={t('common.no_data')}
          minWidth={480}
          rowKey={(row) => row.name}
          rows={data}
          columns={[
            {
              key: 'name',
              title: firstColumnTitle,
              width: firstColumnWidth,
              render: (item, index) => (
                <>
                  <span className="shrink-0 font-mono text-[11px] font-semibold text-text-tertiary">#{index + 1}</span>
                  <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: DISTRIBUTION_DOT_COLORS[index % DISTRIBUTION_DOT_COLORS.length] }} />
                  <span className="min-w-0 truncate font-medium text-text" title={item.name}>{item.name}</span>
                </>
              ),
            },
            {
              align: 'end',
              key: 'requests',
              title: t('usage.requests'),
              width: '16%',
              render: (item) => <span className="truncate font-mono text-text-secondary">{item.requests.toLocaleString()}</span>,
            },
            {
              align: 'end',
              key: 'tokens',
              title: t('usage.tokens'),
              width: '18%',
              render: (item) => <span className="truncate font-mono text-text-secondary">{fmtNum(item.tokens)}</span>,
            },
            {
              align: 'end',
              key: 'actualCost',
              title: t('usage.actual_cost'),
              width: '18%',
              render: (item) => <CostValue className="truncate font-mono" value={item.actualCost} tone="actual" />,
            },
            {
              align: 'end',
              key: 'totalCost',
              title: t('usage.standard_cost'),
              width: '18%',
              render: (item) => <CostValue className="truncate font-mono" value={item.totalCost} tone="standard" />,
            },
          ]}
        />
      </div>
    </SectionCard>
  );
}

type GroupStatsRow = {
  key: string | number;
  name: string;
  requests: number;
  tokens: number;
  total_cost: number;
  actual_cost: number;
};

function GroupStatsCard({
  activeKey,
  rows,
  onActiveKeyChange,
}: {
  activeKey: string;
  rows: GroupStatsRow[];
  onActiveKeyChange: (key: string) => void;
}) {
  const { t } = useTranslation();

  return (
    <SectionCard
      title={t('usage.group_stats')}
      extra={
        <Tabs
          className="ag-segmented-tabs ag-segmented-tabs-compact ag-segmented-tabs-auto"
          selectedKey={activeKey}
          onSelectionChange={(key) => {
            const nextKey = String(key);
            if (nextKey !== activeKey) {
              onActiveKeyChange(nextKey);
            }
          }}
        >
          <Tabs.List>
            {Object.entries(groupByKeys).map(([key, i18nKey], index) => (
              <Tabs.Tab id={key} key={key}>
                {index > 0 ? <Tabs.Separator /> : null}
                <Tabs.Indicator />
                <span>{t(i18nKey)}</span>
              </Tabs.Tab>
            ))}
          </Tabs.List>
        </Tabs>
      }
    >
      <div className="h-[248px] min-w-0 overflow-auto 2xl:h-[288px]">
        <CompactDataTable
          ariaLabel={t('usage.group_stats')}
          className="ag-compact-data-table--dense"
          emptyText={t('common.no_data')}
          minWidth={520}
          rowKey={(row) => row.key}
          rows={rows}
          columns={[
            {
              key: 'name',
              title: t(groupByHeaderKeys[activeKey] ?? 'usage.model'),
              width: '30%',
              render: (row, index) => (
                <>
                  <span className="shrink-0 font-mono text-[11px] font-semibold text-text-tertiary">#{index + 1}</span>
                  <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: DISTRIBUTION_DOT_COLORS[index % DISTRIBUTION_DOT_COLORS.length] }} />
                  <span className="min-w-0 truncate font-medium text-text" title={row.name}>{row.name}</span>
                </>
              ),
            },
            {
              align: 'end',
              key: 'requests',
              title: t('usage.requests'),
              width: '16%',
              render: (row) => <span className="truncate font-mono text-text-secondary">{row.requests.toLocaleString()}</span>,
            },
            {
              align: 'end',
              key: 'tokens',
              title: t('usage.tokens'),
              width: '18%',
              render: (row) => <span className="truncate font-mono text-text-secondary">{fmtNum(row.tokens)}</span>,
            },
            {
              align: 'end',
              key: 'actualCost',
              title: t('usage.actual_cost'),
              width: '18%',
              render: (row) => <CostValue className="truncate font-mono" value={row.actual_cost} tone="actual" />,
            },
            {
              align: 'end',
              key: 'totalCost',
              title: t('usage.standard_cost'),
              width: '18%',
              render: (row) => <CostValue className="truncate font-mono" value={row.total_cost} tone="standard" />,
            },
          ]}
        />
      </div>
    </SectionCard>
  );
}

// ==================== Token 使用趋势 ====================

function TokenTrendCard({
  data,
  granularity,
  onGranularityChange,
}: {
  data: UsageTrendBucket[];
  granularity: string;
  onGranularityChange: (g: string) => void;
}) {
  const { t } = useTranslation();

  const lineLabels: Record<string, string> = {
    input: t('usage.input'),
    output: t('usage.output'),
    cacheCreation: t('usage.cache_creation'),
    cacheRead: t('usage.cache_read'),
    cacheRatio: t('usage.cache_ratio'),
    cacheCumulativeRatio: t('usage.cache_cumulative_ratio'),
  };
  const granularityTabs = (
    <Tabs className="ag-segmented-tabs ag-segmented-tabs-compact" selectedKey={granularity} onSelectionChange={(key) => onGranularityChange(String(key))}>
      <Tabs.List>
        {(['hour', 'day'] as const).map((g, index) => (
          <Tabs.Tab id={g} key={g}>
            {index > 0 ? <Tabs.Separator /> : null}
            <Tabs.Indicator />
            <span>{t(`usage.granularity_${g}`)}</span>
          </Tabs.Tab>
        ))}
      </Tabs.List>
    </Tabs>
  );

  if (data.length === 0) {
    return (
      <SectionCard title={t('usage.token_trend')} extra={granularityTabs}>
        <div className="flex h-[248px] items-center justify-center text-sm text-text-tertiary 2xl:h-[288px]">
          {t('common.no_data')}
        </div>
      </SectionCard>
    );
  }

  return (
    <SectionCard
      title={t('usage.token_trend')}
      extra={granularityTabs}
    >
      <div className="h-[248px] 2xl:h-[288px]">
        <Suspense fallback={<div className="h-full w-full" />}>
          <UsageTokenTrendChart data={data} lineLabels={lineLabels} />
        </Suspense>
      </div>
    </SectionCard>
  );
}

// ==================== 主页面 ====================

export default function UsagePage() {
  const { t } = useTranslation();
  const { beforeId, page, setPage, pageSize, setPageSize, resetCursorPagination } = useCursorPagination(20, 'admin.usage');
  const [initialFilterState] = useState<AdminUsageFilterState>(readAdminUsageFilterState);
  const [filters, setFilters] = useState<Partial<UsageQuery>>(() => initialFilterState.filters);
  const [selectedUserLabel, setSelectedUserLabel] = useState(initialFilterState.userLabel);
  const [selectedAPIKeyLabel, setSelectedAPIKeyLabel] = useState(initialFilterState.apiKeyLabel);
  const [statsGroupBy, setStatsGroupBy] = useState<string>('model');
  const [granularity, setGranularity] = useState<string>('hour');
  const [selectedColumnKeys, setSelectedColumnKeys] = useState<Set<string>>(
    readAdminUsageColumnKeys,
  );
  const [autoRefresh, setAutoRefresh] = usePersistentAutoRefresh(ADMIN_USAGE_AUTO_UPDATE_STORAGE_KEY, 0, ADMIN_AUTO_REFRESH_OPTIONS);
  const [usageCardsCollapsed, setUsageCardsCollapsed] = usePersistentBoolean(ADMIN_USAGE_CARDS_COLLAPSED_STORAGE_KEY, false);
  const { platforms, platformName } = usePlatforms();
  const autoRefreshEnabled = autoRefresh > 0;
  const autoRefreshLabel = `${t('usage.auto_update')} `;
  const autoRefreshOffLabel = t('usage.auto_update_off');

  const handleModelChange = useCallback((model: string) => {
    const nextModel = model || undefined;
    resetCursorPagination();
    setFilters((prev) => (prev.model === nextModel ? prev : { ...prev, model: nextModel }));
  }, [resetCursorPagination]);

  const handleAccountChange = useCallback((account: string) => {
    const nextAccount = account.trim() || undefined;
    resetCursorPagination();
    setFilters((prev) => (prev.account === nextAccount ? prev : { ...prev, account: nextAccount }));
  }, [resetCursorPagination]);

  // 构建查询参数
  const queryParams = useMemo<UsageQuery>(() => ({
    page,
    page_size: pageSize,
    before_id: beforeId,
    ...filters,
  }), [beforeId, filters, page, pageSize]);

  // 使用记录列表
  const {
    data,
    dataUpdatedAt,
    isFetching: isUsageFetching,
    isLoading,
    isPlaceholderData,
    refetch: refetchUsage,
  } = useQuery({
    queryKey: ['admin-usage', queryParams],
    queryFn: ({ signal }) => usageApi.adminList(queryParams, { signal }),
    meta: { globalLoading: false },
    refetchOnReconnect: autoRefreshEnabled,
    refetchOnWindowFocus: autoRefreshEnabled,
    placeholderData: keepPreviousData,
  });

  const statsFilters = useMemo(() => ({
    account: filters.account,
    start_date: filters.start_date,
    end_date: filters.end_date,
    platform: filters.platform,
    model: filters.model,
    user_id: filters.user_id ? Number(filters.user_id) : undefined,
    api_key_id: filters.api_key_id ? Number(filters.api_key_id) : undefined,
  }), [filters.account, filters.api_key_id, filters.end_date, filters.model, filters.platform, filters.start_date, filters.user_id]);

  const {
    data: summaryStats,
    isFetching: isSummaryStatsFetching,
    isPlaceholderData: isSummaryStatsPlaceholderData,
    refetch: refetchSummaryStats,
  } = useQuery({
    queryKey: ['admin-usage-stats', 'summary', statsFilters],
    queryFn: ({ signal }) =>
      usageApi.stats(statsFilters, { signal }),
    meta: { globalLoading: false },
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
    placeholderData: keepPreviousData,
  });

  const analysisGroupBy = useMemo(
    () => Array.from(new Set(['model', 'group', statsGroupBy])).join(','),
    [statsGroupBy],
  );
  const analysisStatsEnabled = !usageCardsCollapsed && summaryStats != null && !isSummaryStatsPlaceholderData;
  const {
    data: analysisStats,
    isFetching: isAnalysisStatsFetching,
    refetch: refetchAnalysisStats,
  } = useQuery({
    queryKey: ['admin-usage-stats', 'analysis', analysisGroupBy, statsFilters],
    queryFn: ({ signal }) =>
      usageApi.stats({
        ...statsFilters,
        group_by: analysisGroupBy,
        include_summary: false,
      }, { signal }),
    enabled: analysisStatsEnabled,
    meta: { globalLoading: false },
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
    placeholderData: keepPreviousData,
  });

  // Token 趋势
  const { data: trendData, isFetching: isTrendFetching, refetch: refetchTrend } = useQuery({
    queryKey: ['admin-usage-trend', granularity, filters.start_date, filters.end_date, filters.platform, filters.model, filters.account, filters.user_id, filters.api_key_id],
    queryFn: ({ signal }) =>
      usageApi.trend({
        granularity,
        start_date: filters.start_date,
        end_date: filters.end_date,
        platform: filters.platform,
        model: filters.model,
        account: filters.account,
        user_id: filters.user_id ? Number(filters.user_id) : undefined,
        api_key_id: filters.api_key_id ? Number(filters.api_key_id) : undefined,
      }, { signal }),
    meta: { globalLoading: false },
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
    placeholderData: keepPreviousData,
  });

  const isStatsFetching = isSummaryStatsFetching || (analysisStatsEnabled && isAnalysisStatsFetching);
  const isRefreshing = isUsageFetching || isStatsFetching || isTrendFetching;
  const isUsageTableRefreshing = isUsageFetching;

  const handleManualRefresh = useCallback(() => {
    void refetchUsage({ cancelRefetch: false });
    void refetchTrend({ cancelRefetch: false });
    void refetchSummaryStats({ cancelRefetch: false }).then((result) => {
      if (!usageCardsCollapsed && result.isSuccess) {
        void refetchAnalysisStats({ cancelRefetch: false });
      }
    });
  }, [refetchAnalysisStats, refetchSummaryStats, refetchTrend, refetchUsage, usageCardsCollapsed]);

  const handleAutoRefresh = useCallback(() => {
    void refetchUsage({ cancelRefetch: false });
  }, [refetchUsage]);

  const updateFilter = useCallback((key: keyof UsageQuery, value: string) => {
    const nextValue = (key === 'user_id' || key === 'api_key_id')
      ? (value ? Number(value) : undefined)
      : value || undefined;
    startTransition(() => {
      setFilters((prev) => ({ ...prev, [key]: nextValue }));
      resetCursorPagination();
    });
  }, [resetCursorPagination]);

  const handleUserOrAPIKeySelectionChange = useCallback((selection: UserOrAPIKeySearchSelection | null) => {
    const userID = selection?.kind === 'user' ? selection.id : '';
    const apiKeyID = selection?.kind === 'api_key' ? selection.id : '';
    setSelectedUserLabel(selection?.kind === 'user' ? selection.label : '');
    setSelectedAPIKeyLabel(selection?.kind === 'api_key' ? selection.label : '');
    startTransition(() => {
      setFilters((prev) => ({
        ...prev,
        api_key_id: apiKeyID ? Number(apiKeyID) : undefined,
        user_id: userID ? Number(userID) : undefined,
      }));
      resetCursorPagination();
    });
  }, [resetCursorPagination]);

  const selectedSearchKind = filters.user_id != null
    ? 'user'
    : filters.api_key_id != null
      ? 'api_key'
      : undefined;
  const selectedSearchKey = selectedSearchKind === 'user'
    ? String(filters.user_id)
    : selectedSearchKind === 'api_key'
      ? String(filters.api_key_id)
      : null;
  const selectedSearchLabel = selectedSearchKind === 'user'
    ? selectedUserLabel
    : selectedSearchKind === 'api_key'
      ? selectedAPIKeyLabel
      : '';

  useEffect(() => {
    writeAdminUsageFilterState(filters, selectedUserLabel, selectedAPIKeyLabel);
  }, [filters.account, filters.api_key_id, filters.model, filters.platform, filters.user_id, selectedAPIKeyLabel, selectedUserLabel]);

  const activeStats = summaryStats;

  // 分布表格数据
  const modelDistribution: DistributionItem[] = useMemo(
    () => (analysisStats?.by_model ?? []).map((s) => ({
      name: s.model,
      requests: s.requests,
      tokens: s.tokens,
      totalCost: s.total_cost,
      actualCost: s.actual_cost,
    })),
    [analysisStats?.by_model],
  );

  const groupDistribution: DistributionItem[] = useMemo(
    () => (analysisStats?.by_group ?? []).map((s) => ({
      name: s.name || `#${s.group_id}`,
      requests: s.requests,
      tokens: s.tokens,
      totalCost: s.total_cost,
      actualCost: s.actual_cost,
    })),
    [analysisStats?.by_group],
  );

  const groupStatsRows: GroupStatsRow[] = useMemo(() => {
    if (!analysisStats) return [];
    const dataMap: Record<string, GroupStatsRow[]> = {
      account: analysisStats.by_account?.map((s) => ({ key: s.account_id, name: s.name, requests: s.requests, tokens: s.tokens, total_cost: s.total_cost, actual_cost: s.actual_cost })) ?? [],
      group: analysisStats.by_group?.map((s) => ({ key: s.group_id, name: s.name || `#${s.group_id}`, requests: s.requests, tokens: s.tokens, total_cost: s.total_cost, actual_cost: s.actual_cost })) ?? [],
      model: analysisStats.by_model?.map((s) => ({ key: s.model, name: s.model, requests: s.requests, tokens: s.tokens, total_cost: s.total_cost, actual_cost: s.actual_cost })) ?? [],
      user: analysisStats.by_user?.map((s) => ({ key: s.user_id, name: s.email, requests: s.requests, tokens: s.tokens, total_cost: s.total_cost, actual_cost: s.actual_cost })) ?? [],
    };
    return dataMap[statsGroupBy] ?? [];
  }, [analysisStats, statsGroupBy]);

  const sharedColumns = useUsageColumns();

  const platformOptions = [
    { id: '', label: t('common.all') },
    ...platforms.map((p) => ({ id: p, label: platformName(p) })),
  ];
  const selectedPlatformLabel = filters.platform
    ? (platformOptions.find((item) => item.id === filters.platform)?.label ?? platformName(filters.platform))
    : t('common.all');

  const allColumns = useMemo(() => {
    const adminColumns: UsageColumnConfig<UsageLogResp>[] = [
      {
        key: 'user_id',
        title: t('common.user'),
        width: '160px',
        render: (row) => {
          const fallbackLabel = row.user_deleted ? t('usage.user_deleted') : `#${row.user_id}`;
          const label = row.user_email || fallbackLabel;

          return (
            <div className="flex min-w-0 items-center gap-1.5">
              <span className="shrink-0 font-mono text-xs text-text-tertiary">{row.user_id > 0 ? `#${row.user_id}` : '-'}</span>
              <span className={`min-w-0 truncate text-[13px] font-medium ${row.user_deleted ? 'text-text-tertiary' : 'text-text'}`} title={label}>
                {label}
              </span>
            </div>
          );
        },
      },
    ];
    const modelIdx = sharedColumns.findIndex((c) => c.key === 'model');
    const streamColumn = sharedColumns.find((column) => column.key === 'stream');
    const timingKeys = new Set(['first_event_ms', 'first_token_ms', 'duration_ms']);
    const timingColumns = sharedColumns
      .filter((column) => timingKeys.has(column.key))
      .map((column) => ({
        ...column,
        width: '64px',
      }));
    const leadingSharedColumns = sharedColumns
      .slice(0, modelIdx + 1)
      .map((column) => (column.key === 'model' ? { ...column, width: '224px' } : column));
    const sharedColumnsAfterModel = sharedColumns
      .slice(modelIdx + 1)
      .filter((column) => !timingKeys.has(column.key) && column.key !== 'stream')
      .map((column) => (column.key === 'cost' ? { ...column, width: '120px' } : column));
    const endpointColumn: UsageColumnConfig<UsageLogResp> = {
      key: 'endpoint',
      title: t('usage.endpoint', '端点'),
      width: '156px',
      hideOnMobile: true,
      render: (row) => (
        <span className="block truncate font-mono text-xs leading-tight text-text-secondary" title={row.endpoint || '-'}>
          {row.endpoint || '-'}
        </span>
      ),
    };
    const apiKeyColumn: UsageColumnConfig<UsageLogResp> = {
      key: 'api_key',
      title: t('usage.api_key', 'API Key'),
      width: '104px',
      hideOnMobile: true,
      render: (row) => {
        if (row.api_key_id === 0) {
          return <span className="block max-w-full truncate text-[13px] text-text-tertiary">{t('usage.api_key_plugin_call')}</span>;
        }
        if (row.api_key_deleted) {
          return <span className="block max-w-full truncate text-[13px] text-text-tertiary">{t('usage.api_key_deleted')}</span>;
        }
        const name = row.api_key_name || '-';
        return (
          <span className="block max-w-full truncate text-xs text-text-secondary" title={name}>{name}</span>
        );
      },
    };
    const accountColumn: UsageColumnConfig<UsageLogResp> = {
      key: 'account_name',
      title: t('usage.upstream_credential', 'Credential'),
      width: '172px',
      hideOnMobile: true,
      render: (row) => {
        const name = row.account_name || '-';
        const accountID = row.account_id > 0 ? `#${row.account_id}` : '';
        const displayName = [accountID, name].filter(Boolean).join(' ');
        const email = row.account_email?.trim();
        const title = email && name !== '-' ? `${displayName}\n${email}` : displayName;
        return (
          <div className="flex w-full min-w-0 flex-col items-start text-left" title={title}>
            <span className={`flex w-full min-w-0 items-center gap-1 text-xs font-medium ${row.account_deleted ? 'text-text-tertiary' : 'text-text-secondary'}`}>
              <span className="shrink-0 font-mono text-[11px] text-warning">{accountID}</span>
              <span className={`min-w-0 truncate text-left ${row.account_deleted ? 'line-through' : ''}`}>{name}</span>
            </span>
            {email && name !== '-' ? (
              <span className={`block w-full min-w-0 truncate text-left text-[11px] leading-tight text-text-tertiary ${row.account_deleted ? 'line-through' : ''}`}>{email}</span>
            ) : null}
          </div>
        );
      },
    };
    const userAgentColumn: UsageColumnConfig<UsageLogResp> = {
      key: 'user_agent',
      title: t('usage.user_agent', 'User-Agent'),
      width: '184px',
      hideOnMobile: true,
      render: (row) => {
        const rawUserAgent = compactText(row.user_agent);
        const userAgent = displayUserAgent(row.user_agent);
        return (
          <span
            className="block w-full min-w-0 max-w-full overflow-hidden text-left font-mono text-[11px] leading-tight tracking-tight text-text-secondary"
            style={{
              display: '-webkit-box',
              overflowWrap: 'anywhere',
              WebkitBoxOrient: 'vertical',
              WebkitLineClamp: 2,
              whiteSpace: 'normal',
            }}
            title={rawUserAgent}
          >
            {userAgent}
          </span>
        );
      },
    };
    const ipAddressColumn: UsageColumnConfig<UsageLogResp> = {
      key: 'ip_address',
      title: t('usage.ip_address', 'IP'),
      width: '108px',
      hideOnMobile: true,
      render: (row) => {
        const ipAddress = compactText(row.ip_address);
        return (
          <span className="block max-w-full truncate font-mono text-xs leading-tight text-text-secondary" title={ipAddress}>
            {ipAddress}
          </span>
        );
      },
    };
    const wsDialColumn: UsageColumnConfig<UsageLogResp> = {
      key: 'ws_dial_ms',
      title: t('usage.ws_dial', 'WS'),
      width: '64px',
      hideOnMobile: true,
      render: (row) => (
        <span className="block text-center font-mono text-[13px] text-text-secondary">
          {formatUsageTimingMs(row.ws_dial_ms)}
        </span>
      ),
    };
    return [
      ...adminColumns,
      ...leadingSharedColumns,
      ...(streamColumn ? [streamColumn] : []),
      wsDialColumn,
      ...timingColumns,
      ...sharedColumnsAfterModel,
      endpointColumn,
      apiKeyColumn,
      accountColumn,
      ipAddressColumn,
      userAgentColumn,
    ] as UsageColumnConfig<UsageLogResp>[];
  }, [sharedColumns, t]);

  const columnOptions = useMemo(
    () => allColumns.map((column) => ({
      key: column.key,
      label: typeof column.title === 'string' ? column.title : column.key,
    })),
    [allColumns],
  );

  const selectedVisibleColumnKeys = useMemo(
    () => new Set(columnOptions.map((option) => option.key).filter((key) => selectedColumnKeys.has(key))),
    [columnOptions, selectedColumnKeys],
  );

  useEffect(() => {
    const validKeys = new Set(columnOptions.map((option) => option.key));
    setSelectedColumnKeys((current) => {
      const next = new Set(Array.from(current).filter((key) => validKeys.has(key)));
      if (next.size === 0) {
        for (const key of ADMIN_USAGE_DEFAULT_COLUMN_KEYS) {
          if (validKeys.has(key)) next.add(key);
        }
      }
      if (next.size === current.size && Array.from(next).every((key) => current.has(key))) {
        return current;
      }
      return next;
    });
  }, [columnOptions]);

  useEffect(() => {
    writeAdminUsageColumnKeys(selectedColumnKeys);
  }, [selectedColumnKeys]);

  const columns = useMemo(() => {
    const visible = allColumns.filter((column) => selectedVisibleColumnKeys.has(column.key));
    return visible.length > 0 ? visible : allColumns.slice(0, 1);
  }, [allColumns, selectedVisibleColumnKeys]);

  const selectedColumnCount = columns.length;
  const handleColumnToggle = useCallback((key: string) => {
    setSelectedColumnKeys((current) => {
      const next = new Set(current);
      if (next.has(key)) {
        if (next.size <= 1) return current;
        next.delete(key);
      } else {
        next.add(key);
      }
      return next;
    });
  }, []);

  const total = data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const canUseCursor = !isPlaceholderData;
  const summaryTotal = activeStats && !isSummaryStatsPlaceholderData ? activeStats.total_requests : undefined;

  return (
    <div>
      {/* 聚合统计 */}
      <div className="mb-6 space-y-4">
        {activeStats ? (
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4 2xl:gap-4">
            <StatCard
              title={t('usage.total_requests')}
              value={activeStats.total_requests.toLocaleString()}
              icon={<Activity className="w-5 h-5" />}
              tone="violet"
            />
            <StatCard
              title={t('usage.total_tokens')}
              value={fmtNum(activeStats.total_tokens)}
              icon={<Sigma className="w-5 h-5" />}
              tone="stream"
            />
            <StatCard
              title={t('usage.actual_cost')}
              value={<CostValue value={activeStats.total_actual_cost} decimals={4} tone="actual" />}
              icon={<DollarSign className="w-5 h-5" />}
              tone="amber"
            />
            <StatCard
              title={t('usage.total_cost')}
              value={<CostValue value={activeStats.total_cost} decimals={4} tone="standard" />}
              icon={<DollarSign className="w-5 h-5" />}
              tone="emerald"
            />
          </div>
        ) : (
          <StatsSkeleton />
        )}

        {activeStats && !usageCardsCollapsed ? (
          <>
            <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
              <DistributionCard
                title={t('usage.model_distribution')}
                firstColumnTitle={t('usage.model')}
                firstColumnWidth="30%"
                data={modelDistribution}
              />
              <DistributionCard
                title={t('usage.group_distribution')}
                firstColumnTitle={t('groups.group')}
                firstColumnWidth="26%"
                data={groupDistribution}
              />
            </div>

            <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
              <TokenTrendCard
                data={trendData ?? []}
                granularity={granularity}
                onGranularityChange={setGranularity}
              />
              <GroupStatsCard
                activeKey={statsGroupBy}
                rows={groupStatsRows}
                onActiveKeyChange={setStatsGroupBy}
              />
            </div>
          </>
        ) : null}
      </div>

      <TablePage
        className="ag-usage-page ag-toolbar-standard-page"
        footer={(
          <TablePaginationFooter
            page={page}
            pageSize={pageSize}
            pageSizeOptions={PAGE_SIZE_OPTIONS}
            setPage={(nextPage) => setPage(nextPage, canUseCursor ? data?.next_cursor : undefined)}
            setPageSize={setPageSize}
            summaryTotal={summaryTotal}
            summaryTotalExact={summaryTotal != null ? true : undefined}
            total={total}
            hasMore={canUseCursor ? data?.has_more : false}
            totalExact={canUseCursor ? data?.total_exact : true}
            totalPages={totalPages}
          />
        )}
        isFetching={isPlaceholderData && isUsageFetching && !isLoading}
      >
      {/* 筛选栏 */}
      <div className="ag-page-toolbar">
        <div className="ag-page-toolbar-filters">
          <div className="ag-page-toolbar-filter-row">
            <div className="w-full sm:w-72">
              <UsageDateRangeFilter
                clearLabel={t('common.clear')}
                endDate={filters.end_date}
                label={t('usage.time_range')}
                startDate={filters.start_date}
                onChange={(startDate, endDate) => {
                  resetCursorPagination();
                  setFilters((prev) => ({ ...prev, start_date: startDate, end_date: endDate }));
                }}
              />
            </div>
            <div className="w-full sm:w-48">
              <SimpleSelect
                ariaLabel={t('usage.platform')}
                fullWidth
                items={platformOptions.map((item) => ({ key: item.id, label: item.label }))}
                selectedKey={filters.platform || ''}
                selectedLabel={filters.platform ? selectedPlatformLabel : (
                  <span className="text-text-tertiary">{t('usage.platform')}</span>
                )}
                onSelectionChange={(key) => updateFilter('platform', key)}
              />
            </div>
            <div className="w-full sm:w-48">
              <UsageModelFilterInput
                ariaLabel={t('usage.model', 'Model')}
                placeholder={t('usage.model_placeholder')}
                value={filters.model ?? ''}
                onModelChange={handleModelChange}
              />
            </div>
            <div className="w-full sm:w-48">
              <SearchFilterInput
                ariaLabel={t('usage.upstream_credential')}
                placeholder={t('usage.upstream_credential')}
                value={filters.account ?? ''}
                onSearchChange={handleAccountChange}
              />
            </div>
            <div className="w-full sm:w-48">
              <UserOrAPIKeySearchFilterComboBox
                ariaLabel={t('usage.search_user_or_api_key')}
                emptyPrompt={t('usage.search_user_or_api_key')}
                loadingLabel={t('common.loading')}
                noDataLabel={t('common.no_data')}
                placeholder={t('usage.search_api_key')}
                selectedKey={selectedSearchKey}
                selectedKind={selectedSearchKind}
                selectedLabel={selectedSearchLabel}
                onSelectionChange={handleUserOrAPIKeySelectionChange}
              />
            </div>
          </div>
        </div>
        <div className="ag-page-toolbar-actions">
          <AutoRefreshControl
            value={autoRefresh}
            options={ADMIN_AUTO_REFRESH_OPTIONS}
            label={autoRefreshLabel}
            offLabel={autoRefreshOffLabel}
            refreshButtonClassName="ag-auto-refresh-refresh--toolbar"
            triggerClassName="ag-auto-refresh-trigger--toolbar-fixed"
            ariaLabel={t('usage.auto_update')}
            refreshAriaLabel={t('common.refresh', 'Refresh')}
            onChange={setAutoRefresh}
            onAutoRefresh={handleAutoRefresh}
            onRefresh={handleManualRefresh}
            isAutoRefreshing={isUsageTableRefreshing}
            isRefreshing={isRefreshing}
          />
          <button
            type="button"
            aria-label={usageCardsCollapsed ? t('usage.show_analysis_cards') : t('usage.hide_analysis_cards')}
            aria-pressed={usageCardsCollapsed}
            className="ag-page-toolbar-button ag-usage-cards-toggle-button ag-toolbar-menu-trigger button button--sm button--secondary inline-flex items-center justify-center gap-2 whitespace-nowrap px-3"
            onClick={() => setUsageCardsCollapsed((value) => !value)}
          >
            {usageCardsCollapsed ? <ChevronDown className="ag-toolbar-menu-caret" aria-hidden="true" /> : <ChevronUp className="ag-toolbar-menu-caret" aria-hidden="true" />}
            <span className="ag-toolbar-menu-trigger-label">
              {usageCardsCollapsed ? t('usage.show_analysis_cards') : t('usage.hide_analysis_cards')}
            </span>
          </button>
          <ColumnVisibilityMenu
            label={t('usage.column_visibility', '列显示')}
            options={columnOptions}
            selectedCount={selectedColumnCount}
            selectedKeys={selectedVisibleColumnKeys}
            onToggle={handleColumnToggle}
          />
        </div>
      </div>

      {/* 使用记录表格 */}
      <RecordsTable
        ariaLabel={t('usage.title', 'Usage')}
        columns={columns}
        dataVersion={dataUpdatedAt}
        emptyDescription={t('usage.empty_description', '调整筛选条件后重试')}
        emptyTitle={t('common.no_data')}
        footer={false}
        highlightNewRows={autoRefreshEnabled && page === 1}
        highlightResetKey={JSON.stringify({ ...filters, page, pageSize })}
        hasMore={canUseCursor ? data?.has_more : false}
        isLoading={isLoading}
        mobileLayout="usageGridWithUser"
        page={page}
        pageSize={pageSize}
        rows={data?.list ?? []}
        setPage={(nextPage) => setPage(nextPage, canUseCursor ? data?.next_cursor : undefined)}
        setPageSize={setPageSize}
        summaryTotal={summaryTotal}
        summaryTotalExact={summaryTotal != null ? true : undefined}
        suppressHighlight={isPlaceholderData}
        total={total}
        totalExact={canUseCursor ? data?.total_exact : true}
      />
      </TablePage>
    </div>
  );
}
