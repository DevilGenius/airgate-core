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
import {
  UserOrAPIKeySearchFilterComboBox,
  type UserOrAPIKeySearchSelection,
} from '../../shared/components/UserOrAPIKeySearchFilterComboBox';
import { AutoRefreshControl } from '../../shared/components/AutoRefreshControl';
import { NativeSwitch } from '../../shared/components/NativeSwitch';
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
import {
  MonitorFilterSelect as FilterSelect,
  MonitorMultiFilterSelect as MultiFilterSelect,
} from './monitor/MonitorFilterSelect';
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
  MonitorRuntimeFeatureUpdateReq,
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
  'plugin_forward_retry',
  'plugin_forward_error',
  'client_request_error',
  'client_closed_request',
];
const MONITOR_EVENT_STATUS_IDS: readonly string[] = ['active', 'resolved'];
const MONITOR_EVENT_SOURCE_IDS: readonly string[] = [
  'forwarder',
  'scheduler',
  'account_checker',
  'token_refresh',
  'task_runner',
  'plugin_manager',
  'monitor_worker',
];
const MONITOR_EVENT_SEVERITY_IDS: readonly string[] = ['critical', 'error', 'warning', 'info'];
const MONITOR_REQUEST_SEVERITY_IDS: readonly string[] = ['warning', 'info'];
const MONITOR_EVENT_TYPE_IDS: readonly string[] = [
  'scheduler_error',
  'upstream_account_error',
  'plugin_error',
  'task_error',
  'system_error',
];

const MONITOR_FILTER_KEYS = {
  activeTable: `${MONITOR_FILTER_STORAGE_KEY}:active_table`,
  events: {
    from: `${MONITOR_FILTER_STORAGE_KEY}:events:from`,
    severity: `${MONITOR_FILTER_STORAGE_KEY}:events:severity`,
    status: `${MONITOR_FILTER_STORAGE_KEY}:events:status`,
    source: `${MONITOR_FILTER_STORAGE_KEY}:events:source`,
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
    severity: `${MONITOR_FILTER_STORAGE_KEY}:requests:severity`,
    timeRange: `${MONITOR_FILTER_STORAGE_KEY}:requests:time_range`,
    to: `${MONITOR_FILTER_STORAGE_KEY}:requests:to`,
    type: `${MONITOR_FILTER_STORAGE_KEY}:requests:type`,
    userID: `${MONITOR_FILTER_STORAGE_KEY}:requests:user_id`,
    userLabel: `${MONITOR_FILTER_STORAGE_KEY}:requests:user_label`,
  },
} as const;

const MONITOR_EVENT_FILTER_STORAGE_KEYS: Partial<Record<keyof MonitorListQuery, string>> = {
  severity: MONITOR_FILTER_KEYS.events.severity,
  status: MONITOR_FILTER_KEYS.events.status,
  source: MONITOR_FILTER_KEYS.events.source,
  type: MONITOR_FILTER_KEYS.events.type,
};

