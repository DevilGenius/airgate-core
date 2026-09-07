import { get } from './client';
import type { UsageLogResp, CustomerUsageLogResp, UsageQuery, UsageStatsResp, UsageTrendBucket, PagedData, UsagePaginationInfo } from '../types';

type UsageRequestOptions = {
  signal?: AbortSignal;
};

export const usageApi = {
  // 用户接口
  list: (params: UsageQuery, options?: UsageRequestOptions) =>
    get<PagedData<UsageLogResp | CustomerUsageLogResp>>('/api/v1/usage', params, options),
  pagination: (params: Partial<UsageQuery> & { refresh_pagination?: boolean }, options?: UsageRequestOptions) =>
    get<UsagePaginationInfo>('/api/v1/usage/pagination', { ...params, page: 1, page_size: 20 }, options),
  userStats: (params: Omit<UsageQuery, 'page' | 'page_size'>, options?: UsageRequestOptions) =>
    get<UsageStatsResp>('/api/v1/usage/stats', params, options),
  userTrend: (params: { granularity: string; start_date?: string; end_date?: string }, options?: UsageRequestOptions) =>
    get<UsageTrendBucket[]>('/api/v1/usage/trend', params, options),

  // 管理员接口
  adminList: (params: UsageQuery, options?: UsageRequestOptions) =>
    get<PagedData<UsageLogResp>>('/api/v1/admin/usage', params, options),
  adminPagination: (params: Partial<UsageQuery> & { refresh_pagination?: boolean }, options?: UsageRequestOptions) =>
    get<UsagePaginationInfo>('/api/v1/admin/usage/pagination', { ...params, page: 1, page_size: 20 }, options),
  stats: (params: { group_by?: string; include_summary?: boolean; start_date?: string; end_date?: string; platform?: string; model?: string; account?: string; user_id?: number; api_key_id?: number }, options?: UsageRequestOptions) =>
    get<UsageStatsResp>('/api/v1/admin/usage/stats', params, options),
  trend: (params: { granularity: string; start_date?: string; end_date?: string; platform?: string; model?: string; account?: string; user_id?: number; api_key_id?: number }, options?: UsageRequestOptions) =>
    get<UsageTrendBucket[]>('/api/v1/admin/usage/trend', params, options),
};
