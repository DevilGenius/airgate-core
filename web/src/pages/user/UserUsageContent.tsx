import { useCallback, useEffect, useMemo, useState, type CSSProperties, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Button, Card, Meter } from '@heroui/react';
import { usageApi } from '../../shared/api/usage';
import { queryKeys } from '../../shared/queryKeys';
import { useCursorPagination } from '../../shared/hooks/useCursorPagination';
import { usePlatforms } from '../../shared/hooks/usePlatforms';
import { useAuth } from '../../app/providers/AuthProvider';
import { useToast } from '../../shared/ui';
import { Activity, DollarSign, Clock, Gauge, Percent, Sigma, Upload } from 'lucide-react';
import type { UsageQuery } from '../../shared/types';
import { useUsageColumns, fmtNum, type UsageColumnConfig, type UsageRow } from '../../shared/columns/usageColumns';
import { getSessionAPIKey } from '../../shared/api/client';
import { CcsImportModal } from './userkeys/CcsImportModal';
import { RecordsTable } from '../../shared/components/RecordsTable';
import { TablePage } from '../../shared/components/TablePage';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { UsageDateRangeFilter } from '../../shared/components/UsageDateRangeFilter';
import { UsageModelFilterInput } from '../../shared/components/UsageModelFilterInput';
import { APIKeySearchFilterComboBox } from '../../shared/components/APIKeySearchFilterComboBox';
import { CostValue } from '../../shared/components/CostValue';
import { AutoRefreshControl } from '../../shared/components/AutoRefreshControl';
import { SimpleSelect } from '../../shared/components/SimpleSelect';
import { PAGE_SIZE_OPTIONS } from '../../shared/constants';
import { USER_AUTO_REFRESH_OPTIONS, usePersistentAutoRefresh } from '../../shared/hooks/usePersistentAutoRefresh';
import { STORAGE_KEYS } from '../../shared/storageKeys';
import { getTotalPages } from '../../shared/utils/pagination';
import { formatRateMultiplier } from '../../shared/utils/rateMultiplier';

const USER_USAGE_AUTO_UPDATE_STORAGE_KEY = STORAGE_KEYS.ui.userUsageAutoRefresh;
const USER_USAGE_FILTER_STORAGE_KEY = STORAGE_KEYS.ui.userUsageFilters;

type MetricTone = 'violet' | 'amber' | 'indigo' | 'emerald' | 'stream';
const STREAM_BLUE = 'oklch(62.04% 0.1950 253.83)';

const METRIC_TONE_CLASSES: Record<MetricTone, string> = {
  amber: 'bg-amber-100 text-amber-600 ring-amber-200 dark:bg-amber-400/15 dark:text-amber-300 dark:ring-amber-400/25',
  emerald: '',
  indigo: 'bg-indigo-100 text-indigo-600 ring-indigo-200 dark:bg-indigo-400/15 dark:text-indigo-300 dark:ring-indigo-400/25',
  stream: '',
  violet: 'bg-violet-100 text-violet-600 ring-violet-200 dark:bg-violet-400/15 dark:text-violet-300 dark:ring-violet-400/25',
};

const METRIC_TONE_STYLES: Partial<Record<MetricTone, CSSProperties>> = {
  emerald: {
    background: 'color-mix(in srgb, var(--ag-success) 18%, var(--ag-surface))',
    boxShadow: '0 0 0 1px color-mix(in srgb, var(--ag-success) 34%, var(--ag-border)), var(--shadow-sm)',
    color: 'var(--ag-success)',
  },
  stream: {
    background: `color-mix(in srgb, ${STREAM_BLUE} 18%, transparent)`,
    boxShadow: `0 0 0 1px color-mix(in srgb, ${STREAM_BLUE} 34%, transparent), var(--shadow-sm)`,
    color: STREAM_BLUE,
  },
};

type StoredUserUsageFilters = {
  api_key_id?: number;
  api_key_label?: string;
  platform?: string;
};

type UserUsageFilterState = {
  apiKeyLabel: string;
  filters: Partial<UsageQuery>;
};

