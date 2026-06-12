import { startTransition, useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Button, Card } from '@heroui/react';
import { AlertTriangle, CheckCircle2, EyeOff, RefreshCw, ShieldAlert, Trash2, TriangleAlert } from 'lucide-react';
import { monitorApi } from '../../shared/api/monitor';
import { subscribeAdminEvents } from '../../shared/api/adminEvents';
import { queryKeys } from '../../shared/queryKeys';
import { APIKeySearchFilterComboBox } from '../../shared/components/APIKeySearchFilterComboBox';
import { RecordsTable } from '../../shared/components/RecordsTable';
import { TablePage } from '../../shared/components/TablePage';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { UsageDateRangeFilter } from '../../shared/components/UsageDateRangeFilter';
import { SimpleSelect } from '../../shared/components/SimpleSelect';
import { PAGE_SIZE_OPTIONS } from '../../shared/constants';
import { getTotalPages } from '../../shared/utils/pagination';
import { useToast } from '../../shared/ui';
import { fmtNum } from '../../shared/columns/usageColumns';
import type {
  MonitorCursorResp,
  MonitorEventResp,
  MonitorListQuery,
  MonitorRequestCursorResp,
  MonitorRequestEventResp,
  MonitorRequestListQuery,
  MonitorSummaryResp,
} from '../../shared/types';

type SelectOption = {
  id: string;
  label: string;
};

type MonitorColumnConfig = {
  key: string;
  title: ReactNode;
  width?: string;
  hideOnMobile?: boolean;
  render: (row: MonitorEventResp) => ReactNode;
};

type MonitorRequestColumnConfig = {
  key: string;
  title: ReactNode;
  width?: string;
  hideOnMobile?: boolean;
  render: (row: MonitorRequestEventResp) => ReactNode;
};

type MonitorTableKey = 'events' | 'requests';
type MonitorTableRow = MonitorEventResp | MonitorRequestEventResp;
type MonitorTableColumnConfig = {
  key: string;
  title: ReactNode;
  width?: string;
  hideOnMobile?: boolean;
  render: (row: MonitorTableRow) => ReactNode;
};

const DEFAULT_PAGE_SIZE = 20;
const MONITOR_COLUMN_WIDTHS = {
  time: '100px',
  severity: '116px',
  event: '300px',
  source: '220px',
  subject: '200px',
  detail: '240px',
  status: '116px',
  actions: '96px',
};

const SEVERITY_CLASSES: Record<string, string> = {
  critical: 'bg-danger/10 text-danger ring-danger/20',
  error: 'bg-rose-100 text-rose-700 ring-rose-200 dark:bg-rose-400/15 dark:text-rose-300 dark:ring-rose-400/25',
  warning: 'bg-amber-100 text-amber-700 ring-amber-200 dark:bg-amber-400/15 dark:text-amber-300 dark:ring-amber-400/25',
};

const STATUS_CLASSES: Record<string, string> = {
  active: 'bg-amber-100 text-amber-700 ring-amber-200 dark:bg-amber-400/15 dark:text-amber-300 dark:ring-amber-400/25',
  ignored: 'bg-zinc-100 text-zinc-700 ring-zinc-200 dark:bg-zinc-400/15 dark:text-zinc-300 dark:ring-zinc-400/25',
  resolved: 'bg-emerald-100 text-emerald-700 ring-emerald-200 dark:bg-emerald-400/15 dark:text-emerald-300 dark:ring-emerald-400/25',
};

const MONITOR_TIME_FORMATTER = new Intl.DateTimeFormat('zh-CN', {
  hour: '2-digit',
  hour12: false,
  minute: '2-digit',
  second: '2-digit',
});
const MONITOR_DATE_FORMATTER = new Intl.DateTimeFormat('zh-CN');

function monitorTimeLabels(value?: string) {
  if (!value) {
    return { dateLabel: '', fullLabel: '-', timeLabel: '-' };
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return { dateLabel: '', fullLabel: value, timeLabel: value };
  }
  const timeLabel = MONITOR_TIME_FORMATTER.format(date);
  const dateLabel = MONITOR_DATE_FORMATTER.format(date);
  return { dateLabel, fullLabel: `${dateLabel} ${timeLabel}`, timeLabel };
}

function detailString(detail: Record<string, unknown> | undefined, key: string): string {
  const value = detail?.[key];
  if (typeof value === 'string') return value.trim();
  if (typeof value === 'number' && Number.isFinite(value)) return String(value);
  return '';
}

function detailNumber(detail: Record<string, unknown> | undefined, key: string): number | undefined {
  const value = detail?.[key];
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim() !== '') {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) return parsed;
  }
  return undefined;
}

function namedIDLabel(name?: string, id?: number | string): string {
  const trimmedName = name?.trim() ?? '';
  const idText = id === undefined || id === null || id === '' ? '' : String(id);
  if (trimmedName && idText) return `${trimmedName} #${idText}`;
  if (trimmedName) return trimmedName;
  if (idText) return `#${idText}`;
  return '';
}

