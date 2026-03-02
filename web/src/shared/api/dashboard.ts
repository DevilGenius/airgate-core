import { get } from './client';
import type { DashboardStatsResp } from '../types';

export const dashboardApi = {
  stats: () => get<DashboardStatsResp>('/api/v1/admin/dashboard/stats'),
};
