import { StrictMode, type ReactNode } from 'react';
import { cleanup, fireEvent, render, renderHook, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { useBackgroundTestNotification } from './useBackgroundTestNotification';

const mocks = vi.hoisted(() => ({ toast: vi.fn(), close: vi.fn(), t: (key: string) => key }));
vi.mock('react-i18next', () => ({ useTranslation: () => ({ t: mocks.t }) }));
vi.mock('../../../shared/ui/feedback/Toast', () => ({
  useToast: () => ({ toast: mocks.toast }),
  toastQueue: { close: mocks.close },
}));

afterEach(() => { cleanup(); vi.clearAllMocks(); });

it('replaces background progress with a clickable completion notice and clears it on restore', () => {
  mocks.toast.mockReturnValueOnce('running').mockReturnValueOnce('finished');
  const onRestore = vi.fn();
  const initialProps = { open: true, running: true, finished: false, title: 'Account A', type: 'success' as const, onRestore };
  const { rerender, unmount } = renderHook(useBackgroundTestNotification, { initialProps });
  expect(mocks.toast).not.toHaveBeenCalled();

  rerender({ ...initialProps, open: false });
  expect(mocks.toast).toHaveBeenLastCalledWith('info', expect.anything(), 'Account A', { timeout: 0 });
  rerender({ ...initialProps, open: false, running: false, finished: true });
  expect(mocks.close).toHaveBeenCalledWith('running');
  expect(mocks.toast).toHaveBeenLastCalledWith('success', expect.anything(), 'Account A', { timeout: 0 });
  render(mocks.toast.mock.calls[1]![1] as ReactNode);
  fireEvent.click(screen.getByRole('button', { name: 'accounts.test_background_finished' }));
  expect(onRestore).toHaveBeenCalledOnce();

  rerender({ ...initialProps, running: false, finished: true });
  expect(mocks.close).toHaveBeenCalledWith('finished');
  unmount();
});

it.each(['error', 'cognition'] as const)('cleans up background notices on unmount and preserves %s results', (type) => {
  mocks.toast.mockReturnValue('failed');
  const { unmount } = renderHook(() => useBackgroundTestNotification({
    open: false, running: false, finished: true, title: 'Account B', type, onRestore: vi.fn(),
  }));
  expect(mocks.toast).toHaveBeenCalledWith(type, expect.anything(), 'Account B', { timeout: 0 });
  unmount();
  expect(mocks.close).toHaveBeenCalledWith('failed');
});

it.each([true, false])('notifies on unmount of a running test (open=%s) with an auto-dismiss timeout', async (open) => {
  mocks.toast.mockReturnValue('progress');
  const { unmount } = renderHook(() => useBackgroundTestNotification({
    open, running: true, finished: false, title: 'Running test', type: 'info', onRestore: vi.fn(),
  }));
  unmount();
  await waitFor(() => expect(mocks.toast).toHaveBeenLastCalledWith(
    'info', 'accounts.test_cancelled_on_close', 'Running test', { timeout: 4000 },
  ));
  if (!open) expect(mocks.close).toHaveBeenCalledWith('progress');
});

it('does not report cancellation after completion or on StrictMode effect replay', async () => {
  const initialProps = { open: true, running: true, finished: false, title: 'Test', type: 'success' as const, onRestore: vi.fn() };
  const { rerender, unmount } = renderHook(useBackgroundTestNotification, {
    initialProps,
    wrapper: ({ children }) => <StrictMode>{children}</StrictMode>,
  });
  await Promise.resolve();
  expect(mocks.toast).not.toHaveBeenCalled();
  rerender({ ...initialProps, running: false, finished: true });
  unmount();
  await Promise.resolve();
  expect(mocks.toast).not.toHaveBeenCalled();
});
