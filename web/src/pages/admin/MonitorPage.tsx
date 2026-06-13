import { startTransition, useCallback, useEffect, useMemo, useState } from 'react';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Button } from '@heroui/react';
import { RefreshCw, Trash2 } from 'lucide-react';
import { monitorApi } from '../../shared/api/monitor';
import { subscribeAdminEvents } from '../../shared/api/adminEvents';
import { queryKeys } from '../../shared/queryKeys';
import { APIKeySearchFilterComboBox } from '../../shared/components/APIKeySearchFilterComboBox';
import { RecordsTable } from '../../shared/components/RecordsTable';
import { TablePage } from '../../shared/components/TablePage';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { PAGE_SIZE_OPTIONS } from '../../shared/constants';
import { getTotalPages } from '../../shared/utils/pagination';
import { useToast } from '../../shared/ui';
import { DEFAULT_PAGE_SIZE, MONITOR_TIME_RANGE_PRESETS } from './monitor/constants';
import { MonitorCustomTimeRangeModal } from './monitor/MonitorCustomTimeRangeModal';
import { MonitorFilterSelect as FilterSelect } from './monitor/MonitorFilterSelect';
import { MonitorStats } from './monitor/MonitorStats';
import { totalForCursorPage } from './monitor/pagination';
import { monitorRangeLabel, presetTimeRange } from './monitor/timeRange';
import { useMonitorColumns, useMonitorRequestColumns } from './monitor/useMonitorColumns';
import type {
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
  const [timeRangePreset, setTimeRangePreset] = useState<MonitorTimeRangePreset>('all');
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

  const applyMonitorTimeRange = useCallback((preset: MonitorTimeRangePreset, from?: string, to?: string) => {
    setTimeRangePreset(preset);
    startTransition(() => {
      setFilters((prev) => ({ ...prev, from, to }));
      resetPagination();
    });
  }, [resetPagination]);

  const handleTimeRangeSelection = useCallback((value: string) => {
    const preset = value as MonitorTimeRangePreset;
    if (preset === 'custom') {
      setCustomTimeRangeOpen(true);
      return;
    }
    const range = presetTimeRange(preset);
    applyMonitorTimeRange(preset, range.from, range.to);
  }, [applyMonitorTimeRange]);

  const handleCustomTimeRangeApply = useCallback((from?: string, to?: string) => {
    applyMonitorTimeRange(from || to ? 'custom' : 'all', from, to);
    setCustomTimeRangeOpen(false);
  }, [applyMonitorTimeRange]);

  const handleCustomTimeRangeClear = useCallback(() => {
    applyMonitorTimeRange('all');
    setCustomTimeRangeOpen(false);
  }, [applyMonitorTimeRange]);

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

  const columns = useMonitorColumns({
    ignorePending: ignoreMutation.isPending,
    onIgnore: ignoreMutation.mutate,
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
                  <FilterSelect
                    ariaLabel={t('monitor.time_range')}
                    className="w-full sm:w-72"
                    options={timeRangeOptions}
                    selectedLabel={timeRangeSelectedLabel}
                    value={timeRangePreset}
                    onChange={handleTimeRangeSelection}
                  />
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
      <MonitorCustomTimeRangeModal
        from={filters.from}
        isOpen={customTimeRangeOpen}
        to={filters.to}
        onApply={handleCustomTimeRangeApply}
        onClear={handleCustomTimeRangeClear}
        onClose={() => setCustomTimeRangeOpen(false)}
      />
    </div>
  );
}
