import { get, put, post } from './client';
import type {
  UserResp, UpdateProfileReq, ChangePasswordReq,
  CreateUserReq, UpdateUserReq, AdjustBalanceReq,
  PageReq, PagedData,
} from '../types';

export const usersApi = {
  // 用户接口
  me: () => get<UserResp>('/api/v1/users/me'),
  updateProfile: (data: UpdateProfileReq) => put<void>('/api/v1/users/me', data),
  changePassword: (data: ChangePasswordReq) => post<void>('/api/v1/users/me/password', data),

  // 管理员接口
  list: (params: PageReq & { status?: string; role?: string }) =>
    get<PagedData<UserResp>>('/api/v1/admin/users', params),
  create: (data: CreateUserReq) => post<UserResp>('/api/v1/admin/users', data),
  update: (id: number, data: UpdateUserReq) => put<void>(`/api/v1/admin/users/${id}`, data),
  adjustBalance: (id: number, data: AdjustBalanceReq) =>
    post<void>(`/api/v1/admin/users/${id}/balance`, data),
};
