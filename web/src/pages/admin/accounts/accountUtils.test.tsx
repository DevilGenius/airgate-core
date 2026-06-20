import { render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import type { AccountFormProps } from '@devilgenius/airgate-theme/plugin';
import type { ComponentType } from 'react';
import {
  clearPluginAccountFormCache,
  createPluginOAuthBridge,
  detectCredentialAccountType,
  filterCredentialsForAccountType,
  getPlatformPluginMap,
  getSchemaAccountTypes,
  getSchemaSelectedAccountType,
  getSchemaVisibleFields,
  usePluginAccountForm,
} from './accountUtils';
import { loadPluginFrontend } from '../../../app/plugin-loader';
import { pluginsApi } from '../../../shared/api/plugins';
import type { CredentialSchemaResp } from '../../../shared/types';

const mockState = vi.hoisted(() => ({
  cacheClearHandler: undefined as ((pluginId?: string) => void) | undefined,
}));

vi.mock('../../../app/plugin-loader', () => ({
  loadPluginFrontend: vi.fn(),
  onPluginFrontendCacheClear: vi.fn((handler: (pluginId?: string) => void) => {
    mockState.cacheClearHandler = handler;
  }),
}));

vi.mock('../../../shared/api/plugins', () => ({
  pluginsApi: {
    list: vi.fn(),
    rpc: vi.fn(),
  },
}));

function HookProbe({ mode = 'create', platform }: { mode?: AccountFormProps['mode']; platform: string }) {
  const { Form, pluginId } = usePluginAccountForm(platform, mode);
  return (
    <div>
      <span data-testid="plugin-id">{pluginId}</span>
      {Form ? <Form accountType="" credentials={{}} mode={mode} onChange={() => {}} /> : <span>No form</span>}
    </div>
  );
}

describe('account credential schema utilities', () => {
  const schema: CredentialSchemaResp = {
    account_types: [
      {
        description: 'API key account',
        fields: [
          { key: 'api_key', label: 'API Key', placeholder: 'sk-...', required: true, type: 'password' },
          { key: 'ignored', label: 'Ignored', placeholder: '', required: false, type: 'text' },
        ],
        key: 'apikey',
        label: 'API Key',
      },
      {
        description: '',
        fields: [
          { key: 'access_token', label: 'Access Token', placeholder: '', required: true, type: 'textarea' },
        ],
        key: 'oauth',
        label: 'OAuth',
      },
    ],
    fields: [{ key: 'fallback', label: 'Fallback', placeholder: '', required: false, type: 'text' }],
  };

  it('detects common credential account types', () => {
    expect(detectCredentialAccountType({ provider: 'sub2api' })).toBe('sub2api');
    expect(detectCredentialAccountType({ api_key: 'sk' })).toBe('apikey');
    expect(detectCredentialAccountType({ access_token: 'token' })).toBe('oauth');
    expect(detectCredentialAccountType({})).toBe('');
  });

  it('selects account type fields and filters credentials', () => {
    expect(getSchemaAccountTypes(schema)).toHaveLength(2);
    expect(getSchemaAccountTypes(undefined)).toEqual([]);
    expect(getSchemaSelectedAccountType(schema, 'oauth')?.label).toBe('OAuth');
    expect(getSchemaSelectedAccountType(schema, 'missing')?.key).toBe('apikey');
    expect(getSchemaSelectedAccountType(undefined, 'missing')).toBeUndefined();
    expect(getSchemaVisibleFields(schema, 'oauth')).toEqual(schema.account_types?.[1]?.fields);
    expect(getSchemaVisibleFields({ fields: schema.fields }, '')).toEqual(schema.fields);
    expect(filterCredentialsForAccountType({ api_key: 'sk', ignored: 'yes', other: 'no' }, schema.account_types?.[0])).toEqual({
      api_key: 'sk',
      ignored: 'yes',
    });
    expect(filterCredentialsForAccountType({ api_key: 'sk' }, undefined)).toEqual({ api_key: 'sk' });
  });
});

describe('plugin account form loading', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    clearPluginAccountFormCache();
  });

  it('caches platform to plugin mapping and supports explicit cache clear', async () => {
    vi.mocked(pluginsApi.list).mockResolvedValue({
      list: [
        { name: 'gateway-openai', platform: 'openai' },
        { name: 'gateway-claude', platform: 'claude' },
      ],
      page: 1,
      page_size: 100,
      total: 2,
    });

    const first = await getPlatformPluginMap();
    const second = await getPlatformPluginMap();

    expect(first.get('openai')).toBe('gateway-openai');
    expect(second.get('claude')).toBe('gateway-claude');
    expect(pluginsApi.list).toHaveBeenCalledTimes(1);

    mockState.cacheClearHandler?.();
    await getPlatformPluginMap();
    expect(pluginsApi.list).toHaveBeenCalledTimes(2);
  });

  it('loads and reuses plugin-provided account forms', async () => {
    vi.mocked(pluginsApi.list).mockResolvedValue({
      list: [{ name: 'gateway-openai', platform: 'openai' }],
      page: 1,
      page_size: 100,
      total: 1,
    });
    const PluginForm: ComponentType<AccountFormProps> = () => <div>Plugin Account Form</div>;
    vi.mocked(loadPluginFrontend).mockResolvedValue({ accountCreate: PluginForm });

    render(<HookProbe platform="openai" />);

    await waitFor(() => expect(screen.getByTestId('plugin-id').textContent).toBe('gateway-openai'));
    expect(await screen.findByText('Plugin Account Form')).toBeTruthy();
    expect(loadPluginFrontend).toHaveBeenCalledWith('gateway-openai');
  });

  it('returns no form when platform is empty or has no plugin', async () => {
    vi.mocked(pluginsApi.list).mockResolvedValue({ list: [], page: 1, page_size: 100, total: 0 });

    const { rerender } = render(<HookProbe platform="" />);
    expect(screen.getByText('No form')).toBeTruthy();

    rerender(<HookProbe platform="unknown" />);
    await waitFor(() => expect(screen.getByText('No form')).toBeTruthy());
    expect(loadPluginFrontend).not.toHaveBeenCalled();
  });
});

