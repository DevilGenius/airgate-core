import { del, get, patch } from './client';
import type {
  MonitorListQuery,
  MonitorListResp,
  MonitorRequestClearResp,
  MonitorRequestListQuery,
  MonitorRequestListResp,
  MonitorSummaryResp,
} from '../types';

type MonitorRequestOptions = {
  signal?: AbortSignal;
};

export const monitorApi = {
  summary: (options?: MonitorRequestOptions) =>
    get<MonitorSummaryResp>('/api/v1/admin/monitor/summary', undefined, options),
  list: (params: MonitorListQuery, options?: MonitorRequestOptions) =>
    get<MonitorListResp>('/api/v1/admin/monitor', params, options),
  requestList: (params: MonitorRequestListQuery, options?: MonitorRequestOptions) =>
    get<MonitorRequestListResp>('/api/v1/admin/monitor/requests', params, options),
  clearRequests: (before?: string) =>
    del<MonitorRequestClearResp>('/api/v1/admin/monitor/requests', before ? { before } : undefined),
  resolve: (id: number) => patch<void>(`/api/v1/admin/monitor/${id}/resolve`),
  ignore: (id: number) => patch<void>(`/api/v1/admin/monitor/${id}/ignore`),
};
