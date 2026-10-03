import { PageToolbarFrame } from '../../shared/components/PageToolbar';
import { memo, startTransition, useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Card } from '../../shared/components/Card';
import { Skeleton } from '@heroui/react';
import { usageApi } from '../../shared/api/usage';
import { useCursorPagination } from '../../shared/hooks/useCursorPagination';
import { isUsagePaginationExpired, useUsagePageIndex } from '../../shared/hooks/useUsagePageIndex';
import { usePlatforms } from '../../shared/hooks/usePlatforms';
import { Activity, Columns3, DollarSign, Sigma } from 'lucide-react';
import { UsageRichTooltipProvider, createUsageClientColumn, useUsageColumns, fmtNum, type UsageColumnConfig } from '../../shared/columns/usageColumns';
import type { UsageLogResp, UsageQuery } from '../../shared/types';
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
import { PAGE_SIZE_OPTIONS } from '../../shared/constants';
import { CostValue } from '../../shared/components/CostValue';
import { AutoRefreshControl } from '../../shared/components/AutoRefreshControl';
import { ToolbarMenu, ToolbarMenuItem } from '../../shared/components/ToolbarMenu';
import { SimpleSelect } from '../../shared/components/SimpleSelect';
import { ADMIN_AUTO_REFRESH_OPTIONS, usePersistentAutoRefresh } from '../../shared/hooks/usePersistentAutoRefresh';
import { STORAGE_KEYS } from '../../shared/storageKeys';
import { getTotalPages } from '../../shared/utils/pagination';
import { createPagedRowsStructuralSharing } from '../../shared/utils/structuralSharing';
import { type MetricTone, METRIC_TONE_CLASSES, METRIC_TONE_STYLES } from '../../shared/ui/metricTones';
import { combineUsageTimingColumns, createUsageTpsColumn, readUsageColumnSelection } from './usage/usageTimingColumns';

const EMPTY_USAGE_ROWS: UsageLogResp[] = [];
const shareAdminUsageRows = createPagedRowsStructuralSharing<UsageLogResp>();

