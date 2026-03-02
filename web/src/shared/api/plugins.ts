import { get, post, put } from './client';
import type {
  PluginResp, PluginConfigReq, InstallPluginReq,
  MarketplacePluginResp, PageReq, PagedData,
} from '../types';

export const pluginsApi = {
  list: (params?: PageReq) =>
    get<PagedData<PluginResp>>('/api/v1/admin/plugins', params),
  install: (data: InstallPluginReq) => post<void>('/api/v1/admin/plugins/install', data),
  uninstall: (id: number) => post<void>(`/api/v1/admin/plugins/${id}/uninstall`),
  enable: (id: number) => post<void>(`/api/v1/admin/plugins/${id}/enable`),
  disable: (id: number) => post<void>(`/api/v1/admin/plugins/${id}/disable`),
  updateConfig: (id: number, data: PluginConfigReq) =>
    put<void>(`/api/v1/admin/plugins/${id}/config`, data),
  marketplace: (params?: PageReq) =>
    get<PagedData<MarketplacePluginResp>>('/api/v1/admin/plugins/marketplace', params),
};
