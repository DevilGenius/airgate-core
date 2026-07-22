import { useState, useEffect, useRef, type ComponentType } from 'react';
import type {
  AccountFormProps,
  PluginOAuthBridge,
  PluginOAuthBatchExchangeResult,
  PluginOAuthExchangeResult,
} from '@devilgenius/airgate-theme/plugin';

/** Session 导入能力尚未沉淀进 SDK；Core 在 bridge 上扩展两个可选字段，
 *  插件 widget 用相同的 intersection 形态拿到这两个 method。 */
type OAuthBridgeWithSession = PluginOAuthBridge & {
  importSession?: (session: string) => Promise<PluginOAuthExchangeResult>;
  batchImportSession?: (sessions: string[]) => Promise<PluginOAuthBatchExchangeResult[]>;
};
import { pluginsApi } from '../../../shared/api/plugins';
import { FETCH_ALL_PARAMS } from '../../../shared/constants';
import { loadPluginFrontend, onPluginFrontendCacheClear } from '../../../app/plugin-loader';
import type {
  AccountExportItem,
  CredentialField,
  AccountTypeResp,
  CredentialSchemaResp,
} from '../../../shared/types';

/** 平台 → 插件名称映射缓存 */
let platformPluginMap: Map<string, string> | null = null;

export async function getPlatformPluginMap(): Promise<Map<string, string>> {
  if (platformPluginMap) return platformPluginMap;
  const resp = await pluginsApi.list(FETCH_ALL_PARAMS);
  const map = new Map<string, string>();
  for (const p of resp.list) {
    if (p.platform) map.set(p.platform, p.name);
  }
  platformPluginMap = map;
  return map;
}

export function detectCredentialAccountType(credentials: Record<string, string>): string {
  if (credentials.provider === 'sub2api') return 'sub2api';
  if (credentials.api_key) return 'apikey';
  if (credentials.access_token) return 'oauth';
  return '';
}

export function normalizeAccountEmailValue(value: string | null | undefined): string | null {
  const normalized = value?.trim().toLowerCase() ?? '';
  return normalized || null;
}

export function syncAccountIdentity(
  credentials: Record<string, string>,
  emailValue: string | null | undefined,
): {
  email: string | null;
  credentials: Record<string, string>;
} {
  const next = { ...credentials };
  const email = normalizeAccountEmailValue(emailValue);
  if (email) next.email = email;
  else delete next.email;
  return { email, credentials: next };
}

export function resolveAccountIdentity(credentials: Record<string, string>): {
  email: string | null;
  credentials: Record<string, string>;
} {
  return syncAccountIdentity(credentials, credentials.email);
}

export function parseAccountImportItems(value: unknown): AccountExportItem[] | null {
  if (Array.isArray(value)) {
    return value.length > 0 ? value as AccountExportItem[] : null;
  }
  if (!value || typeof value !== 'object') return null;
  const envelope = value as { version?: unknown; accounts?: unknown };
  if (envelope.version !== undefined && envelope.version !== 1 && envelope.version !== 2) return null;
  if (!Array.isArray(envelope.accounts) || envelope.accounts.length === 0) return null;
  return envelope.accounts as AccountExportItem[];
}

export function getSchemaAccountTypes(schema?: CredentialSchemaResp): AccountTypeResp[] {
  return schema?.account_types ?? [];
}

export function getSchemaSelectedAccountType(
  schema: CredentialSchemaResp | undefined,
  accountType: string,
): AccountTypeResp | undefined {
  const accountTypes = getSchemaAccountTypes(schema);
  if (!accountTypes.length) return undefined;
  return accountTypes.find((item) => item.key === accountType) ?? accountTypes[0];
}

export function getSchemaVisibleFields(
  schema: CredentialSchemaResp | undefined,
  accountType: string,
): CredentialField[] {
  const selectedType = getSchemaSelectedAccountType(schema, accountType);
  const fields = selectedType?.fields ?? schema?.fields ?? [];
  return fields.filter((field) => field.key !== 'email');
}

export function filterCredentialsForAccountType(
  credentials: Record<string, string>,
  accountType?: AccountTypeResp,
): Record<string, string> {
  if (!accountType) return credentials;

  const allowedKeys = new Set(accountType.fields.map((field) => field.key));
  const next: Record<string, string> = {};
  for (const [key, value] of Object.entries(credentials)) {
    if (allowedKeys.has(key)) {
      next[key] = value;
    }
  }
  return next;
}

const pluginFormCache = new Map<string, ComponentType<AccountFormProps> | null>();

export function clearPluginAccountFormCache(pluginId?: string) {
  platformPluginMap = null;
  if (pluginId) {
    pluginFormCache.delete(pluginId);
    return;
  }
  pluginFormCache.clear();
}

onPluginFrontendCacheClear(clearPluginAccountFormCache);

