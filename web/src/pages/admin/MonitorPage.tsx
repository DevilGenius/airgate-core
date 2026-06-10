import { startTransition, useCallback, useEffect, useMemo, useState, type ReactNode } from 'react';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Button, Card } from '@heroui/react';
import { AlertTriangle, CheckCircle2, EyeOff, RefreshCw, ShieldAlert, Trash2, TriangleAlert } from 'lucide-react';
import { monitorApi } from '../../shared/api/monitor';
import { subscribeAdminEvents } from '../../shared/api/adminEvents';
import { queryKeys } from '../../shared/queryKeys';
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

const DEFAULT_PAGE_SIZE = 20;

const SEVERITY_CLASSES: Record<string, string> = {
  info: 'bg-sky-100 text-sky-700 ring-sky-200 dark:bg-sky-400/15 dark:text-sky-300 dark:ring-sky-400/25',
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

function monitorSubject(event: MonitorEventResp): string {
  return event.account_name_snapshot
    || event.plugin_id
    || event.subject_id
    || '-';
}

function monitorLocator(event: MonitorEventResp): string {
  return event.error_code || event.source || '-';
}

function monitorRequestSubject(event: MonitorRequestEventResp): string {
  return event.api_key_name_snapshot
    || event.account_name_snapshot
    || event.plugin_id
    || event.request_id
    || '-';
}

function monitorRequestLocator(event: MonitorRequestEventResp): string {
  const endpoint = [event.method, event.endpoint].filter(Boolean).join(' ');
  return [endpoint, event.error_code].filter(Boolean).join(' / ') || event.source || '-';
}

function requestStatusLabel(event: MonitorRequestEventResp): string {
  const status = event.http_status ? String(event.http_status) : '-';
  if (!event.upstream_status || event.upstream_status === event.http_status) {
    return status;
  }
  return `${status} / ${event.upstream_status}`;
}

function durationLabel(value?: number): string {
  if (!value || value <= 0) {
    return '-';
  }
  return `${value}ms`;
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
  const [page, setPageState] = useState(1);
  const [pageSize, setPageSizeState] = useState(DEFAULT_PAGE_SIZE);
  const [cursors, setCursors] = useState<Record<number, MonitorCursorResp | undefined>>({});
  const [filters, setFilters] = useState<Partial<MonitorListQuery>>({});
  const cursor = page > 1 ? cursors[page] : undefined;
  const [requestPage, setRequestPageState] = useState(1);
  const [requestPageSize, setRequestPageSizeState] = useState(DEFAULT_PAGE_SIZE);
  const [requestCursors, setRequestCursors] = useState<Record<number, MonitorRequestCursorResp | undefined>>({});
  const requestCursor = requestPage > 1 ? requestCursors[requestPage] : undefined;

  const listParams = useMemo<MonitorListQuery>(() => ({
    ...filters,
    cursor: cursor?.updated_at,
    cursor_id: cursor?.id,
    limit: pageSize,
  }), [cursor?.id, cursor?.updated_at, filters, pageSize]);
  const requestListParams = useMemo<MonitorRequestListQuery>(() => ({
    cursor: requestCursor?.created_at,
    cursor_id: requestCursor?.id,
    limit: requestPageSize,
  }), [requestCursor?.created_at, requestCursor?.id, requestPageSize]);

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
    const refreshFromEvent = () => {
      timeoutId = undefined;
      void refetch({ cancelRefetch: false });
      void refetchSummary({ cancelRefetch: false });
      void refetchRequests({ cancelRefetch: false });
    };

    const unsubscribe = subscribeAdminEvents((event) => {
      if (event.type !== 'monitor.changed' || timeoutId !== undefined) return;
      timeoutId = window.setTimeout(refreshFromEvent, 250);
    });

    return () => {
      unsubscribe();
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

  const selectedStatusValue = statusOptions.find((item) => item.id === (filters.status || ''))?.label ?? t('common.all');
  const selectedSeverityValue = severityOptions.find((item) => item.id === (filters.severity || ''))?.label ?? t('common.all');
  const selectedTypeValue = typeOptions.find((item) => item.id === (filters.type || ''))?.label ?? t('common.all');
  const selectedStatusLabel = `${t('monitor.status')}: ${selectedStatusValue}`;
  const selectedSeverityLabel = `${t('monitor.severity')}: ${selectedSeverityValue}`;
  const selectedTypeLabel = `${t('monitor.type')}: ${selectedTypeValue}`;

  const columns = useMemo<MonitorColumnConfig[]>(() => [
    {
      key: 'updated_at',
      title: t('monitor.updated_at'),
      width: '142px',
      render: (row) => {
        const { dateLabel, fullLabel, timeLabel } = monitorTimeLabels(row.updated_at);
        return (
          <div className="flex min-w-0 items-center gap-1.5 font-mono text-xs" title={fullLabel}>
            <span className="shrink-0 font-mono text-[13px] font-medium text-text">
              {timeLabel}
            </span>
            {dateLabel ? (
              <span className="hidden shrink-0 font-light text-text-tertiary xl:inline">
                {dateLabel}
              </span>
            ) : null}
          </div>
        );
      },
    },
    {
      key: 'type',
      title: t('monitor.type'),
      width: '168px',
      hideOnMobile: true,
      render: (row) => <span className="block w-full truncate text-center text-[13px] leading-none text-text-secondary" title={row.type}>{typeLabel(t, row.type)}</span>,
    },
    {
      key: 'severity',
      title: t('monitor.severity'),
      width: '112px',
      render: (row) => (
        <span className={`inline-flex h-6 min-w-[4.75rem] max-w-full items-center justify-center truncate rounded-[var(--radius)] px-1.5 text-[13px] font-medium leading-none ring-1 ${SEVERITY_CLASSES[row.severity] ?? SEVERITY_CLASSES.warning}`}>
          {severityLabel(t, row.severity)}
        </span>
      ),
    },
    {
      key: 'source',
      title: t('monitor.source'),
      width: '144px',
      hideOnMobile: true,
      render: (row) => (
        <div className="flex h-full w-full min-w-0 flex-col justify-center gap-1 text-left">
          <span className="truncate font-mono text-[13px] leading-none text-text-secondary" title={row.source}>{row.source}</span>
          <span className="truncate text-[11px] leading-none text-text-tertiary" title={row.platform || '-'}>
            {row.platform || '-'}
          </span>
        </div>
      ),
    },
    {
      key: 'event',
      title: t('monitor.event'),
      width: '300px',
      render: (row) => (
        <div className="flex h-full w-full min-w-0 flex-col justify-center gap-1 text-left">
          <span className="truncate text-[13px] font-medium leading-none text-text" title={row.title}>{row.title}</span>
          <span className="truncate text-[11px] leading-none text-text-tertiary" title={row.message || row.type}>
            {row.message || typeLabel(t, row.type)}
          </span>
        </div>
      ),
    },
    {
      key: 'subject',
      title: t('monitor.subject'),
      width: '190px',
      render: (row) => (
        <div className="flex h-full w-full min-w-0 flex-col justify-center gap-1 text-left">
          <span className="truncate text-[13px] font-medium leading-none text-text" title={monitorSubject(row)}>{monitorSubject(row)}</span>
          <span className="truncate font-mono text-[11px] leading-none text-text-tertiary">{row.subject_type || '-'}</span>
        </div>
      ),
    },
    {
      key: 'locator',
      title: t('monitor.locator'),
      width: '240px',
      hideOnMobile: true,
      render: (row) => (
        <div className="flex h-full w-full min-w-0 flex-col justify-center gap-1 text-left">
          <span className="truncate font-mono text-[13px] leading-none text-text-secondary" title={monitorLocator(row)}>{monitorLocator(row)}</span>
          <span className="truncate text-[11px] leading-none text-text-tertiary" title={row.plugin_id || '-'}>
            {row.plugin_id || '-'}
          </span>
        </div>
      ),
    },
    {
      key: 'status',
      title: t('monitor.status'),
      width: '104px',
      render: (row) => (
        <span className={`inline-flex h-6 min-w-[4.75rem] max-w-full items-center justify-center truncate rounded-[var(--radius)] px-1.5 text-[13px] font-medium leading-none ring-1 ${STATUS_CLASSES[row.status] ?? STATUS_CLASSES.active}`}>
          {statusLabel(t, row.status)}
        </span>
      ),
    },
    {
      key: 'actions',
      title: t('common.actions'),
      width: '112px',
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
      width: '142px',
      render: (row) => {
        const { dateLabel, fullLabel, timeLabel } = monitorTimeLabels(row.created_at);
        return (
          <div className="flex min-w-0 items-center gap-1.5 font-mono text-xs" title={fullLabel}>
            <span className="shrink-0 font-mono text-[13px] font-medium text-text">{timeLabel}</span>
            {dateLabel ? <span className="hidden shrink-0 font-light text-text-tertiary xl:inline">{dateLabel}</span> : null}
          </div>
        );
      },
    },
    {
      key: 'type',
      title: t('monitor.type'),
      width: '168px',
      hideOnMobile: true,
      render: (row) => <span className="block w-full truncate text-center text-[13px] leading-none text-text-secondary" title={row.type}>{typeLabel(t, row.type)}</span>,
    },
    {
      key: 'severity',
      title: t('monitor.severity'),
      width: '112px',
      render: (row) => (
        <span className={`inline-flex h-6 min-w-[4.75rem] max-w-full items-center justify-center truncate rounded-[var(--radius)] px-1.5 text-[13px] font-medium leading-none ring-1 ${SEVERITY_CLASSES[row.severity] ?? SEVERITY_CLASSES.warning}`}>
          {severityLabel(t, row.severity)}
        </span>
      ),
    },
    {
      key: 'source',
      title: t('monitor.source'),
      width: '144px',
      hideOnMobile: true,
      render: (row) => (
        <div className="flex h-full w-full min-w-0 flex-col justify-center gap-1 text-left">
          <span className="truncate font-mono text-[13px] leading-none text-text-secondary" title={row.source}>{row.source}</span>
          <span className="truncate text-[11px] leading-none text-text-tertiary" title={row.platform || '-'}>{row.platform || '-'}</span>
        </div>
      ),
    },
    {
      key: 'event',
      title: t('monitor.event'),
      width: '300px',
      render: (row) => (
        <div className="flex h-full w-full min-w-0 flex-col justify-center gap-1 text-left">
          <span className="truncate text-[13px] font-medium leading-none text-text" title={row.title}>{row.title}</span>
          <span className="truncate text-[11px] leading-none text-text-tertiary" title={row.message || row.type}>{row.message || typeLabel(t, row.type)}</span>
        </div>
      ),
    },
    {
      key: 'subject',
      title: t('monitor.subject'),
      width: '190px',
      render: (row) => (
        <div className="flex h-full w-full min-w-0 flex-col justify-center gap-1 text-left">
          <span className="truncate text-[13px] font-medium leading-none text-text" title={monitorRequestSubject(row)}>{monitorRequestSubject(row)}</span>
          <span className="truncate font-mono text-[11px] leading-none text-text-tertiary">{row.api_key_id ? `api_key:${row.api_key_id}` : row.request_id || '-'}</span>
        </div>
      ),
    },
    {
      key: 'locator',
      title: t('monitor.locator'),
      width: '260px',
      hideOnMobile: true,
      render: (row) => (
        <div className="flex h-full w-full min-w-0 flex-col justify-center gap-1 text-left">
          <span className="truncate font-mono text-[13px] leading-none text-text-secondary" title={monitorRequestLocator(row)}>{monitorRequestLocator(row)}</span>
          <span className="truncate text-[11px] leading-none text-text-tertiary" title={row.model || row.plugin_id || '-'}>
            {row.model || row.plugin_id || '-'}
          </span>
        </div>
      ),
    },
    {
      key: 'http_status',
      title: t('monitor.http_status'),
      width: '104px',
      render: (row) => <span className="block w-full truncate text-center font-mono text-[13px] leading-none text-text-secondary">{requestStatusLabel(row)}</span>,
    },
    {
      key: 'duration',
      title: t('monitor.duration_ms'),
      width: '104px',
      hideOnMobile: true,
      render: (row) => <span className="block w-full truncate text-center font-mono text-[13px] leading-none text-text-secondary">{durationLabel(row.duration_ms)}</span>,
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

  return (
    <div>
      <MonitorStats summary={summaryQuery.data} />
      <TablePage
        className="ag-monitor-page ag-toolbar-standard-page"
        footer={(
          <TablePaginationFooter
            page={page}
            pageSize={pageSize}
            pageSizeOptions={PAGE_SIZE_OPTIONS}
            setPage={setPage}
            setPageSize={setPageSize}
            total={total}
            hasMore={data?.has_more}
            totalExact={false}
            totalPages={totalPages}
          />
        )}
        isFetching={hasRows && isPlaceholderData && isFetching && !showInitialLoading}
      >
        <div className="ag-page-toolbar">
          <div className="ag-page-toolbar-filters">
            <div className="ag-page-toolbar-filter-row">
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
              <div className="w-full sm:w-44">
                <SimpleSelect
                  ariaLabel={t('monitor.status')}
                  fullWidth
                  items={statusOptions.map((item) => ({ key: item.id, label: item.label }))}
                  selectedKey={filters.status || ''}
                  selectedLabel={selectedStatusLabel}
                  onSelectionChange={(key) => updateFilter('status', key)}
                />
              </div>
              <div className="w-full sm:w-44">
                <SimpleSelect
                  ariaLabel={t('monitor.severity')}
                  fullWidth
                  items={severityOptions.map((item) => ({ key: item.id, label: item.label }))}
                  selectedKey={filters.severity || ''}
                  selectedLabel={selectedSeverityLabel}
                  onSelectionChange={(key) => updateFilter('severity', key)}
                />
              </div>
              <div className="w-full sm:w-56">
                <SimpleSelect
                  ariaLabel={t('monitor.type')}
                  fullWidth
                  items={typeOptions.map((item) => ({ key: item.id, label: item.label }))}
                  selectedKey={filters.type || ''}
                  selectedLabel={selectedTypeLabel}
                  onSelectionChange={(key) => updateFilter('type', key)}
                />
              </div>
              <div className="w-full sm:w-40">
                <input
                  className="input input--sm w-full"
                  placeholder={t('monitor.error_code')}
                  value={filters.error_code ?? ''}
                  onChange={(event) => updateFilter('error_code', event.target.value)}
                />
              </div>
            </div>
          </div>
          <div className="ag-page-toolbar-actions">
            <Button
              isIconOnly
              aria-label={t('common.refresh', 'Refresh')}
              className="ag-auto-refresh-refresh--toolbar h-8 w-8 min-w-8"
              isDisabled={isFetching || summaryQuery.isFetching}
              size="sm"
              variant="ghost"
              onPress={handleManualRefresh}
            >
              <RefreshCw className={`h-4 w-4 ${isFetching || summaryQuery.isFetching ? 'animate-spin' : ''}`} />
            </Button>
          </div>
        </div>

        <RecordsTable
          ariaLabel={t('monitor.title')}
          columns={columns}
          dataVersion={hasRows ? dataUpdatedAt : 0}
          emptyDescription={t('monitor.empty_description')}
          emptyTitle={t('common.no_data')}
          footer={false}
          highlightNewRows={hasRows && page === 1}
          highlightResetKey={JSON.stringify({ filters, page, pageSize })}
          hasMore={data?.has_more}
          isLoading={showInitialLoading}
          page={page}
          pageSize={pageSize}
          rows={rows}
          setPage={setPage}
          setPageSize={setPageSize}
          suppressHighlight={isPlaceholderData}
          total={total}
          totalExact={false}
        />
      </TablePage>
      <TablePage
        className="ag-monitor-page ag-toolbar-standard-page mt-6"
        footer={(
          <TablePaginationFooter
            page={requestPage}
            pageSize={requestPageSize}
            pageSizeOptions={PAGE_SIZE_OPTIONS}
            setPage={setRequestPage}
            setPageSize={setRequestPageSize}
            total={requestTotal}
            hasMore={requestData?.has_more}
            totalExact={false}
            totalPages={requestTotalPages}
          />
        )}
        isFetching={hasRequestRows && isRequestPlaceholderData && isRequestFetching && !showRequestInitialLoading}
      >
        <div className="ag-page-toolbar">
          <div className="ag-page-toolbar-filters">
            <div className="ag-page-toolbar-filter-row">
              <div className="flex min-h-8 items-center">
                <span className="text-sm font-semibold text-text">{t('monitor.request_events')}</span>
              </div>
            </div>
          </div>
          <div className="ag-page-toolbar-actions">
            <Button
              isIconOnly
              aria-label={t('common.refresh', 'Refresh')}
              className="ag-auto-refresh-refresh--toolbar h-8 w-8 min-w-8"
              isDisabled={isRequestFetching}
              size="sm"
              variant="ghost"
              onPress={() => void refetchRequests({ cancelRefetch: false })}
            >
              <RefreshCw className={`h-4 w-4 ${isRequestFetching ? 'animate-spin' : ''}`} />
            </Button>
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
          </div>
        </div>

        <RecordsTable
          ariaLabel={t('monitor.request_events')}
          columns={requestColumns}
          dataVersion={hasRequestRows ? requestDataUpdatedAt : 0}
          emptyDescription={t('monitor.empty_description')}
          emptyTitle={t('common.no_data')}
          footer={false}
          highlightNewRows={hasRequestRows && requestPage === 1}
          highlightResetKey={JSON.stringify({ requestPage, requestPageSize })}
          hasMore={requestData?.has_more}
          isLoading={showRequestInitialLoading}
          page={requestPage}
          pageSize={requestPageSize}
          rows={requestRows}
          setPage={setRequestPage}
          setPageSize={setRequestPageSize}
          suppressHighlight={isRequestPlaceholderData}
          total={requestTotal}
          totalExact={false}
        />
      </TablePage>
    </div>
  );
}
