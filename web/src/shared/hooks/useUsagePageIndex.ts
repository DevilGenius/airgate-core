import { useCallback, useRef } from 'react';
import { useQuery } from '@tanstack/react-query';
import { usageApi } from '../api/usage';
import { ApiError } from '../api/client';
import type { UsageQuery } from '../types';

export function isUsagePaginationExpired(error: unknown) {
  return error instanceof ApiError && error.code === 40901;
}

// This query never gates the live table query. Pinned pages keep their original
// view while the first page can continue showing newly arrived records.
export function useUsagePageIndex(scope: 'admin' | 'user', filters: Partial<UsageQuery>, enabled: boolean) {
  const forceRefresh = useRef<{ from?: string; started: boolean } | null>(null);
  const query = useQuery({
    queryKey: ['usage-page-index', scope, filters],
    queryFn: async ({ signal }) => {
      const refresh = forceRefresh.current;
      const params = { ...filters, refresh_pagination: refresh && !refresh.started ? true : undefined };
      const info = await (scope === 'admin'
        ? usageApi.adminPagination(params, { signal })
        : usageApi.pagination(params, { signal }));
      if (refresh && forceRefresh.current === refresh) {
        if (info.status === 'failed' || (info.status === 'ready' && info.snapshot !== refresh.from)) {
          forceRefresh.current = null;
        } else {
          // Retain the request through the server's cooldown or a busy builder.
          refresh.started = info.status === 'preparing' || Boolean(info.refreshing);
          return { ...info, refreshing: true };
        }
      }
      return info;
    },
    enabled,
    meta: { globalLoading: false },
    retry: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    refetchInterval: (query) => query.state.data?.status === 'preparing' || query.state.data?.refreshing ? 1500 : 30_000,
  });
  const { refetch } = query;
  const refresh = useCallback(() => {
    forceRefresh.current = { from: query.data?.snapshot, started: false };
    void refetch();
  }, [query.data?.snapshot, refetch]);
  return { info: query.data, error: query.error, refresh };
}
