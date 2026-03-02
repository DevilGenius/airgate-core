import { get, post, put } from './client';
import type { GroupResp, CreateGroupReq, UpdateGroupReq, PageReq, PagedData } from '../types';

export const groupsApi = {
  list: (params: PageReq & { platform?: string }) =>
    get<PagedData<GroupResp>>('/api/v1/admin/groups', params),
  get: (id: number) => get<GroupResp>(`/api/v1/admin/groups/${id}`),
  create: (data: CreateGroupReq) => post<GroupResp>('/api/v1/admin/groups', data),
  update: (id: number, data: UpdateGroupReq) => put<void>(`/api/v1/admin/groups/${id}`, data),
};
