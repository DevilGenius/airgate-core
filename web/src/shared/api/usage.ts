import { get } from './client';
import type { UsageLogResp, UsageQuery, UsageStatsResp, PagedData } from '../types';

export const usageApi = {
  // 用户接口
  list: (params: UsageQuery) =>
    get<PagedData<UsageLogResp>>('/api/v1/usage', params),

  // 管理员接口
  adminList: (params: UsageQuery) =>
    get<PagedData<UsageLogResp>>('/api/v1/admin/usage', params),
  stats: (params: { group_by: string; start_date?: string; end_date?: string }) =>
    get<UsageStatsResp>('/api/v1/admin/usage/stats', params),
};