interface ColumnVisibilityOption {
  key: string;
  label: string;
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
    <Card density="compact" className="ag-dashboard-metric ag-overview-metric-card min-h-[72px]">
      <Card.Content className="ag-dashboard-metric-content ag-overview-metric-content p-3">
        <div className="ag-dashboard-metric-copy">
          <div className="truncate text-sm font-semibold tracking-normal text-text-tertiary">{title}</div>
          <div className="mt-1 flex min-w-0 items-baseline gap-2">
            <div className="ag-overview-metric-value min-w-0 truncate font-mono text-[22px] font-semibold leading-none text-text">{value}</div>
          </div>
        </div>
        <span
          className={`ag-overview-metric-badge h-11 w-11 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1 shadow-sm ${METRIC_TONE_CLASSES[tone]}`}
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
    <div className="ag-overview-metrics-grid grid gap-3">
      {Array.from({ length: 4 }).map((_, index) => (
        <Card density="compact" className="ag-dashboard-metric ag-overview-metric-card min-h-[72px]" key={index}>
          <Card.Content className="ag-dashboard-metric-content ag-overview-metric-content p-3">
            <div className="ag-dashboard-metric-copy space-y-2">
              <Skeleton className="h-3 w-24" />
              <Skeleton className="h-6 w-24" />
            </div>
            <Skeleton className="ag-overview-metric-badge ag-overview-metric-badge-skeleton h-11 w-11 shrink-0 rounded-[var(--field-radius)]" />
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
      className="ag-page-toolbar-button button button--sm button--secondary inline-flex min-w-0 items-center justify-center gap-2 whitespace-nowrap px-3"
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

const ADMIN_USAGE_AUTO_UPDATE_STORAGE_KEY = STORAGE_KEYS.ui.adminUsageAutoRefresh;
const ADMIN_USAGE_COLUMN_STORAGE_KEY = STORAGE_KEYS.ui.adminUsageColumns;
const ADMIN_USAGE_FILTER_STORAGE_KEY = STORAGE_KEYS.ui.adminUsageFilters;
const ADMIN_USAGE_DEFAULT_COLUMN_KEYS = [
  'user_id',
  'created_at',
  'model',
  'stream',
  'ws_dial_ms',
  'first_event_ms',
  'latency',
  'tps',
  'tokens',
  'cost',
  'endpoint',
  'api_key',
  'account_name',
  'client',
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
    const keys = readUsageColumnSelection(parsed, ADMIN_USAGE_DEFAULT_COLUMN_KEYS);
    // Migrate the former separate IP and User-Agent preferences to the
    // compact client column without resetting the rest of the user's layout.
    if (keys.has('ip_address') || keys.has('user_agent')) {
      keys.delete('ip_address');
      keys.delete('user_agent');
      keys.add('client');
    }
    if (keys.has('first_token_ms') || keys.has('duration_ms')) {
      keys.delete('first_token_ms');
      keys.delete('duration_ms');
      keys.add('latency');
    }
    return keys;
  } catch {
    return new Set<string>(ADMIN_USAGE_DEFAULT_COLUMN_KEYS);
  }
}

function writeAdminUsageColumnKeys(keys: Set<string>) {
  try {
    window.localStorage.setItem(ADMIN_USAGE_COLUMN_STORAGE_KEY, JSON.stringify({ version: 2, keys: Array.from(keys) }));
  } catch {
    // localStorage may be unavailable in restricted browser modes.
  }
}

// ==================== 主页面 ====================

export default function UsagePage() {
  const { t } = useTranslation();
  const { activeSnapshot, beforeId, page, setPage, pageSize, setPageSize, resetCursorPagination } = useCursorPagination(20, 'admin.usage');
  const [initialFilterState] = useState<AdminUsageFilterState>(readAdminUsageFilterState);
  const [filters, setFilters] = useState<Partial<UsageQuery>>(() => initialFilterState.filters);
  const { info: pageInfo, error: pageIndexError, refresh: refreshPageIndex } = useUsagePageIndex('admin', filters, page === 1 || !activeSnapshot);
  const paginationView = activeSnapshot ?? (pageInfo?.status === 'ready' ? pageInfo : undefined);
  const [selectedUserLabel, setSelectedUserLabel] = useState(initialFilterState.userLabel);
  const [selectedAPIKeyLabel, setSelectedAPIKeyLabel] = useState(initialFilterState.apiKeyLabel);
  const [selectedColumnKeys, setSelectedColumnKeys] = useState<Set<string>>(
    readAdminUsageColumnKeys,
  );
  const [autoRefresh, setAutoRefresh] = usePersistentAutoRefresh(ADMIN_USAGE_AUTO_UPDATE_STORAGE_KEY, 0, ADMIN_AUTO_REFRESH_OPTIONS);
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
    snapshot: activeSnapshot?.snapshot,
    ...filters,
  }), [activeSnapshot?.snapshot, beforeId, filters, page, pageSize]);

  // 使用记录列表
  const {
    data,
    dataUpdatedAt,
    isFetching: isUsageFetching,
    isLoading,
    isPlaceholderData,
    refetch: refetchUsage,
    error: usageError,
  } = useQuery({
    queryKey: ['admin-usage', queryParams],
    queryFn: ({ signal }) => usageApi.adminList(queryParams, { signal }),
    meta: { globalLoading: false },
    refetchOnReconnect: autoRefreshEnabled,
    refetchOnWindowFocus: autoRefreshEnabled,
    placeholderData: keepPreviousData,
    structuralSharing: shareAdminUsageRows,
    retry: (count, error) => !isUsagePaginationExpired(error) && count < 2,
  });