function monitorGroupLabel(event: MonitorEventResp | MonitorRequestEventResp): string {
  const detail = event.detail;
  const groupName = detailString(detail, 'group_name') || detailString(detail, 'group_name_snapshot');
  const groupID = 'group_id' in event && event.group_id !== undefined
    ? event.group_id
    : detailNumber(detail, 'group_id') ?? ('subject_type' in event && event.subject_type === 'scheduler' ? event.subject_id : undefined);
  return namedIDLabel(groupName, groupID);
}

function monitorAccountLabel(event: MonitorEventResp | MonitorRequestEventResp): string {
  return namedIDLabel(event.account_name_snapshot, event.account_id);
}

function monitorSubject(event: MonitorEventResp): string {
  return monitorAccountLabel(event)
    || monitorGroupLabel(event)
    || namedIDLabel(event.plugin_id, event.subject_id)
    || event.subject_id
    || '-';
}

function monitorSubjectContext(event: MonitorEventResp): string {
  return [event.subject_type, event.platform].filter(Boolean).join(' · ');
}

function monitorLocatorContext(event: MonitorEventResp): string {
  return [event.source, event.plugin_id].filter(Boolean).join(' · ');
}

function monitorRequestSubject(event: MonitorRequestEventResp): string {
  return namedIDLabel(event.api_key_name_snapshot, event.api_key_id)
    || monitorAccountLabel(event)
    || monitorGroupLabel(event)
    || event.plugin_id
    || event.request_id
    || '-';
}

function monitorRequestSubjectContext(event: MonitorRequestEventResp): string {
  return [
    monitorGroupLabel(event),
    event.user_email_snapshot || namedIDLabel(undefined, event.user_id),
    event.platform,
  ].filter(Boolean).join(' · ');
}

function requestEndpointLabel(event: MonitorRequestEventResp): string {
  return [event.method, event.endpoint].filter(Boolean).join(' ') || event.source || '-';
}

function requestEndpointContext(event: MonitorRequestEventResp): string {
  return [event.model, event.plugin_id].filter(Boolean).join(' · ');
}

function requestStatusLabel(event: MonitorRequestEventResp): string {
  const status = event.http_status ? String(event.http_status) : '-';
  if (!event.upstream_status || event.upstream_status === event.http_status) {
    return status;
  }
  return `${status} / ${event.upstream_status}`;
}

function requestErrorCodeLabel(event: MonitorRequestEventResp): string {
  return event.error_code || '-';
}

function requestStatusToneClass(event: MonitorRequestEventResp): string {
  const status = Math.max(event.http_status ?? 0, event.upstream_status ?? 0);
  if (status >= 500) return 'text-danger';
  if (status >= 400) return 'text-amber-600 dark:text-amber-400';
  return 'text-text-secondary';
}

type DetailEntry = {
  key: string;
  value: string;
};

function detailValue(detail: Record<string, unknown> | undefined, key: string): string {
  const value = detail?.[key];
  if (typeof value === 'string') return value.trim();
  if (typeof value === 'number' && Number.isFinite(value)) return String(value);
  if (typeof value === 'boolean') return value ? 'true' : 'false';
  return '';
}

function durationMsLabel(value?: number | string): string {
  const duration = typeof value === 'string' ? Number(value) : value;
  if (!duration || !Number.isFinite(duration) || duration <= 0) return '';
  if (duration >= 10_000) return `${(duration / 1000).toFixed(1)}s`;
  if (duration >= 1000) return `${(duration / 1000).toFixed(2)}s`;
  return `${duration}ms`;
}

function appendDetail(entries: DetailEntry[], key: string, value?: string | number | boolean) {
  const normalized = typeof value === 'string'
    ? value.trim()
    : typeof value === 'number' && Number.isFinite(value)
      ? String(value)
      : typeof value === 'boolean'
        ? value ? 'true' : 'false'
        : '';
  if (!normalized) return;
  if (entries.some((item) => item.key === key && item.value === normalized)) return;
  entries.push({ key, value: normalized });
}

function detailText(entries: DetailEntry[]): string {
  return entries.map((entry) => `${entry.key}=${entry.value}`).join(' · ');
}

function monitorDetailEntries(event: MonitorEventResp): DetailEntry[] {
  const detail = event.detail;
  const entries: DetailEntry[] = [];
  appendDetail(entries, 'model', detailValue(detail, 'model'));
  appendDetail(entries, 'client_model', detailValue(detail, 'client_model'));
  appendDetail(entries, 'http_status', detailValue(detail, 'http_status'));
  appendDetail(entries, 'attempts', detailValue(detail, 'attempts'));
  appendDetail(entries, 'request_id', detailValue(detail, 'request_id'));
  appendDetail(entries, 'request_path', detailValue(detail, 'request_path'));
  appendDetail(entries, 'stage', detailValue(detail, 'stage'));
  appendDetail(entries, 'duration_ms', durationMsLabel(detailValue(detail, 'duration_ms')));
  return entries;
}

