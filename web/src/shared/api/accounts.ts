import { get, post, put, del } from './client';
import type {
  AccountResp, CreateAccountReq, UpdateAccountReq,
  CredentialSchemaResp, PageReq, PagedData, TestConnectionResp,
} from '../types';

export const accountsApi = {
  list: (params: PageReq & { platform?: string; status?: string }) =>
    get<PagedData<AccountResp>>('/api/v1/admin/accounts', params),
  create: (data: CreateAccountReq) => post<AccountResp>('/api/v1/admin/accounts', data),
  update: (id: number, data: UpdateAccountReq) => put<void>(`/api/v1/admin/accounts/${id}`, data),
  delete: (id: number) => del<void>(`/api/v1/admin/accounts/${id}`),
  test: (id: number) => post<TestConnectionResp>(`/api/v1/admin/accounts/${id}/test`),
  credentialsSchema: (platform: string) =>
    get<CredentialSchemaResp>(`/api/v1/admin/accounts/credentials-schema/${platform}`),
};
