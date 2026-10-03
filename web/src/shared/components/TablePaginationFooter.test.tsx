import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { describe, expect, it, vi } from 'vitest';
import { TablePaginationFooter } from './TablePaginationFooter';
import styles from './TablePaginationFooter.module.css';

function exposePopoverInJSDOM() {
  // JSDOM does not implement the native popover top layer.
  screen.getByRole('menu', { hidden: true }).parentElement?.removeAttribute('popover');
}

describe('usage page jump controls', () => {
  it('reserves the same page-number region across six-digit boundaries and ellipsis changes', () => {
    const setPage = vi.fn();
    const { container, rerender } = render(<TablePaginationFooter page={1} total={2_000_000} totalPages={200000} setPage={setPage} />);
    const sizingContent = container.querySelector(`.${styles.pageSizer}`)!.innerHTML;
    for (const page of [2, 5, 99999, 100000, 199999, 200000]) {
      rerender(<TablePaginationFooter page={page} total={2_000_000} totalPages={200000} setPage={setPage} />);
      const sizer = container.querySelector(`.${styles.pageSizer}`)!;
      expect(sizer.innerHTML).toBe(sizingContent);
      expect(sizer).toHaveAttribute('aria-hidden', 'true');
      expect(sizer.querySelector('button')).toBeNull();
      expect(screen.getByRole('button', { name: `第 ${page} 页` })).toHaveAttribute('aria-current', 'page');
    }
  });

  it('shows the exact page count and submits an arbitrary deep page', async () => {
    const setPage = vi.fn();
    render(<TablePaginationFooter page={1} pageSize={20} total={5_851_096} totalPages={292555} totalExact setPage={setPage} enablePageJump paginationStatus="ready" />);
    expect(screen.getAllByText('292555').length).toBeGreaterThan(0);
    fireEvent.change(screen.getByRole('textbox', { name: '跳转页码' }), { target: { value: '150001' } });
    fireEvent.click(screen.getByRole('button', { name: /^跳转$/ }));
    await waitFor(() => expect(setPage).toHaveBeenCalledWith(150001));
  });

  it('rejects invalid or out-of-range page numbers without issuing a request', () => {
    const setPage = vi.fn();
    render(<TablePaginationFooter page={1} pageSize={20} total={100} totalPages={5} totalExact setPage={setPage} enablePageJump />);
    for (const value of ['0', '-1', '6', '1.5', 'abc', '']) {
      fireEvent.change(screen.getByRole('textbox', { name: '跳转页码' }), { target: { value } });
      fireEvent.click(screen.getByRole('button', { name: /^跳转$/ }));
      expect(screen.getByRole('alert')).toHaveTextContent('1 至 5');
    }
    expect(setPage).not.toHaveBeenCalled();
  });

  it('renders live rows navigation before exact page metadata is available', () => {
    render(<TablePaginationFooter page={1} pageSize={20} total={21} totalPages={2} totalExact={false} hasMore setPage={vi.fn()} paginationStatus="preparing" />);
    expect(screen.getByRole('status')).toHaveTextContent('正在统计总页数');
    expect(screen.queryByRole('textbox', { name: '跳转页码' })).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: '下一页' })).toHaveAttribute('aria-disabled', 'false');
  });

  it('supports keyboard navigation and disables the first and last page boundaries', async () => {
    const user = userEvent.setup();
    const setPage = vi.fn();
    const { rerender } = render(<TablePaginationFooter page={1} total={100} totalPages={5} setPage={setPage} />);
    expect(screen.getByRole('navigation', { name: '分页' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: '上一页' })).toBeDisabled();
    screen.getByRole('button', { name: '下一页' }).focus();
    await user.keyboard('{Enter}');
    await waitFor(() => expect(setPage).toHaveBeenCalledExactlyOnceWith(2));

    rerender(<TablePaginationFooter page={5} total={100} totalPages={5} setPage={setPage} />);
    expect(screen.getByRole('button', { name: '下一页' })).toBeDisabled();
    expect(screen.getByRole('button', { name: '第 5 页' })).toHaveAttribute('aria-current', 'page');
  });

  it('keeps pointer navigation to one request per click', async () => {
    const user = userEvent.setup();
    const setPage = vi.fn();
    render(<TablePaginationFooter page={2} total={100} totalPages={5} setPage={setPage} />);
    await user.click(screen.getByRole('button', { name: '第 4 页' }));
    await waitFor(() => expect(setPage).toHaveBeenCalledExactlyOnceWith(4));
  });

  it('changes page size using the keyboard-accessible select', async () => {
    const user = userEvent.setup();
    const setPageSize = vi.fn();
    render(<TablePaginationFooter page={2} pageSize={20} total={1000} totalPages={50} setPage={vi.fn()} setPageSize={setPageSize} />);
    const trigger = screen.getByRole('button', { name: '每页数量' });
    trigger.focus();
    await user.keyboard('{Enter}');
    expect(trigger).toHaveAttribute('aria-expanded', 'true');
    exposePopoverInJSDOM();
    expect(screen.getByRole('menuitemradio', { name: '20' })).toHaveAttribute('aria-checked', 'true');
    screen.getByRole('menuitemradio', { name: '100' }).focus();
    await user.keyboard('{Enter}');
    await waitFor(() => expect(setPageSize).toHaveBeenCalledExactlyOnceWith(100));
    expect(trigger).toHaveTextContent('100');
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(screen.getByRole('button', { name: '上一页' })).toBeDisabled();
  });

  it('uses the shared dropdown for pointer selection and Escape dismissal', async () => {
    const user = userEvent.setup();
    const setPageSize = vi.fn();
    const { container } = render(<TablePaginationFooter page={1} pageSize={20} total={1000} totalPages={50} setPage={vi.fn()} setPageSize={setPageSize} />);
    expect(container.querySelector('select')).toBeNull();
    const trigger = screen.getByRole('button', { name: '每页数量' });
    expect(trigger).toHaveClass('ag-simple-select-trigger');
    await user.click(trigger);
    await user.keyboard('{Escape}');
    expect(trigger).toHaveAttribute('aria-expanded', 'false');
    expect(setPageSize).not.toHaveBeenCalled();
    await user.click(trigger);
    exposePopoverInJSDOM();
    await user.click(screen.getByRole('menuitemradio', { name: '50' }));
    await waitFor(() => expect(setPageSize).toHaveBeenCalledExactlyOnceWith(50));
    expect(trigger).toHaveTextContent('50');
    await user.click(trigger);
    exposePopoverInJSDOM();
    expect(screen.getByRole('menuitemradio', { name: '50' })).toHaveAttribute('aria-checked', 'true');
  });

  it('associates jump errors with the input and submits valid pages using Enter', async () => {
    const user = userEvent.setup();
    const setPage = vi.fn();
    render(<TablePaginationFooter page={1} total={100} totalPages={5} setPage={setPage} enablePageJump />);
    const input = screen.getByRole('textbox', { name: '跳转页码' });
    await user.clear(input);
    await user.type(input, '999{Enter}');
    expect(input).toHaveAccessibleDescription('请输入 1 至 5 之间的页码');
    expect(input).toHaveAttribute('aria-invalid', 'true');
    await user.clear(input);
    await user.type(input, '3{Enter}');
    await waitFor(() => expect(setPage).toHaveBeenCalledExactlyOnceWith(3));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
