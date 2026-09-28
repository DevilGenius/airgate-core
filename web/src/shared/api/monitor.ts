import { del, get, patch } from './client';
import type {
  MonitorListQuery,
  MonitorListResp,
  MonitorRequestClearResp,
  MonitorRequestListQuery,
  MonitorRequestListResp,
  MonitorRuntimeFeatureStateResp,
  MonitorRuntimeFeatureUpdateReq,
  MonitorRuntimeResp,
  MonitorSummaryResp,
} from '../types';

type MonitorRequestOptions = {
  signal?: AbortSignal;
};

export const monitorApi = {
  runtime: (options?: MonitorRequestOptions) =>
    get<MonitorRuntimeResp>('/api/v1/admin/monitor/runtime', undefined, options),
  summary: (options?: MonitorRequestOptions) =>
    get<MonitorSummaryResp>('/api/v1/admin/monitor/summary', undefined, options),
  requestSummary: (options?: MonitorRequestOptions) =>
    get<MonitorSummaryResp>('/api/v1/admin/monitor/requests/summary', undefined, options),
  list: (params: MonitorListQuery, options?: MonitorRequestOptions) =>
    get<MonitorListResp>('/api/v1/admin/monitor', params, options),
  requestList: (params: MonitorRequestListQuery, options?: MonitorRequestOptions) =>
    get<MonitorRequestListResp>('/api/v1/admin/monitor/requests', params, options),
  /**
   * 删除 created_at < before 的事件及 last_seen_at < before 的 Trace；省略则清空两者。
   * deleted 只统计事件。两次删除非原子，报错时事件可能已删除。
   */
  clearRequests: (before?: string) =>
    del<MonitorRequestClearResp>('/api/v1/admin/monitor/requests', before ? { before } : undefined),
  clearRequestTraces: () =>
    del<MonitorRequestClearResp>('/api/v1/admin/monitor/request-traces'),
  runtimeFeatures: (options?: MonitorRequestOptions) =>
    get<MonitorRuntimeFeatureStateResp>('/api/v1/admin/monitor/runtime-features', undefined, options),
  updateRuntimeFeatures: (input: MonitorRuntimeFeatureUpdateReq) =>
    patch<MonitorRuntimeFeatureStateResp>('/api/v1/admin/monitor/runtime-features', input),
  resolve: (id: number) => patch<void>(`/api/v1/admin/monitor/${id}/resolve`),
};