describe('plugin OAuth bridge', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('maps plugin RPC responses into SDK OAuth bridge contracts', async () => {
    vi.mocked(pluginsApi.rpc)
      .mockResolvedValueOnce({ authorize_url: 'https://auth.example', state: 'state-1' })
      .mockResolvedValueOnce({ account_name: 'acct', account_type: 'oauth', credentials: { access_token: 'token' } })
      .mockResolvedValueOnce({
        results: [
          { account_name: 'ok', account_type: 'oauth', credentials: { access_token: 'one' }, status: 'ok' },
          { error: 'bad cookie', status: 'failed' },
        ],
      })
      .mockResolvedValueOnce({ account_name: 'refresh', account_type: 'oauth', credentials: { refresh_token: 'rt' } })
      .mockResolvedValueOnce({
        results: [{ account_name: 'rt', account_type: 'oauth', credentials: { refresh_token: 'one' }, status: 'ok' }],
      })
      .mockResolvedValueOnce({ account_name: 'session', account_type: 'oauth', credentials: { session: 's' } })
      .mockResolvedValueOnce({
        results: [{ account_name: 'sess', account_type: 'oauth', credentials: { session: 'one' }, status: 'ok' }],
      });

    const bridge = createPluginOAuthBridge('gateway-openai');

    await expect(bridge?.start()).resolves.toEqual({ authorizeURL: 'https://auth.example', state: 'state-1' });
    await expect(bridge?.exchange('http://callback')).resolves.toEqual({
      accountName: 'acct',
      accountType: 'oauth',
      credentials: { access_token: 'token' },
    });
    await expect(bridge?.batchExchange?.(['cookie'])).resolves.toEqual([
      { accountName: 'ok', accountType: 'oauth', credentials: { access_token: 'one' }, status: 'ok' },
      { accountName: '', accountType: 'oauth', credentials: {}, error: 'bad cookie', status: 'failed' },
    ]);
    await expect(bridge?.importRefresh?.('refresh-token', 'client-id')).resolves.toEqual({
      accountName: 'refresh',
      accountType: 'oauth',
      credentials: { refresh_token: 'rt' },
    });
    await expect(bridge?.batchImportRefresh?.(['refresh-token'], 'client-id')).resolves.toEqual([
      { accountName: 'rt', accountType: 'oauth', credentials: { refresh_token: 'one' }, status: 'ok' },
    ]);
    await expect(bridge?.importSession?.('session-key')).resolves.toEqual({
      accountName: 'session',
      accountType: 'oauth',
      credentials: { session: 's' },
    });
    await expect(bridge?.batchImportSession?.(['session-key'])).resolves.toEqual([
      { accountName: 'sess', accountType: 'oauth', credentials: { session: 'one' }, status: 'ok' },
    ]);

    expect(pluginsApi.rpc).toHaveBeenNthCalledWith(1, 'gateway-openai', 'oauth/start');
    expect(pluginsApi.rpc).toHaveBeenNthCalledWith(2, 'gateway-openai', 'oauth/exchange', { callback_url: 'http://callback' });
    expect(pluginsApi.rpc).toHaveBeenNthCalledWith(3, 'gateway-openai', 'console/batch-cookie-auth', { session_keys: ['cookie'] });
    expect(pluginsApi.rpc).toHaveBeenNthCalledWith(4, 'gateway-openai', 'oauth/import-refresh', { client_id: 'client-id', refresh_token: 'refresh-token' });
    expect(pluginsApi.rpc).toHaveBeenNthCalledWith(5, 'gateway-openai', 'oauth/batch-import-refresh', { client_id: 'client-id', refresh_tokens: ['refresh-token'] });
    expect(pluginsApi.rpc).toHaveBeenNthCalledWith(6, 'gateway-openai', 'oauth/import-session', { session: 'session-key' });
    expect(pluginsApi.rpc).toHaveBeenNthCalledWith(7, 'gateway-openai', 'oauth/batch-import-session', { sessions: ['session-key'] });
    expect(createPluginOAuthBridge('')).toBeUndefined();
  });
});
