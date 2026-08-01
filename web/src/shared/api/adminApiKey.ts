import { get, post, del } from './client';

export interface ManagedAPIKeyResp {
  hint: string;
  key?: string; // 明文密钥，仅生成时返回一次
}

export type AdminAPIKeyResp = ManagedAPIKeyResp;

export const adminApiKeyApi = {
  get: () => get<ManagedAPIKeyResp | null>('/api/v1/admin/settings/admin-api-key'),
  generate: () => post<ManagedAPIKeyResp>('/api/v1/admin/settings/admin-api-key'),
  remove: () => del<null>('/api/v1/admin/settings/admin-api-key'),
};

export const credKeyApi = {
  get: () => get<ManagedAPIKeyResp | null>('/api/v1/admin/settings/cred-key'),
  generate: () => post<ManagedAPIKeyResp>('/api/v1/admin/settings/cred-key'),
  remove: () => del<null>('/api/v1/admin/settings/cred-key'),
};
