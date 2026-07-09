import { startTransition, useCallback, useEffect, useMemo, useState } from 'react';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Button } from '@heroui/react';
import { Trash2 } from 'lucide-react';
import { accountsApi } from '../../shared/api/accounts';
import { groupsApi } from '../../shared/api/groups';
import { monitorApi } from '../../shared/api/monitor';
import { subscribeAdminEvents } from '../../shared/api/adminEvents';
import { queryKeys } from '../../shared/queryKeys';
import { APIKeySearchFilterComboBox } from '../../shared/components/APIKeySearchFilterComboBox';
import { AutoRefreshControl } from '../../shared/components/AutoRefreshControl';
import { RecordsTable } from '../../shared/components/RecordsTable';
import { RemoteSearchFilterComboBox } from '../../shared/components/RemoteSearchFilterComboBox';
import { TablePage } from '../../shared/components/TablePage';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { PAGE_SIZE_OPTIONS } from '../../shared/constants';
import { usePersistentAutoRefresh } from '../../shared/hooks/usePersistentAutoRefresh';
import { usePagination } from '../../shared/hooks/usePagination';
import { STORAGE_KEYS } from '../../shared/storageKeys';
import { getTotalPages } from '../../shared/utils/pagination';
import { useToast } from '../../shared/ui';
import { DEFAULT_PAGE_SIZE, MONITOR_TIME_RANGE_PRESETS } from './monitor/constants';
import { MonitorCustomTimeRangeModal } from './monitor/MonitorCustomTimeRangeModal';
import { MonitorFilterSelect as FilterSelect } from './monitor/MonitorFilterSelect';
import { MonitorRuntimeStats } from './monitor/MonitorRuntimeStats';
import { totalForCursorPage } from './monitor/pagination';
import { monitorRangeLabel, presetTimeRange } from './monitor/timeRange';
import { useMonitorColumns, useMonitorRequestColumns } from './monitor/useMonitorColumns';
import type {
  AccountResp,
  GroupResp,
  MonitorCursorResp,
  MonitorListQuery,
  MonitorRequestCursorResp,
  MonitorRequestListQuery,
} from '../../shared/types';
import type {
  MonitorTableColumnConfig,
  MonitorTableKey,
  MonitorTableRow,
  MonitorTimeRangePreset,
  SelectOption,
} from './monitor/types';

const MONITOR_TOOLBAR_CONTROL_CLASS = 'ag-monitor-toolbar-control';
const MONITOR_EVENTS_PAGE_SIZE_SCOPE = 'admin.monitor.events';
const MONITOR_REQUESTS_PAGE_SIZE_SCOPE = 'admin.monitor.requests';
const MONITOR_FILTER_STORAGE_KEY = STORAGE_KEYS.ui.adminMonitorFilters;
const MONITOR_AUTO_REFRESH_STORAGE_KEY = STORAGE_KEYS.ui.adminMonitorAutoRefresh;
const MONITOR_AUTO_REFRESH_OPTIONS = [0, 5, 15, 30, 60] as const;
const MONITOR_REQUEST_TYPE_IDS = [
  'api_request_error',
  'plugin_route_error',
  'plugin_forward_error',
  'client_request_error',
  'client_closed_request',
];
const MONITOR_EVENT_STATUS_IDS: readonly string[] = ['active', 'resolved'];
const MONITOR_EVENT_SEVERITY_IDS: readonly string[] = ['critical', 'error', 'warning', 'info'];
const MONITOR_EVENT_TYPE_IDS: readonly string[] = [
  'scheduler_error',
  'upstream_account_error',
  'plugin_error',
  'task_error',
  'system_error',
];
const MONITOR_HTTP_STATUS_CODES: readonly string[] = ['400', '401', '403', '404', '408', '429', '499', '500', '502', '503', '504'];