function requestDetailEntries(event: MonitorRequestEventResp): DetailEntry[] {
  const detail = event.detail;
  const entries: DetailEntry[] = [];
  appendDetail(entries, 'duration_ms', durationMsLabel(event.duration_ms || detailValue(detail, 'duration_ms')));
  appendDetail(entries, 'request_id', event.request_id);
  appendDetail(entries, 'fingerprint', event.fingerprint);
  appendDetail(entries, 'upstream_status', event.upstream_status && event.upstream_status !== event.http_status ? event.upstream_status : undefined);
  appendDetail(entries, 'attempts', detailValue(detail, 'attempts'));
  appendDetail(entries, 'stage', detailValue(detail, 'stage'));
  appendDetail(entries, 'outcome_kind', detailValue(detail, 'outcome_kind'));
  appendDetail(entries, 'reason', detailValue(detail, 'reason'));
  return entries;
}

function statusLabel(t: ReturnType<typeof useTranslation>['t'], value: string): string {
  return t(`monitor.status_${value}`, value);
}

function severityLabel(t: ReturnType<typeof useTranslation>['t'], value: string): string {
  return t(`monitor.severity_${value}`, value);
}

function typeLabel(t: ReturnType<typeof useTranslation>['t'], value: string): string {
  return t(`monitor.type_${value}`, value);
}

function TimeCell({ value }: { value?: string }) {
  const { dateLabel, fullLabel, timeLabel } = monitorTimeLabels(value);
  return (
    <div className="flex min-w-0 flex-col justify-center gap-1 text-left" title={fullLabel}>
      <span className="truncate font-mono text-[13px] font-medium leading-none text-text">{timeLabel}</span>
      {dateLabel ? (
        <span className="truncate font-mono text-[11px] leading-none text-text-tertiary">{dateLabel}</span>
      ) : null}
    </div>
  );
}

function StackCell({
  mono,
  primary,
  primaryClass = 'text-text',
  primaryTitle,
  secondary,
  secondaryTitle,
}: {
  mono?: boolean;
  primary: ReactNode;
  primaryClass?: string;
  primaryTitle?: string;
  secondary?: ReactNode;
  secondaryTitle?: string;
}) {
  return (
    <div className="flex h-full w-full min-w-0 flex-col justify-center gap-1 text-left">
      <span className={`truncate text-[13px] leading-none ${mono ? 'font-mono ' : ''}${primaryClass}`} title={primaryTitle}>
        {primary}
      </span>
      {secondary ? (
        <span className="truncate text-[11px] leading-none text-text-tertiary" title={secondaryTitle}>
          {secondary}
        </span>
      ) : null}
    </div>
  );
}

function StatusPill({ className, label }: { className?: string; label: string }) {
  return (
    <span className={`inline-flex h-5 max-w-full items-center justify-center truncate rounded-[var(--radius)] px-2 text-xs font-medium leading-none ring-1 ${className ?? ''}`}>
      {label}
    </span>
  );
}

function DetailCell({ entries }: { entries: DetailEntry[] }) {
  if (entries.length === 0) {
    return <span className="block w-full truncate text-left text-[13px] leading-none text-text-tertiary">-</span>;
  }
  const title = detailText(entries);
  const primary = detailText(entries.slice(0, 2));
  const secondary = detailText(entries.slice(2));
  return (
    <StackCell
      mono
      primary={primary}
      primaryClass="text-text-secondary"
      primaryTitle={title}
      secondary={secondary || undefined}
      secondaryTitle={secondary ? title : undefined}
    />
  );
}

function FilterSelect({
  ariaLabel,
  className,
  label,
  onChange,
  options,
  value,
}: {
  ariaLabel: string;
  className?: string;
  label?: string;
  onChange: (value: string) => void;
  options: SelectOption[];
  value: string;
}) {
  const selected = options.find((item) => item.id === value)?.label ?? options[0]?.label ?? '';
  return (
    <div className={className}>
      <SimpleSelect
        ariaLabel={ariaLabel}
        fullWidth
        items={options.map((item) => ({ key: item.id, label: item.label }))}
        selectedKey={value}
        selectedLabel={label ? `${label}: ${selected}` : selected}
        onSelectionChange={onChange}
      />
    </div>
  );
}

function StatCard({
  icon,
  label,
  tone,
  value,
}: {
  icon: ReactNode;
  label: string;
  tone: string;
  value: number;
}) {
  return (
    <Card className="ag-dashboard-metric min-h-[72px] 2xl:min-h-[78px]">
      <Card.Content className="ag-dashboard-metric-content p-3 2xl:p-3.5">
        <div className="ag-dashboard-metric-copy">
          <div className="truncate text-sm font-semibold tracking-normal text-text-tertiary">{label}</div>
          <div className="mt-1 font-mono text-[22px] font-semibold leading-none text-text 2xl:text-2xl">{fmtNum(value)}</div>
        </div>
        <span className={`hidden h-11 w-11 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1 shadow-sm 2xl:flex ${tone}`}>
          {icon}
        </span>
      </Card.Content>
    </Card>
  );
}

