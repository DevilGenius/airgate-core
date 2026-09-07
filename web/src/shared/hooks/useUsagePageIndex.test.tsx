import { act, renderHook, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { PropsWithChildren } from 'react';
import { describe, expect, it, vi } from 'vitest';
import { useUsagePageIndex } from './useUsagePageIndex';

const api = vi.hoisted(() => ({ pagination: vi.fn(), adminPagination: vi.fn() }));
vi.mock('../api/usage', () => ({ usageApi: api }));

describe('usage page metadata refresh', () => {
  it('waits for a new view through cooldown and stops forcing once a build is acknowledged', async () => {
    const old = { status: 'ready', total: 1000, snapshot: 'old' };
    api.adminPagination.mockResolvedValueOnce(old)
      .mockResolvedValueOnce(old)
      .mockResolvedValueOnce({ ...old, refreshing: true })
      .mockResolvedValue({ status: 'ready', total: 1001, snapshot: 'new' });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const wrapper = ({ children }: PropsWithChildren) => <QueryClientProvider client={client}>{children}</QueryClientProvider>;
    const { result, unmount } = renderHook(() => useUsagePageIndex('admin', {}, true), { wrapper });
    await waitFor(() => expect(result.current.info?.snapshot).toBe('old'));
    act(() => result.current.refresh());
    await waitFor(() => expect(result.current.info?.refreshing).toBe(true));
    await waitFor(() => expect(result.current.info?.snapshot).toBe('new'), { timeout: 5000 });
    expect(api.adminPagination).toHaveBeenCalledTimes(4);
    expect(api.adminPagination.mock.calls[1]?.[0].refresh_pagination).toBe(true);
    expect(api.adminPagination.mock.calls[2]?.[0].refresh_pagination).toBe(true);
    expect(api.adminPagination.mock.calls[3]?.[0].refresh_pagination).toBeUndefined();
    unmount();
    client.clear();
  });
});