const MONITOR_FILTER_KEYS = {
  activeTable: `${MONITOR_FILTER_STORAGE_KEY}:active_table`,
  events: {
    from: `${MONITOR_FILTER_STORAGE_KEY}:events:from`,
    severity: `${MONITOR_FILTER_STORAGE_KEY}:events:severity`,
    status: `${MONITOR_FILTER_STORAGE_KEY}:events:status`,
    timeRange: `${MONITOR_FILTER_STORAGE_KEY}:events:time_range`,
    to: `${MONITOR_FILTER_STORAGE_KEY}:events:to`,
    type: `${MONITOR_FILTER_STORAGE_KEY}:events:type`,
  },
  requests: {
    accountID: `${MONITOR_FILTER_STORAGE_KEY}:requests:account_id`,
    accountLabel: `${MONITOR_FILTER_STORAGE_KEY}:requests:account_label`,
    apiKeyID: `${MONITOR_FILTER_STORAGE_KEY}:requests:api_key_id`,
    apiKeyLabel: `${MONITOR_FILTER_STORAGE_KEY}:requests:api_key_label`,
    from: `${MONITOR_FILTER_STORAGE_KEY}:requests:from`,
    groupID: `${MONITOR_FILTER_STORAGE_KEY}:requests:group_id`,
    groupLabel: `${MONITOR_FILTER_STORAGE_KEY}:requests:group_label`,
    httpStatus: `${MONITOR_FILTER_STORAGE_KEY}:requests:http_status`,
    timeRange: `${MONITOR_FILTER_STORAGE_KEY}:requests:time_range`,
    to: `${MONITOR_FILTER_STORAGE_KEY}:requests:to`,
    type: `${MONITOR_FILTER_STORAGE_KEY}:requests:type`,
  },
} as const;

const MONITOR_EVENT_FILTER_STORAGE_KEYS: Partial<Record<keyof MonitorListQuery, string>> = {
  severity: MONITOR_FILTER_KEYS.events.severity,
  status: MONITOR_FILTER_KEYS.events.status,
  type: MONITOR_FILTER_KEYS.events.type,
};

const MONITOR_REQUEST_FILTER_STORAGE_KEYS: Partial<Record<keyof MonitorRequestListQuery, string>> = {
  account_id: MONITOR_FILTER_KEYS.requests.accountID,
  api_key_id: MONITOR_FILTER_KEYS.requests.apiKeyID,
  group_id: MONITOR_FILTER_KEYS.requests.groupID,
  http_status: MONITOR_FILTER_KEYS.requests.httpStatus,
  type: MONITOR_FILTER_KEYS.requests.type,
};

function readStoredString(key: string) {
  if (typeof window === 'undefined') return '';
  try {
    return window.localStorage.getItem(key) ?? '';
  } catch {
    return '';
  }
}

function writeStoredString(key: string | undefined, value: string | number | undefined) {
  if (!key || typeof window === 'undefined') return;
  try {
    const text = value == null ? '' : String(value);
    if (text) {
      window.localStorage.setItem(key, text);
    } else {
      window.localStorage.removeItem(key);
    }
  } catch {
    // localStorage may be unavailable; filters should still work for this session.
  }
}

function readStoredPositiveNumber(key: string) {
  const value = Number(readStoredString(key));
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : undefined;
}

function readStoredOption(key: string, allowedValues: readonly string[]) {
  const value = readStoredString(key);
  return allowedValues.includes(value) ? value : undefined;
}

function readStoredHTTPStatus(key: string) {
  const value = readStoredString(key);
  return MONITOR_HTTP_STATUS_CODES.includes(value) ? Number(value) : undefined;
}

function storedIDLabel(id: number | undefined, label: string) {
  return id != null ? label || `#${id}` : '';
}

function normalizeStoredMonitorTable(value: string): MonitorTableKey {
  return value === 'requests' ? 'requests' : 'events';
}

function normalizeStoredTimeRangePreset(value: string): MonitorTimeRangePreset {
  return MONITOR_TIME_RANGE_PRESETS.some((item) => item.id === value)
    ? value as MonitorTimeRangePreset
    : 'all';
}