const MONITOR_REQUEST_FILTER_STORAGE_KEYS: Partial<Record<keyof MonitorRequestListQuery, string>> = {
  account_id: MONITOR_FILTER_KEYS.requests.accountID,
  api_key_id: MONITOR_FILTER_KEYS.requests.apiKeyID,
  group_id: MONITOR_FILTER_KEYS.requests.groupID,
  http_status: MONITOR_FILTER_KEYS.requests.httpStatus,
  severity: MONITOR_FILTER_KEYS.requests.severity,
  type: MONITOR_FILTER_KEYS.requests.type,
  user_id: MONITOR_FILTER_KEYS.requests.userID,
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

function filterValues(value: string | undefined, allowedValues: readonly string[]) {
  const selectedValues = new Set((value ?? '').split(/\s+/).filter(Boolean));
  return allowedValues.filter((item) => selectedValues.has(item));
}

function readStoredOptions(key: string, allowedValues: readonly string[]) {
  const values = filterValues(readStoredString(key), allowedValues);
  return values.length > 0 ? values.join(' ') : undefined;
}

function toggleFilterValue(value: string | undefined, item: string, allowedValues: readonly string[]) {
  const selectedValues = new Set(filterValues(value, allowedValues));
  if (selectedValues.has(item)) {
    selectedValues.delete(item);
  } else {
    selectedValues.add(item);
  }
  const nextValues = allowedValues.filter((candidate) => selectedValues.has(candidate));
  return nextValues.length > 0 ? nextValues.join(' ') : undefined;
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
  const userID = readStoredPositiveNumber(MONITOR_FILTER_KEYS.requests.userID);
  const filters: Partial<MonitorListQuery> = {
    from: eventsTimeRange.from,
    severity: readStoredOptions(MONITOR_FILTER_KEYS.events.severity, MONITOR_EVENT_SEVERITY_IDS),
    status: readStoredOptions(MONITOR_FILTER_KEYS.events.status, MONITOR_EVENT_STATUS_IDS),
    source: readStoredOptions(MONITOR_FILTER_KEYS.events.source, MONITOR_EVENT_SOURCE_IDS),
    to: eventsTimeRange.to,
    type: readStoredOptions(MONITOR_FILTER_KEYS.events.type, MONITOR_EVENT_TYPE_IDS),
  };
  const requestFilters: Partial<MonitorRequestListQuery> = {
    account_id: accountID,
    api_key_id: apiKeyID,
    from: requestsTimeRange.from,
    group_id: groupID,
    http_status: readStoredString(MONITOR_FILTER_KEYS.requests.httpStatus) || undefined,
    severity: readStoredOptions(MONITOR_FILTER_KEYS.requests.severity, MONITOR_REQUEST_SEVERITY_IDS),
    to: requestsTimeRange.to,
    type: readStoredOptions(MONITOR_FILTER_KEYS.requests.type, MONITOR_REQUEST_TYPE_IDS),
    user_id: userID,
  };

  return {
    activeTable: normalizeStoredMonitorTable(readStoredString(MONITOR_FILTER_KEYS.activeTable)),
    filters,
    requestFilters,
    requestTimeRangePreset: requestsTimeRange.preset,
    selectedAPIKeyLabel: storedIDLabel(apiKeyID, readStoredString(MONITOR_FILTER_KEYS.requests.apiKeyLabel)),
    selectedAccountLabel: storedIDLabel(accountID, readStoredString(MONITOR_FILTER_KEYS.requests.accountLabel)),
    selectedGroupLabel: storedIDLabel(groupID, readStoredString(MONITOR_FILTER_KEYS.requests.groupLabel)),
    selectedUserLabel: storedIDLabel(userID, readStoredString(MONITOR_FILTER_KEYS.requests.userLabel)),
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
  const [selectedUserLabel, setSelectedUserLabel] = useState(initialMonitorState.selectedUserLabel);
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

  const runtimeFeaturesQuery = useQuery({
    queryKey: queryKeys.monitorRuntimeFeatures(),
    queryFn: ({ signal }) => monitorApi.runtimeFeatures({ signal }),
    enabled: activeTable === 'events',
    meta: { globalLoading: false },
  });

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

  const clearRequestTracesMutation = useMutation({
    mutationFn: () => monitorApi.clearRequestTraces(),
    onSuccess: (result) => {
      toast('success', t('monitor.request_trace_clear_success', { count: result.deleted }));
    },
    onError: (err: Error) => toast('error', err.message),
  });

  const runtimeFeaturesMutation = useMutation({
    mutationFn: monitorApi.updateRuntimeFeatures,
    onSuccess: (result, input: MonitorRuntimeFeatureUpdateReq) => {
      queryClient.setQueryData(queryKeys.monitorRuntimeFeatures(), result);
      if (typeof input.text_hash_enabled === 'boolean') {
        toast('success', t(input.text_hash_enabled ? 'monitor.text_hash_enabled' : 'monitor.text_hash_disabled'));
      } else if (typeof input.image_hash_enabled === 'boolean') {
        toast('success', t(input.image_hash_enabled ? 'monitor.image_hash_enabled' : 'monitor.image_hash_disabled'));
      } else if (typeof input.request_trace_enabled === 'boolean') {
        toast('success', t(input.request_trace_enabled ? 'monitor.request_trace_enabled' : 'monitor.request_trace_disabled'));
      }
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
    setFilters((prev) => ({ ...prev, [key]: nextValue }));
    resetPagination();
  }, [resetPagination]);

  const updateEventClassificationFilters = useCallback((type?: string, severity?: string) => {
    writeStoredString(MONITOR_FILTER_KEYS.events.type, type);
    writeStoredString(MONITOR_FILTER_KEYS.events.severity, severity);
    setFilters((prev) => ({ ...prev, type, severity }));
    resetPagination();
  }, [resetPagination]);

  const toggleEventClassificationFilter = useCallback((groupID: string, value: string) => {
    if (groupID === 'severity') {
      updateEventClassificationFilters(
        filters.type,
        toggleFilterValue(filters.severity, value, MONITOR_EVENT_SEVERITY_IDS),
      );
      return;
    }
    updateEventClassificationFilters(
      toggleFilterValue(filters.type, value, MONITOR_EVENT_TYPE_IDS),
      filters.severity,
    );
  }, [filters.severity, filters.type, updateEventClassificationFilters]);

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

  const toggleEventStatusFilter = useCallback((value: string) => {
    updateFilter(
      'status',
      toggleFilterValue(filters.status, value, MONITOR_EVENT_STATUS_IDS),
    );
  }, [filters.status, updateFilter]);

  const toggleEventSourceFilter = useCallback((groupID: string, value: string) => {
    if (groupID !== 'source') return;
    updateFilter(
      'source',
      toggleFilterValue(filters.source, value, MONITOR_EVENT_SOURCE_IDS),
    );
  }, [filters.source, updateFilter]);

  const clearEventSourceFilter = useCallback(() => {
    updateFilter('source', undefined);
  }, [updateFilter]);

  const toggleEventFilter = useCallback((groupID: string, value: string) => {
    if (groupID === 'time_range') {
      handleTimeRangeSelection(value);
      return;
    }
    if (groupID === 'status') {
      toggleEventStatusFilter(value);
      return;
    }
    toggleEventClassificationFilter(groupID, value);
  }, [handleTimeRangeSelection, toggleEventClassificationFilter, toggleEventStatusFilter]);

  const clearEventFilters = useCallback(() => {
    setTimeRangePreset('all');
    writeStoredString(MONITOR_FILTER_KEYS.events.type, undefined);
    writeStoredString(MONITOR_FILTER_KEYS.events.severity, undefined);
    writeStoredString(MONITOR_FILTER_KEYS.events.status, undefined);
    writeStoredString(MONITOR_FILTER_KEYS.events.source, undefined);
    writeStoredTimeRange(MONITOR_FILTER_KEYS.events, 'all');
    startTransition(() => {
      setFilters((prev) => ({
        ...prev,
        from: undefined,
        severity: undefined,
        status: undefined,
        source: undefined,
        to: undefined,
        type: undefined,
      }));
      resetPagination();
    });
  }, [resetPagination]);

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

  const clearRequestFilters = useCallback(() => {
    setRequestTimeRangePreset('all');
    writeStoredString(MONITOR_FILTER_KEYS.requests.type, undefined);
    writeStoredString(MONITOR_FILTER_KEYS.requests.severity, undefined);
    writeStoredTimeRange(MONITOR_FILTER_KEYS.requests, 'all');
    startTransition(() => {
      setRequestFilters((prev) => ({
        ...prev,
        from: undefined,
        severity: undefined,
        to: undefined,
        type: undefined,
      }));
      resetRequestPagination();
    });
  }, [resetRequestPagination]);

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

  const updateRequestClassificationFilters = useCallback((type?: string, severity?: string) => {
    writeStoredString(MONITOR_FILTER_KEYS.requests.type, type);
    writeStoredString(MONITOR_FILTER_KEYS.requests.severity, severity);
    setRequestFilters((prev) => ({ ...prev, type, severity }));
    resetRequestPagination();
  }, [resetRequestPagination]);

  const toggleRequestClassificationFilter = useCallback((groupID: string, value: string) => {
    if (groupID === 'severity') {
      updateRequestClassificationFilters(
        requestFilters.type,
        toggleFilterValue(requestFilters.severity, value, MONITOR_REQUEST_SEVERITY_IDS),
      );
      return;
    }
    updateRequestClassificationFilters(
      toggleFilterValue(requestFilters.type, value, MONITOR_REQUEST_TYPE_IDS),
      requestFilters.severity,
    );
  }, [requestFilters.severity, requestFilters.type, updateRequestClassificationFilters]);

  const toggleRequestFilter = useCallback((groupID: string, value: string) => {
    if (groupID === 'time_range') {
      handleRequestTimeRangeSelection(value);
      return;
    }
    toggleRequestClassificationFilter(groupID, value);
  }, [handleRequestTimeRangeSelection, toggleRequestClassificationFilter]);

  const updateRequestHTTPStatusFilter = useCallback((value: string) => {
    const nextValue = value === '' ? undefined : value;
    writeStoredString(MONITOR_FILTER_KEYS.requests.httpStatus, nextValue);
    setRequestFilters((prev) => ({ ...prev, http_status: nextValue }));
    resetRequestPagination();
  }, [resetRequestPagination]);

  const handleUserOrAPIKeySelectionChange = useCallback((selection: UserOrAPIKeySearchSelection | null) => {
    const userID = selection?.kind === 'user' ? Number(selection.id) : undefined;
    const apiKeyID = selection?.kind === 'api_key' ? Number(selection.id) : undefined;
    const userLabel = selection?.kind === 'user' ? selection.label : '';
    const apiKeyLabel = selection?.kind === 'api_key' ? selection.label : '';
    setSelectedUserLabel(userLabel);
    setSelectedAPIKeyLabel(apiKeyLabel);
    writeStoredString(MONITOR_FILTER_KEYS.requests.userLabel, userLabel);
    writeStoredString(MONITOR_FILTER_KEYS.requests.apiKeyLabel, apiKeyLabel);
    writeStoredString(MONITOR_FILTER_KEYS.requests.userID, userID);
    writeStoredString(MONITOR_FILTER_KEYS.requests.apiKeyID, apiKeyID);
    startTransition(() => {
      setRequestFilters((prev) => ({ ...prev, api_key_id: apiKeyID, user_id: userID }));
      resetRequestPagination();
    });
  }, [resetRequestPagination]);

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

  const selectedSearchKind = requestFilters.user_id != null
    ? 'user'
    : requestFilters.api_key_id != null
      ? 'api_key'
      : undefined;
  const selectedSearchKey = selectedSearchKind === 'user'
    ? String(requestFilters.user_id)
    : selectedSearchKind === 'api_key'
      ? String(requestFilters.api_key_id)
      : null;
  const selectedSearchLabel = selectedSearchKind === 'user'
    ? selectedUserLabel
    : selectedSearchKind === 'api_key'
      ? selectedAPIKeyLabel
      : '';

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
    ...MONITOR_EVENT_STATUS_IDS.map((id) => ({ id, label: t(`monitor.status_${id}`, id) })),
  ];
  const sourceOptions: SelectOption[] = [
    ...MONITOR_EVENT_SOURCE_IDS.map((id) => ({ id, label: t(`monitor.source_${id}`, id) })),
  ];
  const severityOptions: SelectOption[] = [
    ...MONITOR_EVENT_SEVERITY_IDS.map((id) => ({ id, label: t(`monitor.severity_${id}`, id) })),
  ];
  const requestSeverityOptions: SelectOption[] = [
    ...MONITOR_REQUEST_SEVERITY_IDS.map((id) => ({ id, label: t(`monitor.request_severity_${id}`, id) })),
  ];
  const typeOptions: SelectOption[] = [
    ...MONITOR_EVENT_TYPE_IDS.map((id) => ({ id, label: t(`monitor.type_${id}`, id) })),
  ];
  const requestTypeOptions: SelectOption[] = [
    ...MONITOR_REQUEST_TYPE_IDS.map((id) => ({ id, label: t(`monitor.type_${id}`, id) })),
  ];
  const selectedEventTypes = filterValues(filters.type, MONITOR_EVENT_TYPE_IDS);
  const selectedEventSeverities = filterValues(filters.severity, MONITOR_EVENT_SEVERITY_IDS);
  const selectedEventStatuses = filterValues(filters.status, MONITOR_EVENT_STATUS_IDS);
  const selectedEventSources = filterValues(filters.source, MONITOR_EVENT_SOURCE_IDS);
  const selectedRequestTypes = filterValues(requestFilters.type, MONITOR_REQUEST_TYPE_IDS);
  const selectedRequestSeverities = filterValues(requestFilters.severity, MONITOR_REQUEST_SEVERITY_IDS);

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
  const pendingRuntimeFeatures = runtimeFeaturesMutation.isPending
    ? runtimeFeaturesMutation.variables
    : undefined;
  const textHashEnabled = pendingRuntimeFeatures?.text_hash_enabled
    ?? runtimeFeaturesQuery.data?.text_hash_enabled
    ?? true;
  const imageHashEnabled = pendingRuntimeFeatures?.image_hash_enabled
    ?? runtimeFeaturesQuery.data?.image_hash_enabled
    ?? true;
  const requestTraceEnabled = pendingRuntimeFeatures?.request_trace_enabled
    ?? runtimeFeaturesQuery.data?.request_trace_enabled
    ?? false;

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
                  <MultiFilterSelect
                    allLabel={t('common.all')}
                    ariaLabel={t('monitor.type')}
                    className={MONITOR_TOOLBAR_CONTROL_CLASS}
                    label={t('monitor.type')}
                    groups={[
                      {
                        id: 'type',
                        label: t('monitor.type'),
                        options: requestTypeOptions,
                        selectedValues: selectedRequestTypes,
                      },
                      {
                        id: 'severity',
                        label: t('monitor.severity'),
                        options: requestSeverityOptions,
                        selectedValues: selectedRequestSeverities,
                      },
                      {
                        defaultValue: 'all',
                        id: 'time_range',
                        label: t('monitor.time_range'),
                        options: timeRangeOptions,
                        selectionMode: 'single',
                        selectedValues: [requestTimeRangePreset],
                        showInSummary: false,
                        summaryLabel: requestTimeRangeSelectedLabel,
                      },
                    ]}
                    onClear={clearRequestFilters}
                    onToggle={toggleRequestFilter}
                  />
                  <div className={MONITOR_TOOLBAR_CONTROL_CLASS}>
                    <input
                      aria-label={t('monitor.error_code')}
                      autoComplete="off"
                      className="input input--sm w-full"
                      placeholder={t('monitor.http_status_filter_placeholder')}
                      spellCheck={false}
                      title={t('monitor.http_status_filter_hint')}
                      value={requestFilters.http_status ?? ''}
                      onChange={(event) => updateRequestHTTPStatusFilter(event.target.value)}
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
                      placeholder={t('monitor.group_placeholder')}
                      queryItems={queryGroupFilterItems}
                      selectedKey={requestFilters.group_id ? String(requestFilters.group_id) : null}
                      selectedLabel={selectedGroupLabel}
                      onSelectionChange={handleGroupSelectionChange}
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
                      placeholder={t('monitor.account_placeholder')}
                      queryItems={queryAccountFilterItems}
                      selectedKey={requestFilters.account_id ? String(requestFilters.account_id) : null}
                      selectedLabel={selectedAccountLabel}
                      onSelectionChange={handleAccountSelectionChange}
                    />
                  </div>
                  <div className={MONITOR_TOOLBAR_CONTROL_CLASS}>
                    <UserOrAPIKeySearchFilterComboBox
                      ariaLabel={t('monitor.search_user_or_api_key')}
                      emptyPrompt={t('monitor.search_user_or_api_key')}
                      loadingLabel={t('common.loading')}
                      noDataLabel={t('common.no_data')}
                      placeholder={t('monitor.api_key_placeholder')}
                      selectedKey={selectedSearchKey}
                      selectedKind={selectedSearchKind}
                      selectedLabel={selectedSearchLabel}
                      onSelectionChange={handleUserOrAPIKeySelectionChange}
                    />
                  </div>
                </>
              ) : (
                <>
                  <MultiFilterSelect
                    allLabel={t('common.all')}
                    ariaLabel={t('monitor.type')}
                    className={MONITOR_TOOLBAR_CONTROL_CLASS}
                    label={t('monitor.type')}
                    groups={[
                      {
                        id: 'type',
                        label: t('monitor.type'),
                        options: typeOptions,
                        selectedValues: selectedEventTypes,
                      },
                      {
                        id: 'severity',
                        label: t('monitor.severity'),
                        options: severityOptions,
                        selectedValues: selectedEventSeverities,
                      },
                      {
                        id: 'status',
                        label: t('monitor.status'),
                        options: statusOptions,
                        selectionMode: 'multiple',
                        selectedValues: selectedEventStatuses,
                      },
                      {
                        defaultValue: 'all',
                        id: 'time_range',
                        label: t('monitor.time_range'),
                        options: timeRangeOptions,
                        selectionMode: 'single',
                        selectedValues: [timeRangePreset],
                        showInSummary: false,
                        summaryLabel: timeRangeSelectedLabel,
                      },
                    ]}
                    onClear={clearEventFilters}
                    onToggle={toggleEventFilter}
                  />
                  <MultiFilterSelect
                    allLabel={t('common.all')}
                    ariaLabel={t('monitor.source')}
                    className={MONITOR_TOOLBAR_CONTROL_CLASS}
                    label={t('monitor.source')}
                    groups={[{
                      id: 'source',
                      label: t('monitor.source'),
                      options: sourceOptions,
                      selectionMode: 'multiple',
                      selectedValues: selectedEventSources,
                    }]}
                    onClear={clearEventSourceFilter}
                    onToggle={toggleEventSourceFilter}
                  />
                </>
              )}
            </div>
          </div>
          <div className="ag-page-toolbar-actions">
            <AutoRefreshControl
              beforeRefresh={!isRequestTable ? (
                <div className="flex items-center gap-2">
                  <NativeSwitch
                    ariaLabel={t('monitor.text_hash')}
                    className="ag-page-toolbar-switch"
                    isDisabled={runtimeFeaturesQuery.isLoading || runtimeFeaturesMutation.isPending}
                    isSelected={textHashEnabled}
                    label={t('monitor.text_hash')}
                    onChange={(enabled) => runtimeFeaturesMutation.mutate({ text_hash_enabled: enabled })}
                  />
                  <NativeSwitch
                    ariaLabel={t('monitor.image_hash')}
                    className="ag-page-toolbar-switch"
                    isDisabled={runtimeFeaturesQuery.isLoading || runtimeFeaturesMutation.isPending}
                    isSelected={imageHashEnabled}
                    label={t('monitor.image_hash')}
                    onChange={(enabled) => runtimeFeaturesMutation.mutate({ image_hash_enabled: enabled })}
                  />
                  <NativeSwitch
                    ariaLabel={t('monitor.request_trace')}
                    className="ag-page-toolbar-switch"
                    isDisabled={runtimeFeaturesQuery.isLoading || runtimeFeaturesMutation.isPending}
                    isSelected={requestTraceEnabled}
                    label={t('monitor.request_trace')}
                    onChange={(enabled) => runtimeFeaturesMutation.mutate({ request_trace_enabled: enabled })}
                  />
                </div>
              ) : null}
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
              afterAutoRefresh={!isRequestTable ? (
                <Button
                  isIconOnly
                  aria-label={t('monitor.clear_request_traces')}
                  className="h-8 w-8 min-w-8"
                  isDisabled={clearRequestTracesMutation.isPending}
                  size="sm"
                  variant="ghost"
                  onPress={() => clearRequestTracesMutation.mutate()}
                >
                  <Trash2 className="h-4 w-4" />
                </Button>
              ) : null}
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