function readStoredPositiveID(value: unknown) {
  const parsed = typeof value === 'number' ? value : Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? Math.floor(parsed) : undefined;
}

function isStoredFilterRecord(value: unknown): value is Record<string, unknown> {
  return value != null && typeof value === 'object' && !Array.isArray(value);
}

function readUserUsageFilterState(customerScope: boolean): UserUsageFilterState {
  const fallback: UserUsageFilterState = { apiKeyLabel: '', filters: {} };
  if (typeof window === 'undefined') return fallback;

  try {
    const raw = window.localStorage.getItem(USER_USAGE_FILTER_STORAGE_KEY);
    if (!raw) return fallback;
    const parsed = JSON.parse(raw);
    if (!isStoredFilterRecord(parsed)) return fallback;

    const filters: Partial<UsageQuery> = {};
    const platform = customerScope ? '' : (typeof parsed.platform === 'string' ? parsed.platform.trim() : '');
    const apiKeyID = customerScope ? undefined : readStoredPositiveID(parsed.api_key_id);

    if (platform) filters.platform = platform;
    if (apiKeyID != null) filters.api_key_id = apiKeyID;

    return {
      apiKeyLabel: apiKeyID != null ? (typeof parsed.api_key_label === 'string' && parsed.api_key_label ? parsed.api_key_label : `#${apiKeyID}`) : '',
      filters,
    };
  } catch {
    return fallback;
  }
}

