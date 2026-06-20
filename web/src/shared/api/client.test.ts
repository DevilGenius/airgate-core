import { describe, expect, it, vi, beforeEach } from 'vitest';
import { STORAGE_KEYS } from '../storageKeys';

function jwt(payload: Record<string, unknown>) {
  const encoded = btoa(JSON.stringify(payload))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/g, '');
  return `header.${encoded}.signature`;
}

function jsonResponse(status: number, body: unknown): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: vi.fn().mockResolvedValue(body),
  } as unknown as Response;
}

async function importClient() {
  vi.resetModules();
  return import('./client');
}

describe('api client token storage', () => {
  beforeEach(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
    globalThis.fetch = vi.fn() as unknown as typeof fetch;
  });

  it('stores remembered tokens in both browser storage buckets', async () => {
    const client = await importClient();
    const token = jwt({ api_key_id: 7, exp: Math.floor(Date.now() / 1000) + 3600, role: 'api_key' });

    client.setToken(token, { remember: true });
    client.setSessionAPIKey('sk-session');

    expect(client.getToken()).toBe(token);
    expect(client.getTokenRole()).toBe('api_key');
    expect(client.getTokenAPIKeyID()).toBe(7);
    expect(window.sessionStorage.getItem(STORAGE_KEYS.auth.token)).toBe(token);
    expect(window.localStorage.getItem(STORAGE_KEYS.auth.token)).toBe(token);
    expect(window.localStorage.getItem(STORAGE_KEYS.auth.tokenMode)).toBe('local');
    expect(client.getSessionAPIKey()).toBe('sk-session');

    client.setToken(null);
    client.setSessionAPIKey(null);

    expect(client.getToken()).toBeNull();
    expect(window.sessionStorage.getItem(STORAGE_KEYS.auth.token)).toBeNull();
    expect(window.localStorage.getItem(STORAGE_KEYS.auth.token)).toBeNull();
    expect(client.getSessionAPIKey()).toBeNull();
  });

  it('hydrates remembered local tokens into session storage on module load', async () => {
    const token = jwt({ exp: Math.floor(Date.now() / 1000) + 3600, role: 'admin' });
    window.localStorage.setItem(STORAGE_KEYS.auth.token, token);
    window.localStorage.setItem(STORAGE_KEYS.auth.tokenMode, 'local');

    const client = await importClient();

    expect(client.getToken()).toBe(token);
    expect(window.sessionStorage.getItem(STORAGE_KEYS.auth.token)).toBe(token);
    expect(client.getTokenRole()).toBe('admin');
  });

  it('drops stale local token state when a non-remembered session token exists', async () => {
    const sessionToken = jwt({ role: 'user' });
    window.sessionStorage.setItem(STORAGE_KEYS.auth.token, sessionToken);
    window.localStorage.setItem(STORAGE_KEYS.auth.token, jwt({ role: 'admin' }));
    window.localStorage.setItem(STORAGE_KEYS.auth.tokenMode, 'local');

    const client = await importClient();

    expect(client.getToken()).toBe(sessionToken);
    expect(client.getTokenRole()).toBe('user');
    expect(window.localStorage.getItem(STORAGE_KEYS.auth.token)).toBeNull();
    expect(window.localStorage.getItem(STORAGE_KEYS.auth.tokenMode)).toBeNull();
  });
});

describe('api client requests', () => {
  beforeEach(() => {
    window.localStorage.clear();
    window.sessionStorage.clear();
    globalThis.fetch = vi.fn() as unknown as typeof fetch;
  });

  it('adds auth, json headers, filtered query params and timezone to GET requests', async () => {
    const client = await importClient();
    const token = jwt({ exp: Math.floor(Date.now() / 1000) + 3600, role: 'admin' });
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockResolvedValueOnce(jsonResponse(200, { code: 0, data: { ok: true } }));

    client.setToken(token);
    const data = await client.get<{ ok: boolean }>('/api/v1/example', {
      empty: '',
      nil: null,
      page: 2,
      q: 'airgate',
      undef: undefined,
    });

    expect(data).toEqual({ ok: true });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const [url, init] = fetchMock.mock.calls[0]!;
    const parsed = new URL(String(url));
    expect(parsed.pathname).toBe('/api/v1/example');
    expect(parsed.searchParams.get('page')).toBe('2');
    expect(parsed.searchParams.get('q')).toBe('airgate');
    expect(parsed.searchParams.has('empty')).toBe(false);
    expect(parsed.searchParams.has('nil')).toBe(false);
    expect(parsed.searchParams.has('undef')).toBe(false);
    expect(parsed.searchParams.get('tz')).toBeTruthy();
    expect(init?.headers).toMatchObject({
      Authorization: `Bearer ${token}`,
      'Content-Type': 'application/json',
    });
  });

  it('refreshes expiring tokens before the protected request', async () => {
    const client = await importClient();
    const oldToken = jwt({ exp: Math.floor(Date.now() / 1000) + 5, role: 'admin' });
    const newToken = jwt({ exp: Math.floor(Date.now() / 1000) + 3600, role: 'user' });
    const fetchMock = vi.mocked(fetch);
    fetchMock
      .mockResolvedValueOnce(jsonResponse(200, { code: 0, data: { token: newToken } }))
      .mockResolvedValueOnce(jsonResponse(200, { code: 0, data: { saved: true } }));

    client.setToken(oldToken, { remember: true });
    const data = await client.post<{ saved: boolean }>('/api/v1/example', { name: 'core' });

    expect(data).toEqual({ saved: true });
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(String(fetchMock.mock.calls[0]![0])).toContain('/api/v1/auth/refresh');
    expect(fetchMock.mock.calls[1]![1]?.headers).toMatchObject({
      Authorization: `Bearer ${newToken}`,
    });
    expect(fetchMock.mock.calls[1]![1]?.body).toBe(JSON.stringify({ name: 'core' }));
    expect(client.getTokenRole()).toBe('user');
  });

  it('clears auth and broadcasts once when a 401 response cannot be refreshed', async () => {
    const client = await importClient();
    const token = jwt({ exp: Math.floor(Date.now() / 1000) + 3600, role: 'admin' });
    const fetchMock = vi.mocked(fetch);
    const listener = vi.fn();
    fetchMock
      .mockResolvedValueOnce(jsonResponse(401, { code: 401, message: 'expired' }))
      .mockResolvedValueOnce(jsonResponse(500, { code: -1, message: 'refresh failed' }));

    client.setToken(token);
    const off = client.onAuthExpired(listener);

    await expect(client.get('/api/v1/secret')).rejects.toMatchObject({
      code: 401,
      httpStatus: 401,
      message: 'expired',
    });

    expect(client.getToken()).toBeNull();
    expect(listener).toHaveBeenCalledTimes(1);
    off();
  });

  it('converts network errors but preserves abort errors', async () => {
    const client = await importClient();
    const fetchMock = vi.mocked(fetch);
    fetchMock.mockRejectedValueOnce(new Error('offline'));

    await expect(client.get('/api/v1/offline')).rejects.toMatchObject({
      code: -1,
      httpStatus: 0,
    });

    const abort = new DOMException('stopped', 'AbortError');
    fetchMock.mockRejectedValueOnce(abort);

    await expect(client.get('/api/v1/cancelled')).rejects.toBe(abort);
  });
});
