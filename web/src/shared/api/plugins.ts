import { get, post, upload } from './client';
import type {
  PluginResp, MarketplacePluginResp, PageReq, PagedData,
  PluginOAuthStartResp, PluginOAuthExchangeResp,
} from '../types';

export const pluginsApi = {
  list: (params?: PageReq) =>
    get<PagedData<PluginResp>>('/api/v1/admin/plugins', params),
  uninstall: (name: string) => post<void>(`/api/v1/admin/plugins/${name}/uninstall`),
  reload: (name: string) => post<void>(`/api/v1/admin/plugins/${name}/reload`),
  marketplace: (params?: PageReq) =>
    get<PagedData<MarketplacePluginResp>>('/api/v1/admin/marketplace/plugins', params),
  // 上传安装插件
  upload: (file: File, name?: string) => {
    const fd = new FormData();
    fd.append('file', file);
    if (name) fd.append('name', name);
    return upload<void>('/api/v1/admin/plugins/upload', fd);
  },
  // 从 GitHub Release 安装
  installGithub: (repo: string) =>
    post<void>('/api/v1/admin/plugins/install-github', { repo }),
  oauthStart: (name: string) =>
    post<PluginOAuthStartResp>(`/api/v1/admin/plugins/${name}/oauth/start`),
  oauthExchange: (name: string, callbackUrl: string) =>
    post<PluginOAuthExchangeResp>(`/api/v1/admin/plugins/${name}/oauth/exchange`, {
      callback_url: callbackUrl,
    }),
};
