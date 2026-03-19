import { get, post, put } from './client';
import type { GroupResp, CreateGroupReq, UpdateGroupReq, PageReq, PagedData } from '../types';

export const groupsApi = {
  // 用户接口
  listAvailable: (params: PageReq & { platform?: string }) =>
    get<PagedData<GroupResp>>('/api/v1/groups', params),

  // 管理员接口
  list: (params: PageReq & { platform?: string }) =>
    get<PagedData<GroupResp>>('/api/v1/admin/groups', params),
  get: (id: number) => get<GroupResp>(`/api/v1/admin/groups/${id}`),
  create: (data: CreateGroupReq) => post<GroupResp>('/api/v1/admin/groups', data),
  update: (id: number, data: UpdateGroupReq) => put<void>(`/api/v1/admin/groups/${id}`, data),
};
