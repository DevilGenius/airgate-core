import { get, post, put, del, patch } from './client';
import type {
  AccountResp, CreateAccountReq, UpdateAccountReq,
  AccountExportFile, ImportAccountsResp, AccountExportItem,
  CredentialSchemaResp, ModelInfo, PageReq, PagedData,
} from '../types';

export type AccountListFilter = { platform?: string; status?: string; group_id?: number; proxy_id?: number };

export const accountsApi = {
  list: (params: PageReq & AccountListFilter) =>
    get<PagedData<AccountResp>>('/api/v1/admin/accounts', params),
  // 按当前筛选条件导出全部账号（不分页）
  export: (params: { keyword?: string } & AccountListFilter) =>
    get<AccountExportFile>('/api/v1/admin/accounts/export', params),
  // 批量导入账号
  import: (accounts: AccountExportItem[]) =>
    post<ImportAccountsResp>('/api/v1/admin/accounts/import', { accounts }),
  create: (data: CreateAccountReq) => post<AccountResp>('/api/v1/admin/accounts', data),
  update: (id: number, data: UpdateAccountReq) => put<void>(`/api/v1/admin/accounts/${id}`, data),
  delete: (id: number) => del<void>(`/api/v1/admin/accounts/${id}`),
  // 切换调度状态（active ↔ disabled）
  toggleScheduling: (id: number) => patch<{ id: number; status: string }>(`/api/v1/admin/accounts/${id}/toggle`),
  // 获取账号所属平台的模型列表
  models: (id: number) => get<ModelInfo[]>(`/api/v1/admin/accounts/${id}/models`),
  // 测试连接 URL（SSE 流式，前端用 fetch 消费）
  testUrl: (id: number) => `/api/v1/admin/accounts/${id}/test`,
  // 获取指定平台账号的用量窗口（插件提供，格式因平台而异）
  usage: (platform: string) =>
    get<{ accounts: Record<string, any> }>('/api/v1/admin/accounts/usage', { platform }),
  credentialsSchema: (platform: string) =>
    get<CredentialSchemaResp>(`/api/v1/admin/accounts/credentials-schema/${platform}`),
  // 手动刷新账号额度（调用插件 QueryQuota）
  refreshQuota: (id: number) =>
    post<{ plan_type?: string; email?: string; subscription_active_until?: string }>(`/api/v1/admin/accounts/${id}/refresh-quota`),
  // 获取账号使用统计（可选时间范围）
  stats: (id: number, params?: { start_date?: string; end_date?: string }) =>
    get<AccountStatsResp>(`/api/v1/admin/accounts/${id}/stats`, params),
};

export interface AccountPeriodStats {
  count: number;
  input_tokens: number;
  output_tokens: number;
  total_cost: number;
  actual_cost: number;
}

export interface AccountDailyStats {
  date: string;
  count: number;
  total_cost: number;
  actual_cost: number;
}

export interface AccountModelStats {
  model: string;
  count: number;
  input_tokens: number;
  output_tokens: number;
  total_cost: number;
  actual_cost: number;
}

export interface AccountPeakDay {
  date: string;
  count: number;
  total_cost: number;
  actual_cost: number;
}

export interface AccountStatsResp {
  account_id: number;
  name: string;
  platform: string;
  status: string;
  start_date: string;
  end_date: string;
  total_days: number;
  today: AccountPeriodStats;
  range: AccountPeriodStats;
  daily_trend: AccountDailyStats[];
  models: AccountModelStats[];
  active_days: number;
  avg_duration_ms: number;
  peak_cost_day: AccountPeakDay;
  peak_request_day: AccountPeakDay;
}
