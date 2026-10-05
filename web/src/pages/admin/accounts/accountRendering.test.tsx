import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AccountResp } from '../../../shared/types';
import { TablePage } from '../../../shared/components/TablePage';
import { AccountCapacityStore, AccountSelectionStore } from './accountRuntimeStores';
import { AccountTableRow, type AccountTableColumn } from './accountTableSupport';

afterEach(cleanup);

function account(id: number): AccountResp {
  return {
    id, name: `account-${id}`, email: null, platform: 'openai', type: 'oauth',
    credentials: {}, model_policy: {}, state: 'active', priority: 0,
    max_concurrency: 4, scheduling_weight: 100, current_concurrency: 0, rate_multiplier: 1,
    upstream_is_pool: false, group_ids: [], last_used_at: '',
    created_at: '', updated_at: '',
  };
}

describe('credential rendering boundaries', () => {
  it('keeps bulk selection and toolbar updates out of 250 account rows', async () => {
    const rows = Array.from({ length: 250 }, (_, index) => account(index + 1));
    const store = new AccountSelectionStore();
    const renderName = vi.fn((row: AccountResp) => row.name);
    const columns: AccountTableColumn[] = [{ key: 'name', title: 'Name', render: renderName }];
    const onSelectedChange = (id: number, selected: boolean) => { store.setRow(id, selected); };
    const page = (refreshing: boolean) => (
      <TablePage toolbar={<span>{refreshing ? 'Refreshing' : 'Accounts'}</span>} isFetching={refreshing}>
        <table><tbody>{rows.map((row) => (
          <AccountTableRow key={row.id} row={row} columns={columns} isUsageExpanded={false}
            selectRowAriaLabel={`Select ${row.id}`} selectionStore={store} onSelectedChange={onSelectedChange} />
        ))}</tbody></table>
      </TablePage>
    );
    const { rerender } = render(page(false));
    expect(renderName).toHaveBeenCalledTimes(250);
    renderName.mockClear();

    await act(async () => {
      store.setRows(rows.map((row) => row.id), true);
      await new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
    });
    rerender(page(true));
    expect(screen.getAllByRole('checkbox', { checked: true })).toHaveLength(250);
    expect(renderName).not.toHaveBeenCalled();
  });

  it('refreshes only a changed cell when refreshed row objects arrive', () => {
    const store = new AccountSelectionStore();
    const renderName = vi.fn((row: AccountResp) => row.name);
    const renderCapacity = vi.fn((row: AccountResp) => row.current_concurrency);
    const columns: AccountTableColumn[] = [
      { key: 'name', title: 'Name', render: renderName },
      { key: 'capacity', title: 'Capacity', render: renderCapacity },
    ];
    const onSelectedChange = vi.fn();
    const rows = [account(1), account(2)];
    const table = (items: AccountResp[]) => (
      <table><tbody>{items.map((row) => (
        <AccountTableRow key={row.id} row={row} columns={columns} isUsageExpanded={false}
          selectRowAriaLabel={`Select ${row.id}`} selectionStore={store} onSelectedChange={onSelectedChange} />
      ))}</tbody></table>
    );
    const { rerender } = render(table(rows));
    renderName.mockClear();
    renderCapacity.mockClear();
    rerender(table(rows.map((row) => ({ ...row, current_concurrency: row.id === 1 ? 2 : 0 }))));
    expect(renderName).not.toHaveBeenCalled();
    expect(renderCapacity).toHaveBeenCalledTimes(1);
    expect(renderCapacity.mock.calls[0]?.[0].id).toBe(1);
  });

  it('notifies only changed capacity subscribers during live updates', () => {
    const store = new AccountCapacityStore();
    const first = vi.fn();
    const second = vi.fn();
    const unsubscribe = store.subscribe(1, first);
    store.subscribe(2, second);
    store.setMany([[1, 2], [2, 3]]);
    first.mockClear();
    second.mockClear();
    store.setMany([[1, 4], [2, 3]]);
    expect(first).toHaveBeenCalledTimes(1);
    expect(second).not.toHaveBeenCalled();
    unsubscribe();
    store.setCount(1, 0);
    expect(first).toHaveBeenCalledTimes(1);
  });
});
