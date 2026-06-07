import { get, patch } from './client';
import type { MonitorListQuery, MonitorListResp, MonitorSummaryResp } from '../types';

type MonitorRequestOptions = {
  signal?: AbortSignal;
};

export const monitorApi = {
  summary: (options?: MonitorRequestOptions) =>
    get<MonitorSummaryResp>('/api/v1/admin/monitor/summary', undefined, options),
  list: (params: MonitorListQuery, options?: MonitorRequestOptions) =>
    get<MonitorListResp>('/api/v1/admin/monitor', params, options),
  resolve: (id: number) => patch<void>(`/api/v1/admin/monitor/${id}/resolve`),
  ignore: (id: number) => patch<void>(`/api/v1/admin/monitor/${id}/ignore`),
};