function MonitorStats({ summary }: { summary?: MonitorSummaryResp }) {
  const { t } = useTranslation();
  return (
    <div className="mb-6 grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4 2xl:gap-4">
      <StatCard
        icon={<ShieldAlert className="h-5 w-5" />}
        label={t('monitor.active')}
        tone="bg-zinc-100 text-zinc-600 ring-zinc-200 dark:bg-zinc-400/15 dark:text-zinc-300 dark:ring-zinc-400/25"
        value={summary?.active_total ?? 0}
      />
      <StatCard
        icon={<TriangleAlert className="h-5 w-5" />}
        label={t('monitor.critical')}
        tone="bg-danger/10 text-danger ring-danger/20"
        value={summary?.critical_total ?? 0}
      />
      <StatCard
        icon={<AlertTriangle className="h-5 w-5" />}
        label={t('monitor.error')}
        tone="bg-rose-100 text-rose-600 ring-rose-200 dark:bg-rose-400/15 dark:text-rose-300 dark:ring-rose-400/25"
        value={summary?.error_total ?? 0}
      />
      <StatCard
        icon={<AlertTriangle className="h-5 w-5" />}
        label={t('monitor.warning')}
        tone="bg-amber-100 text-amber-600 ring-amber-200 dark:bg-amber-400/15 dark:text-amber-300 dark:ring-amber-400/25"
        value={summary?.warning_total ?? 0}
      />
    </div>
  );
}

function totalForCursorPage(page: number, pageSize: number, rows: number, hasMore?: boolean): number {
  return Math.max(0, (page - 1) * pageSize + rows + (hasMore ? 1 : 0));
}