export function usePluginAccountForm(platform: string, mode: AccountFormProps['mode'] = 'create') {
  const [Form, setForm] = useState<ComponentType<AccountFormProps> | null>(null);
  const [pluginId, setPluginId] = useState('');
  // loaded 表示插件表单探测已结束（有表单、无表单或无插件），
  // 调用方据此避免在插件表单加载完成前误判为「无插件表单」。
  const [loaded, setLoaded] = useState(false);
  const loadedRef = useRef('');

  useEffect(() => {
    if (!platform) {
      setForm(null);
      setPluginId('');
      setLoaded(true);
      loadedRef.current = '';
      return;
    }
    // 跳过重复加载（但 cleanup 时重置，兼容 React 18 Strict Mode double-mount）
    const loadKey = `${platform}:${mode}`;
    if (loadedRef.current === loadKey) return;
    loadedRef.current = loadKey;
    setLoaded(false);
    let cancelled = false;

    getPlatformPluginMap().then((map) => {
      const resolvedPluginId = map.get(platform) ?? '';
      if (cancelled) return;

      setPluginId(resolvedPluginId);

      if (!resolvedPluginId) {
        setForm(null);
        setLoaded(true);
        return;
      }
      const cacheKey = `${resolvedPluginId}:${mode}`;
      if (pluginFormCache.has(cacheKey)) {
        const cachedForm = pluginFormCache.get(cacheKey) ?? null;
        setForm(() => cachedForm);
        setLoaded(true);
        return;
      }
      loadPluginFrontend(resolvedPluginId).then((mod) => {
        if (cancelled) return;
        const form = mode === 'edit'
          ? (mod?.accountEdit ?? null)
          : (mod?.accountCreate ?? null);
        pluginFormCache.set(cacheKey, form);
        setForm(() => form);
        setLoaded(true);
      }).catch(() => {
        if (cancelled) return;
        setLoaded(true);
      });
    }).catch(() => {
      if (cancelled) return;
      setLoaded(true);
    });

    return () => {
      cancelled = true;
      loadedRef.current = ''; // 重置，让 Strict Mode re-mount 时能重新加载
    };
  }, [platform, mode]);

  return { Form, pluginId, loaded };
}

export function createPluginOAuthBridge(pluginId: string): OAuthBridgeWithSession | undefined {
  if (!pluginId) return undefined;

  return {
    start: async () => {
      const result = await pluginsApi.rpc<{ authorize_url: string; state: string }>(
        pluginId, 'oauth/start',
      );
      return {
        authorizeURL: result.authorize_url,
        state: result.state,
      };
    },
    exchange: async (callbackURL: string) => {
      const result = await pluginsApi.rpc<{
        account_type: string; account_name: string; credentials: Record<string, string>;
      }>(pluginId, 'oauth/exchange', { callback_url: callbackURL });
      return {
        accountType: result.account_type,
        accountName: result.account_name,
        credentials: result.credentials,
      };
    },
    batchExchange: async (sessionKeys: string[]) => {
      const resp = await pluginsApi.rpc<{
        results: Array<{
          account_type?: string;
          account_name?: string;
          credentials?: Record<string, string>;
          status: string;
          error?: string;
        }>;
      }>(pluginId, 'console/batch-cookie-auth', { session_keys: sessionKeys });
      return resp.results.map((r) => ({
        accountType: r.account_type ?? 'oauth',
        accountName: r.account_name ?? '',
        credentials: r.credentials ?? {},
        status: (r.status === 'ok' ? 'ok' : 'failed') as 'ok' | 'failed',
        error: r.error,
      }));
    },
    importRefresh: async (refreshToken: string, clientId?: string) => {
      const result = await pluginsApi.rpc<{
        account_type: string; account_name: string; credentials: Record<string, string>;
      }>(pluginId, 'oauth/import-refresh', { refresh_token: refreshToken, client_id: clientId });
      return {
        accountType: result.account_type,
        accountName: result.account_name,
        credentials: result.credentials,
      };
    },
    batchImportRefresh: async (refreshTokens: string[], clientId?: string) => {
      const resp = await pluginsApi.rpc<{
        results: Array<{
          account_type?: string;
          account_name?: string;
          credentials?: Record<string, string>;
          status: string;
          error?: string;
        }>;
      }>(pluginId, 'oauth/batch-import-refresh', { refresh_tokens: refreshTokens, client_id: clientId });
      return resp.results.map((r) => ({
        accountType: r.account_type ?? 'oauth',
        accountName: r.account_name ?? '',
        credentials: r.credentials ?? {},
        status: (r.status === 'ok' ? 'ok' : 'failed') as 'ok' | 'failed',
        error: r.error,
      }));
    },
    // 透传插件的 session 导入能力。当前仅 gateway-openai (v0.2.5+) 实现，
    // 其它插件 RPC 会返回 404，由 widget 自行判定是否暴露 UI。
    // SDK PluginOAuthBridge 暂未声明这两个字段，故下方用 type intersection 接住。
    importSession: async (session: string) => {
      const result = await pluginsApi.rpc<{
        account_type: string; account_name: string; credentials: Record<string, string>;
      }>(pluginId, 'oauth/import-session', { session });
      return {
        accountType: result.account_type,
        accountName: result.account_name,
        credentials: result.credentials,
      };
    },
    batchImportSession: async (sessions: string[]) => {
      const resp = await pluginsApi.rpc<{
        results: Array<{
          account_type?: string;
          account_name?: string;
          credentials?: Record<string, string>;
          status: string;
          error?: string;
        }>;
      }>(pluginId, 'oauth/batch-import-session', { sessions });
      return resp.results.map((r) => ({
        accountType: r.account_type ?? 'oauth',
        accountName: r.account_name ?? '',
        credentials: r.credentials ?? {},
        status: (r.status === 'ok' ? 'ok' : 'failed') as 'ok' | 'failed',
        error: r.error,
      }));
    },
  };
}