  useEffect(() => {
    if (!isUsagePaginationExpired(usageError)) return;
    resetCursorPagination();
    refreshPageIndex();
  }, [usageError, resetCursorPagination, refreshPageIndex]);

  const refreshPagination = useCallback(() => {
    resetCursorPagination();
    refreshPageIndex();
    if (page === 1) void refetchUsage({ cancelRefetch: false });
  }, [page, refetchUsage, refreshPageIndex, resetCursorPagination]);

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

  const isRefreshing = isUsageFetching || isSummaryStatsFetching;
  const isUsageTableRefreshing = isUsageFetching;

  const handleManualRefresh = useCallback(() => {
    refreshPagination();
    void refetchSummaryStats({ cancelRefetch: false });
  }, [refetchSummaryStats, refreshPagination]);

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
      .map((column) => (column.key === 'model' ? { ...column, width: '256px' } : column));
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
    const clientColumn = createUsageClientColumn(t);
    const wsDialColumn: UsageColumnConfig<UsageLogResp> = {
      key: 'ws_dial_ms',
      title: t('usage.ws_dial', 'WS'),
      width: '64px',
      hideOnMobile: true,
      render: (row) => (
        <span className="block text-center font-mono text-[13px] text-text-tertiary">
          {formatUsageTimingMs(row.ws_dial_ms)}
        </span>
      ),
    };
    const tpsColumn = createUsageTpsColumn<UsageLogResp>(t);
    return [
      ...adminColumns,
      ...leadingSharedColumns,
      ...(streamColumn ? [streamColumn] : []),
      wsDialColumn,
      ...timingColumns,
      tpsColumn,
      ...sharedColumnsAfterModel,
      endpointColumn,
      apiKeyColumn,
      accountColumn,
      clientColumn,
    ] as UsageColumnConfig<UsageLogResp>[];
  }, [sharedColumns, t]);

  const columnOptions = useMemo(() => {
    return allColumns
      .flatMap((column) => {
        if (column.key === 'duration_ms') return [];
        if (column.key === 'first_token_ms') {
          return [{ key: 'latency', label: `${t('usage.first_token')}/${t('usage.duration')}` }];
        }
        return [{
          key: column.key,
          label: typeof column.title === 'string' ? column.title : column.key,
        }];
      });
  }, [allColumns, t]);

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
    const selectedActualKeys = new Set(selectedVisibleColumnKeys);
    if (selectedVisibleColumnKeys.has('latency')) {
      selectedActualKeys.add('first_token_ms');
      selectedActualKeys.add('duration_ms');
    }
    const visible = allColumns.filter((column) => selectedActualKeys.has(column.key));
    return combineUsageTimingColumns(visible.length > 0 ? visible : allColumns.slice(0, 1));
  }, [allColumns, selectedVisibleColumnKeys]);

  const selectedColumnCount = selectedVisibleColumnKeys.size;
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

  const total = paginationView?.total ?? data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const canUseCursor = !isPlaceholderData;
  const summaryTotal = paginationView?.total;
  const totalExact = Boolean(paginationView || (canUseCursor && data?.total_exact));
  const paginationStatus = paginationView ? 'ready' : pageIndexError ? 'failed' : pageInfo?.status ?? 'preparing';
  const highlightResetKey = useMemo(
    () => JSON.stringify({ ...filters, page, pageSize }),
    [filters, page, pageSize],
  );

  return (
    <div>
      {/* 聚合统计 */}
      <div className="mb-6 space-y-4">
        {activeStats ? (
          <div className="ag-overview-metrics-grid grid gap-3">
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

      </div>

      <TablePage
        className="ag-usage-page ag-toolbar-standard-page"
        footer={(
          <TablePaginationFooter
            page={page}
            pageSize={pageSize}
            pageSizeOptions={PAGE_SIZE_OPTIONS}
            setPage={(nextPage) => setPage(nextPage, canUseCursor ? data?.next_cursor : undefined, pageInfo)}
            setPageSize={setPageSize}
            summaryTotal={summaryTotal}
            summaryTotalExact={summaryTotal != null ? true : undefined}
            total={total}
            hasMore={canUseCursor ? data?.has_more : false}
            totalExact={totalExact}
            totalPages={totalPages}
            enablePageJump={Boolean(paginationView)}
            paginationStatus={paginationStatus}
            isPaginationRefreshing={!activeSnapshot && pageInfo?.refreshing}
            snapshotAt={paginationView?.created_at}
            onRefreshPagination={refreshPagination}
          />
        )}
        isFetching={isPlaceholderData && isUsageFetching && !isLoading}
      >
      {/* 筛选栏 */}
      <PageToolbarFrame>
        <div className="ag-page-toolbar-filters">
          <div className="ag-page-toolbar-filter-row">
            <div className="ag-toolbar-calendar">
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
            <div className="ag-toolbar-control">
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
            <div className="ag-toolbar-control">
              <UsageModelFilterInput
                ariaLabel={t('usage.model', 'Model')}
                placeholder={t('usage.model_placeholder')}
                value={filters.model ?? ''}
                onModelChange={handleModelChange}
              />
            </div>
            <div className="ag-toolbar-control">
              <SearchFilterInput
                ariaLabel={t('usage.upstream_credential')}
                placeholder={t('usage.upstream_credential')}
                value={filters.account ?? ''}
                onSearchChange={handleAccountChange}
              />
            </div>
            <div className="ag-toolbar-control">
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
        <div className="ag-page-toolbar-actions ag-usage-toolbar-actions">
          <AutoRefreshControl
            value={autoRefresh}
            options={ADMIN_AUTO_REFRESH_OPTIONS}
            label={autoRefreshLabel}
            offLabel={autoRefreshOffLabel}
            refreshButtonClassName="ag-auto-refresh-refresh--toolbar"
            showRefreshButton={false}
            triggerClassName="ag-auto-refresh-trigger--toolbar-fixed"
            ariaLabel={t('usage.auto_update')}
            refreshAriaLabel={t('common.refresh', 'Refresh')}
            onChange={setAutoRefresh}
            onAutoRefresh={handleAutoRefresh}
            onRefresh={handleManualRefresh}
            isAutoRefreshing={isUsageTableRefreshing}
            isRefreshing={isRefreshing}
          />
          <ColumnVisibilityMenu
            label={t('usage.column_visibility', '列显示')}
            options={columnOptions}
            selectedCount={selectedColumnCount}
            selectedKeys={selectedVisibleColumnKeys}
            onToggle={handleColumnToggle}
          />
        </div>
      </PageToolbarFrame>

      {/* 使用记录表格 */}
      <UsageRichTooltipProvider>
        <RecordsTable
          ariaLabel={t('usage.title', 'Usage')}
          columns={columns}
          dataVersion={dataUpdatedAt}
          emptyDescription={t('usage.empty_description', '调整筛选条件后重试')}
          emptyTitle={t('common.no_data')}
          footer={false}
          highlightNewRows={autoRefreshEnabled && page === 1}
          highlightResetKey={highlightResetKey}
          hasMore={canUseCursor ? data?.has_more : false}
          isLoading={isLoading}
          mobileLayout="usageGridWithUser"
          page={page}
          pageSize={pageSize}
          rows={data?.list ?? EMPTY_USAGE_ROWS}
          setPage={(nextPage) => setPage(nextPage, canUseCursor ? data?.next_cursor : undefined, pageInfo)}
          setPageSize={setPageSize}
          summaryTotal={summaryTotal}
          summaryTotalExact={summaryTotal != null ? true : undefined}
          suppressHighlight={isPlaceholderData}
          total={total}
          totalExact={totalExact}
        />
      </UsageRichTooltipProvider>
      </TablePage>
    </div>
  );
}
