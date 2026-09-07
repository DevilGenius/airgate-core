import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { describe, expect, it, vi } from 'vitest';
import { TablePaginationFooter } from './TablePaginationFooter';

describe('usage page jump controls', () => {
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
});