function readStoredTimeRange(keys: { from: string; timeRange: string; to: string }) {
  const preset = normalizeStoredTimeRangePreset(readStoredString(keys.timeRange));
  if (preset === 'custom') {
    const from = readStoredString(keys.from) || undefined;
    const to = readStoredString(keys.to) || undefined;
    if (!from && !to) {
      return { ...presetTimeRange('all'), preset: 'all' as MonitorTimeRangePreset };
    }
    return {
      from,
      preset,
      to,
    };
  }
  return { ...presetTimeRange(preset), preset };
}

function writeStoredTimeRange(keys: { from: string; timeRange: string; to: string }, preset: MonitorTimeRangePreset, from?: string, to?: string) {
  writeStoredString(keys.timeRange, preset === 'all' ? '' : preset);
  writeStoredString(keys.from, preset === 'custom' ? from : undefined);
  writeStoredString(keys.to, preset === 'custom' ? to : undefined);
}

function readInitialMonitorState() {
  const eventsTimeRange = readStoredTimeRange(MONITOR_FILTER_KEYS.events);
  const requestsTimeRange = readStoredTimeRange(MONITOR_FILTER_KEYS.requests);
  const apiKeyID = readStoredPositiveNumber(MONITOR_FILTER_KEYS.requests.apiKeyID);
  const accountID = readStoredPositiveNumber(MONITOR_FILTER_KEYS.requests.accountID);
  const groupID = readStoredPositiveNumber(MONITOR_FILTER_KEYS.requests.groupID);
  const filters: Partial<MonitorListQuery> = {
    from: eventsTimeRange.from,
    severity: readStoredOption(MONITOR_FILTER_KEYS.events.severity, MONITOR_EVENT_SEVERITY_IDS),
    status: readStoredOption(MONITOR_FILTER_KEYS.events.status, MONITOR_EVENT_STATUS_IDS),
    to: eventsTimeRange.to,
    type: readStoredOption(MONITOR_FILTER_KEYS.events.type, MONITOR_EVENT_TYPE_IDS),
  };
  const requestFilters: Partial<MonitorRequestListQuery> = {
    account_id: accountID,
    api_key_id: apiKeyID,
    from: requestsTimeRange.from,
    group_id: groupID,
    http_status: readStoredHTTPStatus(MONITOR_FILTER_KEYS.requests.httpStatus),
    to: requestsTimeRange.to,
    type: readStoredOption(MONITOR_FILTER_KEYS.requests.type, MONITOR_REQUEST_TYPE_IDS),
  };

  return {
    activeTable: normalizeStoredMonitorTable(readStoredString(MONITOR_FILTER_KEYS.activeTable)),
    filters,
    requestFilters,
    requestTimeRangePreset: requestsTimeRange.preset,
    selectedAPIKeyLabel: storedIDLabel(apiKeyID, readStoredString(MONITOR_FILTER_KEYS.requests.apiKeyLabel)),
    selectedAccountLabel: storedIDLabel(accountID, readStoredString(MONITOR_FILTER_KEYS.requests.accountLabel)),
    selectedGroupLabel: storedIDLabel(groupID, readStoredString(MONITOR_FILTER_KEYS.requests.groupLabel)),
    timeRangePreset: eventsTimeRange.preset,
  };
}