function writeUserUsageFilterState(filters: Partial<UsageQuery>, apiKeyLabel: string, customerScope: boolean) {
  if (typeof window === 'undefined') return;

  const stored: StoredUserUsageFilters = {};
  if (!customerScope && filters.platform) stored.platform = filters.platform;
  if (!customerScope && filters.api_key_id != null && filters.api_key_id > 0) {
    stored.api_key_id = filters.api_key_id;
    if (apiKeyLabel) stored.api_key_label = apiKeyLabel;
  }

  try {
    if (Object.keys(stored).length > 0) {
      window.localStorage.setItem(USER_USAGE_FILTER_STORAGE_KEY, JSON.stringify(stored));
    } else {
      window.localStorage.removeItem(USER_USAGE_FILTER_STORAGE_KEY);
    }
  } catch {
    // localStorage may be unavailable in restricted browser modes.
  }
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

function APIKeyInfoBar() {
  const { t } = useTranslation();
  const { user } = useAuth();
  const { toast } = useToast();
  const [ccsOpen, setCcsOpen] = useState(false);
  if (!user?.api_key_id) return null;

  const quota = user.api_key_quota_usd ?? 0;
  const used = user.api_key_used_quota ?? 0;
  const expiresAt = user.api_key_expires_at;
  const pct = quota > 0 ? Math.min((used / quota) * 100, 100) : 0;

  // 原文 Key 仅在 API Key 登录当次会话内通过 sessionStorage 暂存；刷新页面后丢失，
  // 此时按钮会提示用户重新登录。
  const sessionKey = getSessionAPIKey();
  const platform = user.api_key_platform || '';
  const canImportCcs = !!sessionKey;

  function handleImportCcs() {
    if (!sessionKey) {
      toast('error', t('user_keys.ccs_session_expired'));
      return;
    }
    setCcsOpen(true);
  }

  // 后端已经把"实际扣费倍率 × 销售倍率"折算成单一字段 api_key_rate，
  // 前端拿不到原始来源，避免通过 DevTools 推断 reseller 定价模型。
  const effectiveRate = user.api_key_rate;

  // 到期时间格式化
  let expiresLabel = '';
  let expiresWarning = false;
  if (expiresAt) {
    const d = new Date(expiresAt);
    const now = new Date();
    const diffDays = Math.ceil((d.getTime() - now.getTime()) / 86400000);
    expiresLabel = d.toLocaleDateString();
    expiresWarning = diffDays <= 7;
  }

  return (
    <Card className="mb-5">
      <Card.Content className="flex items-center gap-4 px-4 py-3 text-sm flex-wrap">
        {quota > 0 && (
          <div className="flex items-center gap-2">
            <Gauge className="w-3.5 h-3.5 text-text-tertiary" />
            <span className="text-text-tertiary">{t('auth.apikey_quota')}:</span>
            <span className={pct >= 90 ? 'text-danger font-medium' : 'text-text-secondary'}>
              ${used.toFixed(4)} / ${quota.toFixed(2)}
            </span>
            <Meter
              aria-label={t('auth.apikey_quota')}
              className="w-20"
              color={pct >= 90 ? 'danger' : pct >= 70 ? 'warning' : 'accent'}
              maxValue={100}
              minValue={0}
              size="sm"
              value={pct}
            >
              <Meter.Track>
                <Meter.Fill />
              </Meter.Track>
            </Meter>
          </div>
        )}

        {quota === 0 && (
          <div className="flex items-center gap-2 text-text-tertiary">
            <Gauge className="w-3.5 h-3.5" />
            <span>{t('auth.apikey_quota')}: {t('auth.apikey_unlimited')}</span>
          </div>
        )}

        {expiresAt && (
          <div className="flex items-center gap-2">
            <Clock className="w-3.5 h-3.5 text-text-tertiary" />
            <span className="text-text-tertiary">{t('auth.apikey_expires')}:</span>
            <span className={expiresWarning ? 'text-warning font-medium' : 'text-text-secondary'}>
              {expiresLabel}
            </span>
          </div>
        )}

        {!expiresAt && (
          <div className="flex items-center gap-2 text-text-tertiary">
            <Clock className="w-3.5 h-3.5" />
            <span>{t('auth.apikey_expires')}: {t('auth.apikey_never')}</span>
          </div>
        )}

        {effectiveRate != null && Number.isFinite(effectiveRate) && effectiveRate >= 0 && (
          <div className="flex items-center gap-2">
            <Percent className="w-3.5 h-3.5 text-text-tertiary" />
            <span className="text-text-tertiary">{t('auth.apikey_rate', '倍率')}:</span>
            <span className="text-text-secondary font-mono">{formatRateMultiplier(effectiveRate)}x</span>
          </div>
        )}

        <Button
          type="button"
          onPress={handleImportCcs}
          isDisabled={!canImportCcs}
          className="ml-auto"
          size="sm"
          variant="outline"
        >
          <Upload className="w-3.5 h-3.5" />
          <span>{t('user_keys.import_ccs')}</span>
        </Button>

        <CcsImportModal
          open={ccsOpen}
          ccsKeyValue={sessionKey}
          ccsPlatform={platform}
          onClose={() => setCcsOpen(false)}
        />
      </Card.Content>
    </Card>
  );
}

export default function UserUsageContent() {
  const { t } = useTranslation();
  const { user } = useAuth();
  const customerScope = !!user?.api_key_id;
  const { beforeId, page, setPage, pageSize, setPageSize, resetCursorPagination } = useCursorPagination(20, 'user.usage');
  const [initialFilterState] = useState(() => readUserUsageFilterState(customerScope));
  const [filters, setFilters] = useState<Partial<UsageQuery>>(() => initialFilterState.filters);
  const [selectedAPIKeyLabel, setSelectedAPIKeyLabel] = useState(initialFilterState.apiKeyLabel);
  const [autoRefresh, setAutoRefresh] = usePersistentAutoRefresh(USER_USAGE_AUTO_UPDATE_STORAGE_KEY, 0, USER_AUTO_REFRESH_OPTIONS);
  const autoRefreshEnabled = autoRefresh > 0;
  const autoRefreshLabel = `${t('usage.auto_update')} `;
  const autoRefreshOffLabel = t('usage.auto_update_off');

  const handleModelChange = useCallback((model: string) => {
    const nextModel = model || undefined;
    resetCursorPagination();
    setFilters((prev) => (prev.model === nextModel ? prev : { ...prev, model: nextModel }));
  }, [resetCursorPagination]);

  const queryParams = useMemo<UsageQuery>(() => ({
    page,
    page_size: pageSize,
    before_id: beforeId,
    ...filters,
  }), [beforeId, filters, page, pageSize]);

  const { platforms, platformName } = usePlatforms();
  const platformOptions = [
    { id: '', label: t('common.all') },
    ...platforms.map((p) => ({ id: p, label: platformName(p) })),
  ];
  const selectedPlatformLabel = filters.platform
    ? (platformOptions.find((item) => item.id === filters.platform)?.label ?? platformName(filters.platform))
    : t('common.all');

  const {
    data,
    dataUpdatedAt,
    isFetching: isUsageFetching,
    isLoading,
    isPlaceholderData,
    refetch: refetchUsage,
  } = useQuery({
    queryKey: queryKeys.userUsage(queryParams),
    queryFn: ({ signal }) => usageApi.list(queryParams, { signal }),
    meta: { globalLoading: false },
    refetchOnReconnect: autoRefreshEnabled,
    refetchOnWindowFocus: autoRefreshEnabled,
    placeholderData: keepPreviousData,
  });

  // 聚合统计（跟随筛选条件，独立于分页）
  const { data: stats, isFetching: isStatsFetching, refetch: refetchStats } = useQuery({
    queryKey: queryKeys.userUsageStats(filters),
    queryFn: ({ signal }) => usageApi.userStats(filters, { signal }),
    meta: { globalLoading: false },
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
  });

  const isRefreshing = isUsageFetching || isStatsFetching;
  const isUsageTableRefreshing = isUsageFetching;

  const handleManualRefresh = useCallback(() => {
    void refetchUsage({ cancelRefetch: false });
    void refetchStats({ cancelRefetch: false });
  }, [refetchStats, refetchUsage]);

  const handleAutoRefresh = useCallback(() => {
    void refetchUsage({ cancelRefetch: false });
  }, [refetchUsage]);

  function updateFilter(key: string, value: string) {
    const nextValue = key === 'api_key_id' && value ? Number(value) : value || undefined;
    setFilters((prev) => ({ ...prev, [key]: nextValue }));
    resetCursorPagination();
  }

  useEffect(() => {
    if (!customerScope) return;
    setSelectedAPIKeyLabel('');
    setFilters((prev) => {
      if (prev.api_key_id == null && !prev.platform) return prev;
      const next = { ...prev };
      delete next.api_key_id;
      delete next.platform;
      return next;
    });
  }, [customerScope]);

  useEffect(() => {
    writeUserUsageFilterState(filters, selectedAPIKeyLabel, customerScope);
  }, [customerScope, filters.api_key_id, filters.platform, selectedAPIKeyLabel]);

  const list = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const summaryTotal = stats?.total_requests;
  const canUseCursor = !isPlaceholderData;
  const visibleActualCost = customerScope ? (stats?.total_billed_cost ?? 0) : (stats?.total_actual_cost ?? 0);

  const sharedColumns = useUsageColumns({ customerScope, adminView: false });
  const modelColumnIndex = sharedColumns.findIndex((column) => column.key === 'model');
  const timeColumnIndex = sharedColumns.findIndex((column) => column.key === 'created_at');
  const streamColumn = sharedColumns.find((column) => column.key === 'stream');
  const timingColumns = sharedColumns.filter((column) => column.key === 'first_token_ms' || column.key === 'duration_ms');
  const sharedColumnsAfterModel = sharedColumns
    .slice(modelColumnIndex + 1)
    .filter((column) => column.key !== 'first_token_ms' && column.key !== 'duration_ms' && column.key !== 'stream');
  const endpointColumn: UsageColumnConfig<UsageRow> = {
    key: 'endpoint',
    title: t('usage.endpoint', '端点'),
    width: '180px',
    hideOnMobile: true,
    render: (row) => {
      const endpoint = 'endpoint' in row && row.endpoint ? row.endpoint : '-';

      return (
        <span className="block truncate font-mono text-xs leading-tight text-text-secondary" title={endpoint}>
          {endpoint}
        </span>
      );
    },
  };
  const apiKeyColumn: UsageColumnConfig<UsageRow> = {
    key: 'api_key',
    title: 'API Key',
    width: '96px',
    hideOnMobile: true,
    render: (row) => {
      if (row.api_key_id === 0) {
        return <span className="block max-w-full truncate text-[13px] text-text-tertiary">{t('usage.api_key_plugin_call')}</span>;
      }
      if ('api_key_deleted' in row && row.api_key_deleted) {
        return <span className="block max-w-full truncate text-[13px] text-text-tertiary">{t('usage.api_key_deleted')}</span>;
      }

      const name = 'api_key_name' in row && row.api_key_name ? row.api_key_name : '-';

      return (
        <span className="block max-w-full truncate text-xs text-text-secondary" title={name}>{name}</span>
      );
    },
  };
  const columns = modelColumnIndex >= 0
    ? [
        ...sharedColumns.slice(0, timeColumnIndex + 1),
        ...(customerScope ? [] : [apiKeyColumn]),
        ...sharedColumns.slice(timeColumnIndex + 1, modelColumnIndex + 1),
        ...(streamColumn ? [streamColumn] : []),
        ...timingColumns,
        ...sharedColumnsAfterModel,
        endpointColumn,
      ]
    : [
        ...sharedColumns,
        endpointColumn,
        ...(customerScope ? [] : [apiKeyColumn]),
      ];

  return (
    <div>
      {/* API Key 登录信息 */}
      <APIKeyInfoBar />

      {/* 概览统计 */}
      <div className={`mb-6 grid grid-cols-1 gap-3 ${customerScope ? 'md:grid-cols-3 xl:grid-cols-3' : 'md:grid-cols-2 xl:grid-cols-4'} 2xl:gap-4`}>
        <StatCard
          title={t('usage.total_requests')}
          value={(stats?.total_requests ?? 0).toLocaleString()}
          icon={<Activity className="w-5 h-5" />}
          tone="violet"
        />
        <StatCard
          title={t('usage.total_tokens')}
          value={fmtNum(stats?.total_tokens ?? 0)}
          icon={<Sigma className="w-5 h-5" />}
          tone="stream"
        />
        <StatCard
          title={t('usage.actual_cost')}
          value={<CostValue value={visibleActualCost} decimals={4} tone="actual" />}
          icon={<DollarSign className="w-5 h-5" />}
          tone="amber"
        />
        {!customerScope && (
          <StatCard
            title={t('usage.total_cost')}
            value={<CostValue value={stats?.total_cost ?? 0} decimals={4} tone="standard" />}
            icon={<DollarSign className="w-5 h-5" />}
            tone="emerald"
          />
        )}
      </div>

      <TablePage
        className="ag-usage-page"
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
            {!customerScope && (
              <div className="w-full sm:w-48">
                <APIKeySearchFilterComboBox
                  ariaLabel="API Key"
                  emptyPrompt="API Key"
                  loadingLabel={t('common.loading')}
                  noDataLabel={t('common.no_data')}
                  placeholder="API Key"
                  scope="user"
                  selectedKey={filters.api_key_id ? String(filters.api_key_id) : null}
                  selectedLabel={selectedAPIKeyLabel}
                  onSelectionChange={(key, label) => {
                    setSelectedAPIKeyLabel(key ? label : '');
                    updateFilter('api_key_id', key);
                  }}
                />
              </div>
            )}
            <div className="w-full sm:w-48">
              <UsageModelFilterInput
                ariaLabel={t('usage.model', 'Model')}
                placeholder={t('usage.model_placeholder')}
                value={filters.model ?? ''}
                onModelChange={handleModelChange}
              />
            </div>
          </div>
        </div>
        <div className="ag-page-toolbar-actions">
          <AutoRefreshControl
            value={autoRefresh}
            options={USER_AUTO_REFRESH_OPTIONS}
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
        mobileLayout="usageGrid"
        page={page}
        pageSize={pageSize}
        rows={list}
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
