import { StrictMode, Suspense } from 'react';
import { act, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AccountCapacityStore } from './accountRuntimeStores';
import { AccountCapacityChip, AccountCapacityLiveChip, AccountStatusCell } from './accountStatusCapacity';
import type { AccountResp } from '../../../shared/types';

describe('account cognition status', () => {
  it.each([true, false])('shows detected status %s in the second status row', (degraded) => {
    const row: AccountResp = {
      id: 1, name: 'test-account', email: null,
      platform: 'openai', type: 'oauth', credentials: {}, model_policy: {},
      state: 'degraded', state_until: new Date(Date.now() + 60000).toISOString(),
      extra: { cognition_degraded: degraded },
      priority: 0, max_concurrency: 4, scheduling_weight: 100, current_concurrency: 0,
      rate_multiplier: 1, upstream_is_pool: false, group_ids: [],
      created_at: '2026-10-01T00:00:00Z', updated_at: '2026-10-01T00:00:00Z',
    };
    const { container, rerender } = render(<AccountStatusCell row={row} />);
    const tag = screen.getByText(degraded ? '降智' : '正常');
    expect(tag).toHaveAttribute('data-degraded', String(degraded));
    expect(tag.parentElement?.parentElement).toBe(container.firstElementChild?.children[1]);
    expect(container.firstElementChild?.children[0]?.textContent).toContain('降级');
    rerender(<AccountStatusCell row={{ ...row, extra: {} }} />);
    expect(screen.queryByTitle('账号降智检测结果')).not.toBeInTheDocument();
  });
});

describe('account capacity animation lifecycle', () => {
  const cancel = vi.fn();
  const animate = vi.fn(() => ({ cancel }));

  beforeEach(() => {
    cancel.mockClear();
    animate.mockClear();
    Object.defineProperty(HTMLElement.prototype, 'animate', {
      configurable: true,
      value: vi.fn(),
      writable: true,
    });
    vi.spyOn(HTMLElement.prototype, 'animate').mockImplementation(animate as unknown as HTMLElement['animate']);
    vi.spyOn(document, 'hidden', 'get').mockReturnValue(false);
  });

  it('keeps cached list refreshes and the first live snapshot static', () => {
    const store = new AccountCapacityStore();
    const { rerender } = render(<AccountCapacityLiveChip rowId={1} current={4} max={10} store={store} />);
    rerender(<AccountCapacityLiveChip rowId={1} current={2} max={10} store={store} />);
    expect(screen.getByLabelText('2 / 10')).toHaveAttribute('data-animated', 'false');
    expect(animate).not.toHaveBeenCalled();

    act(() => store.setCount(1, 1));
    expect(screen.getByLabelText('1 / 10')).toBeInTheDocument();
    expect(animate).not.toHaveBeenCalled();
    act(() => store.setCount(1, 3));
    expect(screen.getByLabelText('3 / 10')).toBeInTheDocument();
    expect(animate).toHaveBeenCalledTimes(2);
  });

  it('recognizes readiness even when the first snapshot equals the fallback', () => {
    const store = new AccountCapacityStore();
    render(<AccountCapacityLiveChip rowId={1} current={0} max={10} store={store} />);
    act(() => store.setCount(1, 0));
    expect(animate).not.toHaveBeenCalled();
    act(() => store.setCount(1, 1));
    expect(animate).toHaveBeenCalledTimes(2);
  });

  it('cancels pending animations and does not replay them on navigation back', () => {
    const store = new AccountCapacityStore();
    store.setCount(1, 3);
    const { unmount } = render(<AccountCapacityLiveChip rowId={1} current={4} max={10} store={store} />);
    act(() => store.setCount(1, 2));
    expect(animate).toHaveBeenCalledTimes(2);
    unmount();
    expect(cancel).toHaveBeenCalledTimes(2);
    animate.mockClear();

    const nextStore = new AccountCapacityStore();
    render(<AccountCapacityLiveChip rowId={1} current={4} max={10} store={nextStore} />);
    act(() => nextStore.setCount(1, 2));
    expect(screen.getByLabelText('2 / 10')).toBeInTheDocument();
    expect(animate).not.toHaveBeenCalled();
    act(() => nextStore.setCount(1, 1));
    expect(animate).toHaveBeenCalledTimes(2);
  });

  it('resets the baseline when Suspense hides and restores existing content', () => {
    const pending = new Promise<never>(() => {});
    function Content({ suspended, value }: { suspended: boolean; value: number }) {
      if (suspended) throw pending;
      return <AccountCapacityChip current={value} max={10} />;
    }
    const view = (suspended: boolean, value: number) => (
      <Suspense fallback={<span>Loading</span>}>
        <Content suspended={suspended} value={value} />
      </Suspense>
    );
    const { rerender } = render(view(false, 1));
    rerender(view(false, 2));
    expect(animate).toHaveBeenCalledTimes(2);
    rerender(view(true, 2));
    expect(cancel).toHaveBeenCalledTimes(2);
    animate.mockClear();
    rerender(view(false, 3));
    expect(screen.getByLabelText('3 / 10')).toBeInTheDocument();
    expect(animate).not.toHaveBeenCalled();
    rerender(view(false, 4));
    expect(animate).toHaveBeenCalledTimes(2);
  });

  it('does not animate StrictMode mounting or repeated identical values', () => {
    const view = (value: number) => <StrictMode><AccountCapacityChip current={value} max={10} /></StrictMode>;
    const { rerender } = render(view(1));
    rerender(view(1));
    expect(animate).not.toHaveBeenCalled();
    rerender(view(2));
    expect(animate).toHaveBeenCalledTimes(2);
  });
});