export default function MonitorPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const queryClient = useQueryClient();
  const [autoRefresh, setAutoRefresh] = usePersistentAutoRefresh(MONITOR_AUTO_REFRESH_STORAGE_KEY, 15, MONITOR_AUTO_REFRESH_OPTIONS);
  const autoRefreshEnabled = autoRefresh > 0;
  const autoRefreshLabel = `${t('monitor.auto_update')} `;
  const autoRefreshOffLabel = t('monitor.auto_update_off');
  const [initialMonitorState] = useState(readInitialMonitorState);
  const [activeTable, setActiveTableState] = useState<MonitorTableKey>(initialMonitorState.activeTable);
  const {
    page,
    pageSize,
    setPage: setMonitorPageState,
    setPageSize: setMonitorPageSizeState,
  } = usePagination(DEFAULT_PAGE_SIZE, MONITOR_EVENTS_PAGE_SIZE_SCOPE);
  const [cursors, setCursors] = useState<Record<number, MonitorCursorResp | undefined>>({});
  const [filters, setFilters] = useState<Partial<MonitorListQuery>>(initialMonitorState.filters);
  const cursor = page > 1 ? cursors[page] : undefined;
  const {
    page: requestPage,
    pageSize: requestPageSize,
    setPage: setRequestPageState,
    setPageSize: setRequestPageSizeState,
  } = usePagination(DEFAULT_PAGE_SIZE, MONITOR_REQUESTS_PAGE_SIZE_SCOPE);
  const [requestCursors, setRequestCursors] = useState<Record<number, MonitorRequestCursorResp | undefined>>({});
  const requestCursor = requestPage > 1 ? requestCursors[requestPage] : undefined;
  const [requestFilters, setRequestFilters] = useState<Partial<MonitorRequestListQuery>>(initialMonitorState.requestFilters);
  const [selectedAPIKeyLabel, setSelectedAPIKeyLabel] = useState(initialMonitorState.selectedAPIKeyLabel);
  const [selectedAccountLabel, setSelectedAccountLabel] = useState(initialMonitorState.selectedAccountLabel);
  const [selectedGroupLabel, setSelectedGroupLabel] = useState(initialMonitorState.selectedGroupLabel);
  const [timeRangePreset, setTimeRangePreset] = useState<MonitorTimeRangePreset>(initialMonitorState.timeRangePreset);
  const [requestTimeRangePreset, setRequestTimeRangePreset] = useState<MonitorTimeRangePreset>(initialMonitorState.requestTimeRangePreset);
  const [customTimeRangeTarget, setCustomTimeRangeTarget] = useState<MonitorTableKey>('events');
  const [customTimeRangeOpen, setCustomTimeRangeOpen] = useState(false);

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

  const runtimeQuery = useQuery({
    queryKey: queryKeys.monitorRuntime(),
    queryFn: ({ signal }) => monitorApi.runtime({ signal }),
    meta: { globalLoading: false },
    refetchOnReconnect: autoRefreshEnabled,
    refetchOnWindowFocus: autoRefreshEnabled,
    placeholderData: keepPreviousData,
  });
  const refetchRuntime = runtimeQuery.refetch;

  const summaryQuery = useQuery({
    queryKey: queryKeys.monitorSummary(),
    queryFn: ({ signal }) => monitorApi.summary({ signal }),
    meta: { globalLoading: false },
    placeholderData: keepPreviousData,
  });
  const refetchSummary = summaryQuery.refetch;

  const requestSummaryQuery = useQuery({
    queryKey: queryKeys.monitorRequests('summary'),
    queryFn: ({ signal }) => monitorApi.requestSummary({ signal }),
    meta: { globalLoading: false },
    placeholderData: keepPreviousData,
  });
  const refetchRequestSummary = requestSummaryQuery.refetch;

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
    setMonitorPageState(1);
  }, [setMonitorPageState]);

  const setActiveTable = useCallback((value: MonitorTableKey) => {
    setActiveTableState(value);
    writeStoredString(MONITOR_FILTER_KEYS.activeTable, value === 'events' ? undefined : value);
  }, []);

  const updateFilter = useCallback((key: keyof MonitorListQuery, value: string | number | undefined) => {
    const nextValue = value === '' ? undefined : value;
    writeStoredString(MONITOR_EVENT_FILTER_STORAGE_KEYS[key], nextValue);
    startTransition(() => {
      setFilters((prev) => ({ ...prev, [key]: nextValue }));
      resetPagination();
    });
  }, [resetPagination]);

  const applyMonitorTimeRange = useCallback((preset: MonitorTimeRangePreset, from?: string, to?: string) => {
    setTimeRangePreset(preset);
    writeStoredTimeRange(MONITOR_FILTER_KEYS.events, preset, from, to);
    startTransition(() => {
      setFilters((prev) => ({ ...prev, from, to }));
      resetPagination();
    });
  }, [resetPagination]);

  const handleTimeRangeSelection = useCallback((value: string) => {
    const preset = value as MonitorTimeRangePreset;
    if (preset === 'custom') {
      setCustomTimeRangeTarget('events');
      setCustomTimeRangeOpen(true);
      return;
    }
    const range = presetTimeRange(preset);
    applyMonitorTimeRange(preset, range.from, range.to);
  }, [applyMonitorTimeRange]);

  const resetRequestPagination = useCallback(() => {
    setRequestCursors({});
    setRequestPageState(1);
  }, [setRequestPageState]);

  const applyRequestTimeRange = useCallback((preset: MonitorTimeRangePreset, from?: string, to?: string) => {
    setRequestTimeRangePreset(preset);
    writeStoredTimeRange(MONITOR_FILTER_KEYS.requests, preset, from, to);
    startTransition(() => {
      setRequestFilters((prev) => ({ ...prev, from, to }));
      resetRequestPagination();
    });
  }, [resetRequestPagination]);

  const handleRequestTimeRangeSelection = useCallback((value: string) => {
    const preset = value as MonitorTimeRangePreset;
    if (preset === 'custom') {
      setCustomTimeRangeTarget('requests');
      setCustomTimeRangeOpen(true);
      return;
    }
    const range = presetTimeRange(preset);
    applyRequestTimeRange(preset, range.from, range.to);
  }, [applyRequestTimeRange]);

  const handleCustomTimeRangeApply = useCallback((from?: string, to?: string) => {
    if (customTimeRangeTarget === 'requests') {
      applyRequestTimeRange(from || to ? 'custom' : 'all', from, to);
    } else {
      applyMonitorTimeRange(from || to ? 'custom' : 'all', from, to);
    }
    setCustomTimeRangeOpen(false);
  }, [applyMonitorTimeRange, applyRequestTimeRange, customTimeRangeTarget]);

  const handleCustomTimeRangeClear = useCallback(() => {
    if (customTimeRangeTarget === 'requests') {
      applyRequestTimeRange('all');
    } else {
      applyMonitorTimeRange('all');
    }
    setCustomTimeRangeOpen(false);
  }, [applyMonitorTimeRange, applyRequestTimeRange, customTimeRangeTarget]);

  const updateRequestFilter = useCallback((key: keyof MonitorRequestListQuery, value: string | number | undefined) => {
    const nextValue = value === '' ? undefined : value;
    writeStoredString(MONITOR_REQUEST_FILTER_STORAGE_KEYS[key], nextValue);
    startTransition(() => {
      setRequestFilters((prev) => ({ ...prev, [key]: nextValue }));
      resetRequestPagination();
    });
  }, [resetRequestPagination]);

  const handleAPIKeySelectionChange = useCallback((value: string, label: string) => {
    const nextLabel = value ? label : '';
    setSelectedAPIKeyLabel(nextLabel);
    writeStoredString(MONITOR_FILTER_KEYS.requests.apiKeyLabel, nextLabel);
    updateRequestFilter('api_key_id', value ? Number(value) : undefined);
  }, [updateRequestFilter]);

  const handleAccountSelectionChange = useCallback((value: string, label: string) => {
    const nextLabel = value ? label : '';
    setSelectedAccountLabel(nextLabel);
    writeStoredString(MONITOR_FILTER_KEYS.requests.accountLabel, nextLabel);
    updateRequestFilter('account_id', value ? Number(value) : undefined);
  }, [updateRequestFilter]);

  const handleGroupSelectionChange = useCallback((value: string, label: string) => {
    const nextLabel = value ? label : '';
    setSelectedGroupLabel(nextLabel);
    writeStoredString(MONITOR_FILTER_KEYS.requests.groupLabel, nextLabel);
    updateRequestFilter('group_id', value ? Number(value) : undefined);
  }, [updateRequestFilter]);

  const queryAccountFilterItems = useCallback(async (keyword: string) => {
    const result = await accountsApi.list({ page: 1, page_size: 20, keyword });
    return result.list;
  }, []);

  const queryGroupFilterItems = useCallback(async (keyword: string) => {
    const result = await groupsApi.list({ page: 1, page_size: 20, keyword });
    return result.list;
  }, []);

  const accountFilterOption = useCallback((account: AccountResp) => {
    const label = account.name || `#${account.id}`;
    const description = [account.email, account.platform].filter(Boolean).join(' · ');
    return {
      id: String(account.id),
      label,
      textValue: `${label} ${account.email ?? ''} ${account.platform}`.trim(),
      description,
    };
  }, []);

  const groupFilterOption = useCallback((group: GroupResp) => {
    const label = group.name || `#${group.id}`;
    return {
      id: String(group.id),
      label,
      textValue: label,
      description: group.platform,
    };
  }, []);

  const setPage = useCallback((nextPage: number) => {
    if (nextPage <= 1) {
      setMonitorPageState(1);
      return;
    }
    if (nextPage === page + 1) {
      if (!data?.next_cursor) return;
      setCursors((current) => ({ ...current, [nextPage]: data.next_cursor }));
      setMonitorPageState(nextPage);
      return;
    }
    if (nextPage < page || cursors[nextPage]) {
      setMonitorPageState(nextPage);
    }
  }, [cursors, data?.next_cursor, page, setMonitorPageState]);

  const setPageSize = useCallback((nextPageSize: number) => {
    setCursors({});
    setMonitorPageState(1);
    setMonitorPageSizeState(nextPageSize);
  }, [setMonitorPageSizeState, setMonitorPageState]);

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
  }, [requestCursors, requestData?.next_cursor, requestPage, setRequestPageState]);

  const setRequestPageSize = useCallback((nextPageSize: number) => {
    setRequestCursors({});
    setRequestPageState(1);
    setRequestPageSizeState(nextPageSize);
  }, [setRequestPageSizeState, setRequestPageState]);

  const handleManualRefresh = useCallback(() => {
    void refetchRuntime({ cancelRefetch: false });
    void refetch({ cancelRefetch: false });
    void refetchSummary({ cancelRefetch: false });
    void refetchRequests({ cancelRefetch: false });
    void refetchRequestSummary({ cancelRefetch: false });
  }, [refetch, refetchRequestSummary, refetchRequests, refetchRuntime, refetchSummary]);

  const handleRuntimeAutoRefresh = useCallback(() => {
    void refetchRuntime({ cancelRefetch: false });
  }, [refetchRuntime]);

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
      void refetchRequestSummary({ cancelRefetch: false });
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
  }, [refetch, refetchRequestSummary, refetchRequests, refetchSummary]);

  const statusOptions: SelectOption[] = [
    { id: '', label: t('common.all') },
    ...MONITOR_EVENT_STATUS_IDS.map((id) => ({ id, label: t(`monitor.status_${id}`, id) })),
  ];
  const severityOptions: SelectOption[] = [
    { id: '', label: t('common.all') },
    ...MONITOR_EVENT_SEVERITY_IDS.map((id) => ({ id, label: t(`monitor.severity_${id}`, id) })),
  ];
  const typeOptions: SelectOption[] = [
    { id: '', label: t('common.all') },
    ...MONITOR_EVENT_TYPE_IDS.map((id) => ({ id, label: t(`monitor.type_${id}`, id) })),
  ];

  const httpStatusOptions: SelectOption[] = [
    { id: '', label: t('common.all') },
    ...MONITOR_HTTP_STATUS_CODES.map((code) => ({ id: code, label: code })),
  ];
  const requestTypeOptions: SelectOption[] = [
    { id: '', label: t('common.all') },
    ...MONITOR_REQUEST_TYPE_IDS.map((id) => ({ id, label: t(`monitor.type_${id}`, id) })),
  ];

  const columns = useMonitorColumns({
    onResolve: resolveMutation.mutate,
    resolvePending: resolveMutation.isPending,
  });
  const requestColumns = useMonitorRequestColumns();

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
  const timeRangeOptions: SelectOption[] = MONITOR_TIME_RANGE_PRESETS.map((item) => ({
    id: item.id,
    label: t(item.labelKey),
  }));
  const customRangeLabel = monitorRangeLabel(filters.from, filters.to);
  const selectedTimeRange = timeRangeOptions.find((item) => item.id === timeRangePreset)?.label ?? t('monitor.time_range_all');
  const timeRangeSelectedLabel = timeRangePreset === 'custom' && customRangeLabel
    ? `${t('monitor.time_range')}: ${customRangeLabel}`
    : `${t('monitor.time_range')}: ${selectedTimeRange}`;
  const requestCustomRangeLabel = monitorRangeLabel(requestFilters.from, requestFilters.to);
  const selectedRequestTimeRange = timeRangeOptions.find((item) => item.id === requestTimeRangePreset)?.label ?? t('monitor.time_range_all');
  const requestTimeRangeSelectedLabel = requestTimeRangePreset === 'custom' && requestCustomRangeLabel
    ? `${t('monitor.time_range')}: ${requestCustomRangeLabel}`
    : `${t('monitor.time_range')}: ${selectedRequestTimeRange}`;
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
  const activeSummary = isRequestTable ? requestSummaryQuery.data : summaryQuery.data;
  const activeRefreshBusy = isRequestTable
    ? isRequestFetching || requestSummaryQuery.isFetching
    : isFetching || summaryQuery.isFetching;

  return (
    <div>
      <MonitorRuntimeStats showActiveCounts={!isRequestTable} snapshot={runtimeQuery.data} summary={activeSummary} />
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
                className={MONITOR_TOOLBAR_CONTROL_CLASS}
                options={tableOptions}
                value={activeTable}
                onChange={(value) => setActiveTable(value as MonitorTableKey)}
              />
              {isRequestTable ? (
                <>
                  <FilterSelect
                    ariaLabel={t('monitor.time_range')}
                    className={MONITOR_TOOLBAR_CONTROL_CLASS}
                    options={timeRangeOptions}
                    selectedLabel={requestTimeRangeSelectedLabel}
                    value={requestTimeRangePreset}
                    onChange={handleRequestTimeRangeSelection}
                  />
                  <FilterSelect
                    ariaLabel={t('monitor.type')}
                    className={MONITOR_TOOLBAR_CONTROL_CLASS}
                    label={t('monitor.type')}
                    options={requestTypeOptions}
                    value={requestFilters.type || ''}
                    onChange={(value) => updateRequestFilter('type', value)}
                  />
                  <FilterSelect
                    ariaLabel={t('monitor.http_status_code')}
                    className={MONITOR_TOOLBAR_CONTROL_CLASS}
                    label={t('monitor.http_status_code')}
                    options={httpStatusOptions}
                    value={requestFilters.http_status ? String(requestFilters.http_status) : ''}
                    onChange={(value) => updateRequestFilter('http_status', value ? Number(value) : undefined)}
                  />
                  <div className={MONITOR_TOOLBAR_CONTROL_CLASS}>
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
                  <div className={MONITOR_TOOLBAR_CONTROL_CLASS}>
                    <RemoteSearchFilterComboBox
                      ariaLabel={t('monitor.search_account')}
                      buildQueryKey={(keyword) => queryKeys.accounts('monitor-filter', keyword)}
                      emptyPrompt={t('monitor.search_account')}
                      loadingLabel={t('common.loading')}
                      mapItemToOption={accountFilterOption}
                      noDataLabel={t('common.no_data')}
                      placeholder={t('monitor.search_account')}
                      queryItems={queryAccountFilterItems}
                      selectedKey={requestFilters.account_id ? String(requestFilters.account_id) : null}
                      selectedLabel={selectedAccountLabel}
                      onSelectionChange={handleAccountSelectionChange}
                    />
                  </div>
                  <div className={MONITOR_TOOLBAR_CONTROL_CLASS}>
                    <RemoteSearchFilterComboBox
                      ariaLabel={t('monitor.search_group')}
                      buildQueryKey={(keyword) => queryKeys.groups('monitor-filter', keyword)}
                      emptyPrompt={t('monitor.search_group')}
                      loadingLabel={t('common.loading')}
                      mapItemToOption={groupFilterOption}
                      noDataLabel={t('common.no_data')}
                      placeholder={t('monitor.search_group')}
                      queryItems={queryGroupFilterItems}
                      selectedKey={requestFilters.group_id ? String(requestFilters.group_id) : null}
                      selectedLabel={selectedGroupLabel}
                      onSelectionChange={handleGroupSelectionChange}
                    />
                  </div>
                </>
              ) : (
                <>
                  <FilterSelect
                    ariaLabel={t('monitor.time_range')}
                    className={MONITOR_TOOLBAR_CONTROL_CLASS}
                    options={timeRangeOptions}
                    selectedLabel={timeRangeSelectedLabel}
                    value={timeRangePreset}
                    onChange={handleTimeRangeSelection}
                  />
                  <FilterSelect
                    ariaLabel={t('monitor.type')}
                    className={MONITOR_TOOLBAR_CONTROL_CLASS}
                    label={t('monitor.type')}
                    options={typeOptions}
                    value={filters.type || ''}
                    onChange={(value) => updateFilter('type', value)}
                  />
                  <FilterSelect
                    ariaLabel={t('monitor.severity')}
                    className={MONITOR_TOOLBAR_CONTROL_CLASS}
                    label={t('monitor.severity')}
                    options={severityOptions}
                    value={filters.severity || ''}
                    onChange={(value) => updateFilter('severity', value)}
                  />
                  <div className={MONITOR_TOOLBAR_CONTROL_CLASS}>
                    <input
                      className="input input--sm w-full"
                      placeholder={t('monitor.source')}
                      value={filters.source ?? ''}
                      onChange={(event) => updateFilter('source', event.target.value)}
                    />
                  </div>
                  <FilterSelect
                    ariaLabel={t('monitor.status')}
                    className={MONITOR_TOOLBAR_CONTROL_CLASS}
                    label={t('monitor.status')}
                    options={statusOptions}
                    value={filters.status || ''}
                    onChange={(value) => updateFilter('status', value)}
                  />
                </>
              )}
            </div>
          </div>
          <div className="ag-page-toolbar-actions">
            <AutoRefreshControl
              value={autoRefresh}
              options={MONITOR_AUTO_REFRESH_OPTIONS}
              label={autoRefreshLabel}
              offLabel={autoRefreshOffLabel}
              refreshButtonClassName="ag-auto-refresh-refresh--toolbar"
              triggerClassName="ag-auto-refresh-trigger--toolbar-fixed"
              ariaLabel={t('monitor.auto_update')}
              refreshAriaLabel={t('common.refresh', 'Refresh')}
              onChange={setAutoRefresh}
              onAutoRefresh={handleRuntimeAutoRefresh}
              onRefresh={handleManualRefresh}
              isAutoRefreshing={runtimeQuery.isFetching}
              isRefreshing={activeRefreshBusy || runtimeQuery.isFetching}
            />
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
      <MonitorCustomTimeRangeModal
        from={customTimeRangeTarget === 'requests' ? requestFilters.from : filters.from}
        isOpen={customTimeRangeOpen}
        to={customTimeRangeTarget === 'requests' ? requestFilters.to : filters.to}
        onApply={handleCustomTimeRangeApply}
        onClear={handleCustomTimeRangeClear}
        onClose={() => setCustomTimeRangeOpen(false)}
      />
    </div>
  );
}
