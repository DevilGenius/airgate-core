import { get, post, put, del } from './client';
import type { APIKeyResp, CreateAPIKeyReq, UpdateAPIKeyReq, PageReq, PagedData } from '../types';

export const apikeysApi = {
  // 用户接口
  list: (params?: PageReq) =>
    get<PagedData<APIKeyResp>>('/api/v1/api-keys', params),
  create: (data: CreateAPIKeyReq) => post<APIKeyResp>('/api/v1/api-keys', data),
  update: (id: number, data: UpdateAPIKeyReq) => put<void>(`/api/v1/api-keys/${id}`, data),
  delete: (id: number) => del<void>(`/api/v1/api-keys/${id}`),

  // 管理员接口
  adminUpdate: (id: number, data: UpdateAPIKeyReq & { group_id?: number }) =>
    put<void>(`/api/v1/admin/api-keys/${id}`, data),
};
