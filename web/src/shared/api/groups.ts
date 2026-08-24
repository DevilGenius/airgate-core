import { del, get, post, put } from './client';
import type {
  GroupResp,
  GroupOverviewResp,
  CreateGroupReq,
  UpdateGroupReq,
  PageReq,
  PagedData,
  GroupRateOverrideResp,
} from '../types';

export const groupsApi = {
  // 用户接口
  listAvailable: (params: PageReq & { platform?: string }) =>
    get<PagedData<GroupResp>>('/api/v1/groups', params),

  // 管理员轻量配置列表
  list: (params: PageReq & { platform?: string; service_tier?: string }) =>
    get<PagedData<GroupResp>>('/api/v1/admin/groups', params),
  // 分组管理页完整视图（显式包含账号/容量/用量统计）
  overview: (params: PageReq & { platform?: string; service_tier?: string }) =>
    get<PagedData<GroupOverviewResp>>('/api/v1/admin/groups/overview', params),
  get: (id: number) => get<GroupResp>(`/api/v1/admin/groups/${id}`),
  create: (data: CreateGroupReq) => post<GroupResp>('/api/v1/admin/groups', data),
  update: (id: number, data: UpdateGroupReq) => put<void>(`/api/v1/admin/groups/${id}`, data),
  delete: (id: number) => del<void>(`/api/v1/admin/groups/${id}`),

  // 分组专属倍率（reverse 视角）
  listRateOverrides: (groupId: number) =>
    get<GroupRateOverrideResp[]>(`/api/v1/admin/groups/${groupId}/rate-overrides`),
  setRateOverride: (groupId: number, userId: number, rate: number) =>
    put<GroupRateOverrideResp>(`/api/v1/admin/groups/${groupId}/rate-overrides/${userId}`, { rate }),
  deleteRateOverride: (groupId: number, userId: number) =>
    del<void>(`/api/v1/admin/groups/${groupId}/rate-overrides/${userId}`),
};