export default function MonitorPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const queryClient = useQueryClient();
  const [activeTable, setActiveTable] = useState<MonitorTableKey>('events');
  const [page, setPageState] = useState(1);
  const [pageSize, setPageSizeState] = useState(DEFAULT_PAGE_SIZE);
  const [cursors, setCursors] = useState<Record<number, MonitorCursorResp | undefined>>({});
  const [filters, setFilters] = useState<Partial<MonitorListQuery>>({});
  const cursor = page > 1 ? cursors[page] : undefined;
  const [requestPage, setRequestPageState] = useState(1);
  const [requestPageSize, setRequestPageSizeState] = useState(DEFAULT_PAGE_SIZE);
  const [requestCursors, setRequestCursors] = useState<Record<number, MonitorRequestCursorResp | undefined>>({});
  const requestCursor = requestPage > 1 ? requestCursors[requestPage] : undefined;
  const [requestFilters, setRequestFilters] = useState<Partial<MonitorRequestListQuery>>({});
  const [selectedAPIKeyLabel, setSelectedAPIKeyLabel] = useState('');

  const listParams = useMemo<MonitorListQuery>(() => ({
    ...filters,
    cursor: cursor?.updated_at,
    cursor_id: cursor?.id,
    limit: pageSize,
  }), [cursor?.id, cursor?.updated_at, filters, pageSize]);
  const requestListParams = useMemo<MonitorRequestListQuery>(() => ({
    ...requestFilters,
    cursor: requestCursor?.created_at,
    cursor_id: requestCursor?.id,
    limit: requestPageSize,
  }), [requestCursor?.created_at, requestCursor?.id, requestFilters, requestPageSize]);

  const summaryQuery = useQuery({
    queryKey: queryKeys.monitorSummary(),
    queryFn: ({ signal }) => monitorApi.summary({ signal }),
    meta: { globalLoading: false },
    placeholderData: keepPreviousData,
  });
  const refetchSummary = summaryQuery.refetch;

  const {
    data,
    dataUpdatedAt,
    isFetching,
    isLoading,
    isPlaceholderData,
    refetch,
  } = useQuery({
    queryKey: queryKeys.monitor('list', listParams),
    queryFn: ({ signal }) => monitorApi.list(listParams, { signal }),
    meta: { globalLoading: false },
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
    placeholderData: keepPreviousData,
  });

  const {
    data: requestData,
    dataUpdatedAt: requestDataUpdatedAt,
    isFetching: isRequestFetching,
    isLoading: isRequestLoading,
    isPlaceholderData: isRequestPlaceholderData,
    refetch: refetchRequests,
  } = useQuery({
    queryKey: queryKeys.monitorRequests('list', requestListParams),
    queryFn: ({ signal }) => monitorApi.requestList(requestListParams, { signal }),
    meta: { globalLoading: false },
    refetchOnReconnect: false,
    refetchOnWindowFocus: false,
    placeholderData: keepPreviousData,
  });

  const invalidateMonitor = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.monitor() });
    void queryClient.invalidateQueries({ queryKey: queryKeys.monitorSummary() });
  }, [queryClient]);

  const invalidateRequestMonitor = useCallback(() => {
    void queryClient.invalidateQueries({ queryKey: queryKeys.monitorRequests() });
  }, [queryClient]);

  const resolveMutation = useMutation({
    mutationFn: monitorApi.resolve,
    onSuccess: () => {
      toast('success', t('monitor.resolve_success'));
      invalidateMonitor();
    },
    onError: (err: Error) => toast('error', err.message),
  });

  const ignoreMutation = useMutation({
    mutationFn: monitorApi.ignore,
    onSuccess: () => {
      toast('success', t('monitor.ignore_success'));
      invalidateMonitor();
    },
    onError: (err: Error) => toast('error', err.message),
  });

  const clearRequestsMutation = useMutation({
    mutationFn: () => monitorApi.clearRequests(),
    onSuccess: (result) => {
      toast('success', t('monitor.request_clear_success', { count: result.deleted }));
      setRequestCursors({});
      setRequestPageState(1);
      invalidateRequestMonitor();
    },
    onError: (err: Error) => toast('error', err.message),
  });

  const resetPagination = useCallback(() => {
    setCursors({});
    setPageState(1);
  }, []);

  const updateFilter = useCallback((key: keyof MonitorListQuery, value: string | number | undefined) => {
    const nextValue = value === '' ? undefined : value;
    startTransition(() => {
      setFilters((prev) => ({ ...prev, [key]: nextValue }));
      resetPagination();
    });
  }, [resetPagination]);

  const resetRequestPagination = useCallback(() => {
    setRequestCursors({});
    setRequestPageState(1);
  }, []);

  const updateRequestFilter = useCallback((key: keyof MonitorRequestListQuery, value: string | number | undefined) => {
    const nextValue = value === '' ? undefined : value;
    startTransition(() => {
      setRequestFilters((prev) => ({ ...prev, [key]: nextValue }));
      resetRequestPagination();
    });
  }, [resetRequestPagination]);

  const handleAPIKeySelectionChange = useCallback((value: string, label: string) => {
    setSelectedAPIKeyLabel(value ? label : '');
    updateRequestFilter('api_key_id', value ? Number(value) : undefined);
  }, [updateRequestFilter]);

  const setPage = useCallback((nextPage: number) => {
    if (nextPage <= 1) {
      setPageState(1);
      return;
    }
    if (nextPage === page + 1) {
      if (!data?.next_cursor) return;
      setCursors((current) => ({ ...current, [nextPage]: data.next_cursor }));
      setPageState(nextPage);
      return;
    }
    if (nextPage < page || cursors[nextPage]) {
      setPageState(nextPage);
    }
  }, [cursors, data?.next_cursor, page]);

  const setPageSize = useCallback((nextPageSize: number) => {
    setCursors({});
    setPageState(1);
    setPageSizeState(nextPageSize);
  }, []);

  const setRequestPage = useCallback((nextPage: number) => {
    if (nextPage <= 1) {
      setRequestPageState(1);
      return;
    }
    if (nextPage === requestPage + 1) {
      if (!requestData?.next_cursor) return;
      setRequestCursors((current) => ({ ...current, [nextPage]: requestData.next_cursor }));
      setRequestPageState(nextPage);
      return;
    }
    if (nextPage < requestPage || requestCursors[nextPage]) {
      setRequestPageState(nextPage);
    }
  }, [requestCursors, requestData?.next_cursor, requestPage]);

  const setRequestPageSize = useCallback((nextPageSize: number) => {
    setRequestCursors({});
    setRequestPageState(1);
    setRequestPageSizeState(nextPageSize);
  }, []);

  const handleManualRefresh = useCallback(() => {
    void refetch({ cancelRefetch: false });
    void refetchSummary({ cancelRefetch: false });
    void refetchRequests({ cancelRefetch: false });
  }, [refetch, refetchRequests, refetchSummary]);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return undefined;
    }

    let timeoutId: number | undefined;
    let pendingWhileHidden = false;
    const refreshFromEvent = () => {
      timeoutId = undefined;
      if (document.hidden) {
        pendingWhileHidden = true;
        return;
      }
      void refetch({ cancelRefetch: false });
      void refetchSummary({ cancelRefetch: false });
      void refetchRequests({ cancelRefetch: false });
    };
    const handleVisibilityChange = () => {
      if (document.hidden || !pendingWhileHidden) return;
      pendingWhileHidden = false;
      if (timeoutId !== undefined) {
        window.clearTimeout(timeoutId);
        timeoutId = undefined;
      }
      refreshFromEvent();
    };

    const unsubscribe = subscribeAdminEvents((event) => {
      if ((event.type !== 'monitor.changed' && event.type !== 'admin_events.reconnected') || timeoutId !== undefined) return;
      timeoutId = window.setTimeout(refreshFromEvent, 250);
    });
    document.addEventListener('visibilitychange', handleVisibilityChange);

    return () => {
      unsubscribe();
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      if (timeoutId !== undefined) {
        window.clearTimeout(timeoutId);
      }
    };
  }, [refetch, refetchRequests, refetchSummary]);

  const statusOptions: SelectOption[] = [
    { id: '', label: t('common.all') },
    { id: 'active', label: t('monitor.status_active') },
    { id: 'resolved', label: t('monitor.status_resolved') },
    { id: 'ignored', label: t('monitor.status_ignored') },
  ];
  const severityOptions: SelectOption[] = [
    { id: '', label: t('common.all') },
    { id: 'critical', label: t('monitor.severity_critical') },
    { id: 'error', label: t('monitor.severity_error') },
    { id: 'warning', label: t('monitor.severity_warning') },
  ];
  const typeOptions: SelectOption[] = [
    { id: '', label: t('common.all') },
    { id: 'scheduler_error', label: t('monitor.type_scheduler_error') },
    { id: 'upstream_account_error', label: t('monitor.type_upstream_account_error') },
    { id: 'plugin_error', label: t('monitor.type_plugin_error') },
    { id: 'task_error', label: t('monitor.type_task_error') },
    { id: 'system_error', label: t('monitor.type_system_error') },
  ];

  const httpStatusOptions: SelectOption[] = [
    { id: '', label: t('common.all') },
    ...['400', '401', '403', '404', '408', '429', '499', '500', '502', '503', '504'].map((code) => ({ id: code, label: code })),
  ];

  const columns = useMemo<MonitorColumnConfig[]>(() => [
    {
      key: 'updated_at',
      title: t('monitor.updated_at'),
      width: MONITOR_COLUMN_WIDTHS.time,
      render: (row) => <TimeCell value={row.updated_at} />,
    },
    {
      key: 'severity',
      title: t('monitor.severity'),
      width: MONITOR_COLUMN_WIDTHS.severity,
      render: (row) => (
        <div className="flex h-full w-full min-w-0 flex-col items-center justify-center gap-1">
          <StatusPill className={SEVERITY_CLASSES[row.severity] ?? SEVERITY_CLASSES.warning} label={severityLabel(t, row.severity)} />
          <span className="max-w-full truncate text-[11px] leading-none text-text-tertiary" title={typeLabel(t, row.type)}>
            {typeLabel(t, row.type)}
          </span>
        </div>
      ),
    },
    {
      key: 'event',
      title: t('monitor.event'),
      width: MONITOR_COLUMN_WIDTHS.event,
      render: (row) => (
        <StackCell
          primary={row.title || typeLabel(t, row.type)}
          primaryClass="font-medium text-text"
          primaryTitle={row.title || undefined}
          secondary={row.message || undefined}
          secondaryTitle={row.message || undefined}
        />
      ),
    },
    {
      key: 'locator',
      title: t('monitor.source'),
      width: MONITOR_COLUMN_WIDTHS.source,
      hideOnMobile: true,
      render: (row) => (
        <StackCell
          mono
          primary={monitorLocatorContext(row) || '-'}
          primaryClass="text-text-secondary"
          primaryTitle={monitorLocatorContext(row) || undefined}
          secondary={row.error_code || undefined}
          secondaryTitle={row.error_code || undefined}
        />
      ),
    },
    {
      key: 'subject',
      title: t('monitor.subject'),
      width: MONITOR_COLUMN_WIDTHS.subject,
      render: (row) => (
        <StackCell
          primary={monitorSubject(row)}
          primaryClass="font-medium text-text"
          primaryTitle={monitorSubject(row)}
          secondary={monitorSubjectContext(row) || undefined}
          secondaryTitle={monitorSubjectContext(row) || undefined}
        />
      ),
    },
    {
      key: 'detail',
      title: t('monitor.detail'),
      width: MONITOR_COLUMN_WIDTHS.detail,
      hideOnMobile: true,
      render: (row) => <DetailCell entries={monitorDetailEntries(row)} />,
    },
    {
      key: 'status',
      title: t('monitor.status'),
      width: MONITOR_COLUMN_WIDTHS.status,
      render: (row) => (
        <StatusPill className={STATUS_CLASSES[row.status] ?? STATUS_CLASSES.active} label={statusLabel(t, row.status)} />
      ),
    },
    {
      key: 'actions',
      title: t('common.actions'),
      width: MONITOR_COLUMN_WIDTHS.actions,
      render: (row) => {
        if (row.severity === 'warning') {
          return <span className="text-[13px] leading-none text-text-tertiary">-</span>;
        }
        const disabled = row.status !== 'active';
        return (
          <div className="flex h-full w-full items-center justify-center gap-1">
            <Button
              isIconOnly
              aria-label={t('monitor.resolve')}
              className="h-7 w-7 min-w-7"
              isDisabled={disabled || resolveMutation.isPending || ignoreMutation.isPending}
              size="sm"
              variant="ghost"
              onPress={() => resolveMutation.mutate(row.id)}
            >
              <CheckCircle2 className="h-4 w-4" />
            </Button>
            <Button
              isIconOnly
              aria-label={t('monitor.ignore')}
              className="h-7 w-7 min-w-7"
              isDisabled={disabled || resolveMutation.isPending || ignoreMutation.isPending}
              size="sm"
              variant="ghost"
              onPress={() => ignoreMutation.mutate(row.id)}
            >
              <EyeOff className="h-4 w-4" />
            </Button>
          </div>
        );
      },
    },
  ], [ignoreMutation, resolveMutation, t]);

  const requestColumns = useMemo<MonitorRequestColumnConfig[]>(() => [
    {
      key: 'created_at',
      title: t('monitor.time'),
      width: MONITOR_COLUMN_WIDTHS.time,
      render: (row) => <TimeCell value={row.created_at} />,
    },
    {
      key: 'event',
      title: t('monitor.event'),
      width: MONITOR_COLUMN_WIDTHS.event,
      render: (row) => (
        <StackCell
          primary={(
            <>
              {row.severity === 'warning' ? (
                <span aria-hidden className="mr-1.5 inline-block h-1.5 w-1.5 shrink-0 rounded-full bg-amber-500 align-middle" />
              ) : null}
              {row.title || typeLabel(t, row.type)}
            </>
          )}
          primaryClass="font-medium text-text"
          primaryTitle={row.title || undefined}
          secondary={row.message || typeLabel(t, row.type)}
          secondaryTitle={row.message || undefined}
        />
      ),
    },
    {
      key: 'locator',
      title: t('monitor.endpoint'),
      width: MONITOR_COLUMN_WIDTHS.source,
      hideOnMobile: true,
      render: (row) => (
        <StackCell
          mono
          primary={requestEndpointLabel(row)}
          primaryClass="text-text-secondary"
          primaryTitle={requestEndpointLabel(row)}
          secondary={requestEndpointContext(row) || undefined}
          secondaryTitle={requestEndpointContext(row) || undefined}
        />
      ),
    },
    {
      key: 'subject',
      title: t('monitor.subject'),
      width: MONITOR_COLUMN_WIDTHS.subject,
      render: (row) => (
        <StackCell
          primary={monitorRequestSubject(row)}
          primaryClass="font-medium text-text"
          primaryTitle={monitorRequestSubject(row)}
          secondary={monitorRequestSubjectContext(row) || undefined}
          secondaryTitle={monitorRequestSubjectContext(row) || undefined}
        />
      ),
    },
    {
      key: 'http_status',
      title: t('monitor.error_code'),
      width: MONITOR_COLUMN_WIDTHS.status,
      render: (row) => (
        <div className="flex h-full w-full min-w-0 flex-col items-center justify-center gap-1">
          <span className={`max-w-full truncate font-mono text-[13px] font-medium leading-none ${requestStatusToneClass(row)}`} title={requestStatusLabel(row)}>
            {requestStatusLabel(row)}
          </span>
          <span className="max-w-full truncate font-mono text-[11px] leading-none text-text-tertiary" title={requestErrorCodeLabel(row)}>
            {requestErrorCodeLabel(row)}
          </span>
        </div>
      ),
    },
    {
      key: 'detail',
      title: t('monitor.detail'),
      width: MONITOR_COLUMN_WIDTHS.detail,
      hideOnMobile: true,
      render: (row) => <DetailCell entries={requestDetailEntries(row)} />,
    },
  ], [t]);

  const rows = data?.list ?? [];
  const hasRows = rows.length > 0;
  const showInitialLoading = isLoading && !data;
  const total = totalForCursorPage(page, pageSize, rows.length, data?.has_more);
  const totalPages = getTotalPages(total, pageSize);
  const requestRows = requestData?.list ?? [];
  const hasRequestRows = requestRows.length > 0;
  const showRequestInitialLoading = isRequestLoading && !requestData;
  const requestTotal = totalForCursorPage(requestPage, requestPageSize, requestRows.length, requestData?.has_more);
  const requestTotalPages = getTotalPages(requestTotal, requestPageSize);
  const isRequestTable = activeTable === 'requests';
  const tableOptions = [
    { id: 'events', label: t('monitor.title') },
    { id: 'requests', label: t('monitor.request_events') },
  ];
  const activeTableLabel = tableOptions.find((item) => item.id === activeTable)?.label ?? t('monitor.title');
  const activeColumns = (isRequestTable ? requestColumns : columns) as MonitorTableColumnConfig[];
  const activeRows = (isRequestTable ? requestRows : rows) as MonitorTableRow[];
  const activePage = isRequestTable ? requestPage : page;
  const activePageSize = isRequestTable ? requestPageSize : pageSize;
  const activeSetPage = isRequestTable ? setRequestPage : setPage;
  const activeSetPageSize = isRequestTable ? setRequestPageSize : setPageSize;
  const activeTotal = isRequestTable ? requestTotal : total;
  const activeHasMore = isRequestTable ? requestData?.has_more : data?.has_more;
  const activeTotalPages = isRequestTable ? requestTotalPages : totalPages;
  const activeHasRows = isRequestTable ? hasRequestRows : hasRows;
  const activeDataVersion = isRequestTable ? requestDataUpdatedAt : dataUpdatedAt;
  const activeIsLoading = isRequestTable ? showRequestInitialLoading : showInitialLoading;
  const activeIsPlaceholderData = isRequestTable ? isRequestPlaceholderData : isPlaceholderData;
  const activeHighlightResetKey = isRequestTable
    ? JSON.stringify({ requestFilters, requestPage, requestPageSize })
    : JSON.stringify({ filters, page, pageSize });
  const activeIsTableFetching = isRequestTable
    ? hasRequestRows && isRequestPlaceholderData && isRequestFetching && !showRequestInitialLoading
    : hasRows && isPlaceholderData && isFetching && !showInitialLoading;
  const activeRefreshBusy = isRequestTable ? isRequestFetching : isFetching || summaryQuery.isFetching;

  return (
    <div>
      <MonitorStats summary={summaryQuery.data} />
      <TablePage
        className="ag-monitor-page ag-toolbar-standard-page"
        footer={(
          <TablePaginationFooter
            page={activePage}
            pageSize={activePageSize}
            pageSizeOptions={PAGE_SIZE_OPTIONS}
            setPage={activeSetPage}
            setPageSize={activeSetPageSize}
            total={activeTotal}
            hasMore={activeHasMore}
            totalExact={false}
            totalPages={activeTotalPages}
          />
        )}
        isFetching={activeIsTableFetching}
      >
        <div className="ag-page-toolbar">
          <div className="ag-page-toolbar-filters">
            <div className="ag-page-toolbar-filter-row">
              <FilterSelect
                ariaLabel={t('monitor.title')}
                className="w-full sm:w-48"
                options={tableOptions}
                value={activeTable}
                onChange={(value) => setActiveTable(value as MonitorTableKey)}
              />
              {isRequestTable ? (
                <>
                  <div className="w-full sm:w-40">
                    <input
                      className="input input--sm w-full"
                      placeholder={t('monitor.error_code')}
                      value={requestFilters.error_code ?? ''}
                      onChange={(event) => updateRequestFilter('error_code', event.target.value)}
                    />
                  </div>
                  <FilterSelect
                    ariaLabel={t('monitor.http_status_code')}
                    className="w-full sm:w-40"
                    label={t('monitor.http_status_code')}
                    options={httpStatusOptions}
                    value={requestFilters.http_status ? String(requestFilters.http_status) : ''}
                    onChange={(value) => updateRequestFilter('http_status', value ? Number(value) : undefined)}
                  />
                  <div className="w-full sm:w-48">
                    <APIKeySearchFilterComboBox
                      ariaLabel={t('monitor.search_api_key')}
                      emptyPrompt={t('monitor.search_api_key')}
                      loadingLabel={t('common.loading')}
                      noDataLabel={t('common.no_data')}
                      placeholder={t('monitor.search_api_key')}
                      scope="admin"
                      selectedKey={requestFilters.api_key_id ? String(requestFilters.api_key_id) : null}
                      selectedLabel={selectedAPIKeyLabel}
                      onSelectionChange={handleAPIKeySelectionChange}
                    />
                  </div>
                </>
              ) : (
                <>
                  <div className="w-full sm:w-72">
                    <UsageDateRangeFilter
                      clearLabel={t('common.clear')}
                      endDate={filters.to}
                      label={t('monitor.time_range')}
                      startDate={filters.from}
                      onChange={(from, to) => {
                        resetPagination();
                        setFilters((prev) => ({ ...prev, from, to }));
                      }}
                    />
                  </div>
                  <FilterSelect
                    ariaLabel={t('monitor.status')}
                    className="w-full sm:w-44"
                    label={t('monitor.status')}
                    options={statusOptions}
                    value={filters.status || ''}
                    onChange={(value) => updateFilter('status', value)}
                  />
                  <FilterSelect
                    ariaLabel={t('monitor.severity')}
                    className="w-full sm:w-44"
                    label={t('monitor.severity')}
                    options={severityOptions}
                    value={filters.severity || ''}
                    onChange={(value) => updateFilter('severity', value)}
                  />
                  <FilterSelect
                    ariaLabel={t('monitor.type')}
                    className="w-full sm:w-56"
                    label={t('monitor.type')}
                    options={typeOptions}
                    value={filters.type || ''}
                    onChange={(value) => updateFilter('type', value)}
                  />
                  <div className="w-full sm:w-40">
                    <input
                      className="input input--sm w-full"
                      placeholder={t('monitor.source')}
                      value={filters.source ?? ''}
                      onChange={(event) => updateFilter('source', event.target.value)}
                    />
                  </div>
                </>
              )}
            </div>
          </div>
          <div className="ag-page-toolbar-actions">
            <Button
              isIconOnly
              aria-label={t('common.refresh', 'Refresh')}
              className="ag-auto-refresh-refresh--toolbar h-8 w-8 min-w-8"
              isDisabled={activeRefreshBusy}
              size="sm"
              variant="ghost"
              onPress={() => {
                if (isRequestTable) {
                  void refetchRequests({ cancelRefetch: false });
                  return;
                }
                handleManualRefresh();
              }}
            >
              <RefreshCw className={`h-4 w-4 ${activeRefreshBusy ? 'animate-spin' : ''}`} />
            </Button>
            {isRequestTable ? (
              <Button
                isIconOnly
                aria-label={t('monitor.clear_request_events')}
                className="h-8 w-8 min-w-8"
                isDisabled={clearRequestsMutation.isPending || isRequestFetching}
                size="sm"
                variant="ghost"
                onPress={() => clearRequestsMutation.mutate()}
              >
                <Trash2 className="h-4 w-4" />
              </Button>
            ) : null}
          </div>
        </div>

        <RecordsTable
          ariaLabel={activeTableLabel}
          columns={activeColumns}
          dataVersion={activeHasRows ? activeDataVersion : 0}
          emptyDescription={t('monitor.empty_description')}
          emptyTitle={t('common.no_data')}
          footer={false}
          highlightNewRows={activeHasRows && activePage === 1}
          highlightResetKey={activeHighlightResetKey}
          hasMore={activeHasMore}
          isLoading={activeIsLoading}
          page={activePage}
          pageSize={activePageSize}
          rows={activeRows}
          setPage={activeSetPage}
          setPageSize={activeSetPageSize}
          suppressHighlight={activeIsPlaceholderData}
          total={activeTotal}
          totalExact={false}
        />
      </TablePage>
    </div>
  );
}
