import { useMemo, useRef, useCallback, type ReactNode, type CSSProperties } from 'react';
import { ChevronLeft, ChevronRight } from 'lucide-react';
import { EmptyState } from './EmptyState';
import { useIsMobile } from '../hooks/useMediaQuery';

export interface Column<T> {
  key: string;
  title: ReactNode;
  render?: (row: T) => ReactNode;
  width?: string;
  fixed?: 'left' | 'right';
  align?: 'left' | 'center' | 'right';
  /** Hide this column in mobile card view */
  hideOnMobile?: boolean;
}

export interface TableProps<T> {
  columns: Column<T>[];
  data: T[];
  loading?: boolean;
  rowKey?: (row: T) => string | number;
  page?: number;
  pageSize?: number;
  total?: number;
  onPageChange?: (page: number) => void;
  pageSizeOptions?: number[];
  onPageSizeChange?: (size: number) => void;
  autoHeight?: boolean;
  /** 是否启用多选 */
  selectable?: boolean;
  /** 当前选中的 rowKey 列表（受控） */
  selectedKeys?: (string | number)[];
  /** 选中项变化回调 */
  onSelectionChange?: (keys: (string | number)[]) => void;
  /** @deprecated No longer needed – kept for backward compatibility */
  separateHeader?: boolean;
}

export function Table<T extends Record<string, any>>({
  columns: userColumns,
  data,
  loading = false,
  rowKey,
  page = 1,
  pageSize = 20,
  total = 0,
  onPageChange,
  pageSizeOptions = [10, 20, 50, 100],
  onPageSizeChange,
  autoHeight = false,
  selectable = false,
  selectedKeys,
  onSelectionChange,
}: TableProps<T>) {
  const totalPages = Math.ceil(total / pageSize);
  const isMobile = useIsMobile();

  // 多选逻辑：所有可见行是否全选 / 部分选
  const pageKeys: (string | number)[] = selectable && rowKey ? data.map(rowKey) : [];
  const selectedSet = new Set<string | number>(selectedKeys ?? []);
  const selectedOnPage = pageKeys.filter((k) => selectedSet.has(k));
  const allSelected = pageKeys.length > 0 && selectedOnPage.length === pageKeys.length;
  const someSelected = selectedOnPage.length > 0 && !allSelected;

  const toggleAll = () => {
    if (!onSelectionChange || !rowKey) return;
    const next = new Set(selectedKeys ?? []);
    if (allSelected) {
      pageKeys.forEach((k) => next.delete(k));
    } else {
      pageKeys.forEach((k) => next.add(k));
    }
    onSelectionChange(Array.from(next));
  };

  const toggleRow = (key: string | number) => {
    if (!onSelectionChange) return;
    const next = new Set(selectedKeys ?? []);
    if (next.has(key)) next.delete(key);
    else next.add(key);
    onSelectionChange(Array.from(next));
  };

  // 生成受控勾选框元素（表头 + 每行）
  const headerCheckbox = (
    <input
      type="checkbox"
      aria-label="select all"
      className="cursor-pointer w-4 h-4"
      style={{ accentColor: 'var(--ag-primary)' }}
      checked={allSelected}
      ref={(el) => { if (el) el.indeterminate = someSelected; }}
      onChange={toggleAll}
      onClick={(e) => e.stopPropagation()}
    />
  );

  const selectionColumn: Column<T> | null = selectable && rowKey
    ? {
        key: '__selection__',
        width: '44px',
        fixed: 'left',
        align: 'center',
        hideOnMobile: true,
        title: headerCheckbox,
        render: (row) => {
          const key = rowKey(row);
          return (
            <input
              type="checkbox"
              aria-label="select row"
              className="cursor-pointer w-4 h-4"
      style={{ accentColor: 'var(--ag-primary)' }}
              checked={selectedSet.has(key)}
              onChange={() => toggleRow(key)}
              onClick={(e) => e.stopPropagation()}
            />
          );
        },
      }
    : null;

  const columns = selectionColumn ? [selectionColumn, ...userColumns] : userColumns;

  // Sync horizontal scroll between fixed header and scrollable body
  const headerRef = useRef<HTMLDivElement>(null);
  const bodyRef = useRef<HTMLDivElement>(null);
  const handleBodyScroll = useCallback(() => {
    if (bodyRef.current && headerRef.current) {
      headerRef.current.scrollLeft = bodyRef.current.scrollLeft;
    }
  }, []);

  // Compute sticky styles for fixed columns
  const fixedStyles = useMemo(() => {
    const styles: Record<string, CSSProperties> = {};
    let leftOffset = 0;
    for (const col of columns) {
      if (col.fixed === 'left') {
        styles[col.key] = { position: 'sticky', left: leftOffset, zIndex: 1, backgroundColor: 'var(--ag-bg-elevated)' };
        leftOffset += parseInt(col.width || '0', 10);
      }
    }
    let rightOffset = 0;
    for (let i = columns.length - 1; i >= 0; i--) {
      const col = columns[i]!;
      if (col.fixed === 'right') {
        styles[col.key] = { position: 'sticky', right: rightOffset, zIndex: 1, backgroundColor: 'var(--ag-bg-elevated)' };
        rightOffset += parseInt(col.width || '0', 10);
      }
    }
    return styles;
  }, [columns]);

  const thBaseClass = 'px-5 py-3 text-xs font-semibold text-text-tertiary uppercase tracking-wider whitespace-nowrap bg-bg-elevated';
  const tdBaseClass = 'px-5 py-3 text-sm text-text-secondary whitespace-nowrap align-middle';

  // --- Pagination (shared between desktop & mobile) ---
  const pagination = onPageChange && (
    <div className="flex items-center justify-between">
      <div className="flex items-center gap-2">
        <span className="text-xs text-text-tertiary font-mono">
          共 {total} 条 · 第 {page}/{totalPages} 页
        </span>
        {onPageSizeChange && (
          <select
            value={pageSize}
            onChange={(e) => { onPageSizeChange(Number(e.target.value)); onPageChange(1); }}
            className="text-xs text-text-secondary bg-transparent border border-glass-border rounded px-1.5 py-0.5 cursor-pointer hover:border-primary transition-colors outline-none"
          >
            {pageSizeOptions.map((s) => (
              <option key={s} value={s}>{s} 条/页</option>
            ))}
          </select>
        )}
      </div>
      <div className="flex items-center gap-1">
        <button
          className="flex items-center justify-center w-8 h-8 rounded-sm text-text-secondary hover:bg-bg-hover disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
          disabled={page <= 1}
          onClick={() => onPageChange(page - 1)}
        >
          <ChevronLeft className="w-4 h-4" />
        </button>
        {generatePageNumbers(page, totalPages).map((p, i) =>
          p === '...' ? (
            <span key={`ellipsis-${i}`} className="w-8 text-center text-text-tertiary text-xs">
              ···
            </span>
          ) : (
            <button
              key={p}
              className={`flex items-center justify-center w-8 h-8 rounded-sm text-xs font-medium transition-all ${
                p === page
                  ? 'bg-primary text-text-inverse shadow-md'
                  : 'text-text-secondary hover:bg-bg-hover'
              }`}
              onClick={() => onPageChange(p as number)}
            >
              {p}
            </button>
          ),
        )}
        <button
          className="flex items-center justify-center w-8 h-8 rounded-sm text-text-secondary hover:bg-bg-hover disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
          disabled={page >= totalPages}
          onClick={() => onPageChange(page + 1)}
        >
          <ChevronRight className="w-4 h-4" />
        </button>
      </div>
    </div>
  );

  // --- Loading ---
  if (loading) {
    if (isMobile) {
      return (
        <div className="space-y-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="border border-glass-border bg-bg-elevated shadow-sm rounded-xl p-4 space-y-3">
              {Array.from({ length: 4 }).map((_, j) => (
                <div key={j} className="flex items-center justify-between">
                  <div className="h-3 ag-shimmer rounded w-16" style={{ animationDelay: `${(i * 4 + j) * 80}ms` }} />
                  <div className="h-3 ag-shimmer rounded w-24" style={{ animationDelay: `${(i * 4 + j) * 80 + 40}ms` }} />
                </div>
              ))}
            </div>
          ))}
        </div>
      );
    }
    return (
      <div className="border border-glass-border bg-bg-elevated shadow-sm rounded-xl overflow-hidden">
        <div className="flex gap-4 px-4 py-3 border-b border-border bg-black/[0.03]">
          {columns.map((col) => (
            <div key={col.key} className="h-4 ag-shimmer rounded w-20" style={{ flex: col.width ? `0 0 ${col.width}` : '1' }} />
          ))}
        </div>
        {Array.from({ length: 5 }).map((_, i) => (
          <div key={i} className="flex gap-4 px-4 py-3.5 border-b border-border-subtle">
            {columns.map((col) => (
              <div key={col.key} className="h-4 ag-shimmer rounded w-24" style={{ flex: col.width ? `0 0 ${col.width}` : '1', animationDelay: `${i * 100}ms` }} />
            ))}
          </div>
        ))}
      </div>
    );
  }

  // --- Empty ---
  if (data.length === 0) {
    return (
      <div className="border border-glass-border bg-bg-elevated shadow-sm rounded-xl">
        <EmptyState />
      </div>
    );
  }

  // --- Mobile card view ---
  if (isMobile) {
    const mobileColumns = columns.filter((col) => !col.hideOnMobile);
    const actionCol = mobileColumns.find((col) => col.key === 'actions');
    const fieldCols = mobileColumns.filter((col) => col.key !== 'actions');
    // First column as card header, rest as detail rows
    const [headerCol, ...detailCols] = fieldCols;

    return (
      <div className="space-y-4">
        <div className="space-y-3">
          {data.map((row, i) => (
            <div
              key={rowKey ? rowKey(row) : i}
              className="border border-glass-border bg-bg-elevated shadow-sm rounded-xl overflow-hidden"
            >
              {/* Card header: first column + actions */}
              {headerCol && (
                <div className="flex items-center justify-between gap-2 px-4 pt-3 pb-2">
                  <div className="text-sm text-text font-medium min-w-0 flex-1">
                    {headerCol.render ? headerCol.render(row) : String(row[headerCol.key] ?? '')}
                  </div>
                  {actionCol && (
                    <div className="flex items-center shrink-0">
                      {actionCol.render ? actionCol.render(row) : null}
                    </div>
                  )}
                </div>
              )}
              {/* Detail rows */}
              {detailCols.length > 0 && (
                <div className="px-4 pb-3 pt-1 space-y-0.5">
                  {detailCols.map((col) => (
                    <div key={col.key} className="flex items-center justify-between py-1 min-h-[28px]">
                      <span className="text-[11px] text-text-tertiary shrink-0 mr-3">{col.title}</span>
                      <div className="text-xs text-text-secondary">
                        {col.render ? col.render(row) : String(row[col.key] ?? '')}
                      </div>
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
        {pagination}
      </div>
    );
  }

  // --- Desktop table view ---
  const colGroup = (
    <colgroup>
      {columns.map((col) => <col key={col.key} style={{ width: col.width }} />)}
    </colgroup>
  );

  const theadRow = (
    <thead className="bg-bg-elevated" style={{ boxShadow: '0 1px 0 var(--ag-border)' }}>
      <tr>
        {columns.map((col) => {
          const align = col.align || 'center';
          const textAlign = align === 'left' ? 'text-left' : align === 'right' ? 'text-right' : 'text-center';
          return (
            <th
              key={col.key}
              className={`${thBaseClass} ${textAlign}`}
              style={fixedStyles[col.key] ? { ...fixedStyles[col.key], zIndex: 2 } : undefined}
            >
              {col.title}
            </th>
          );
        })}
      </tr>
    </thead>
  );

  const tbody = (
    <tbody>
      {data.map((row, i) => (
        <tr
          key={rowKey ? rowKey(row) : i}
          className="border-b border-border-subtle last:border-0 transition-colors hover:bg-bg-hover"
        >
          {columns.map((col) => {
            const align = col.align || 'center';
            const justify = align === 'left' ? 'justify-start' : align === 'right' ? 'justify-end' : 'justify-center';
            return (
              <td key={col.key} className={tdBaseClass} style={fixedStyles[col.key]}>
                <div className={`flex items-center ${justify}`}>
                  {col.render ? col.render(row) : String(row[col.key] ?? '')}
                </div>
              </td>
            );
          })}
        </tr>
      ))}
    </tbody>
  );

  return (
    <div className="space-y-4">
      {autoHeight ? (
        <div className="border border-glass-border bg-bg-elevated shadow-sm rounded-xl overflow-x-auto">
          <table className="w-full min-w-max" style={{ tableLayout: 'fixed' }}>
            {colGroup}
            {theadRow}
            {tbody}
          </table>
        </div>
      ) : (
        <div className="border border-glass-border bg-bg-elevated shadow-sm rounded-xl overflow-hidden flex flex-col" style={{ height: '494px' }}>
          {/* 固定表头 —— 不参与垂直滚动 */}
          <div ref={headerRef} className="shrink-0 overflow-hidden">
            <table className="w-full min-w-max" style={{ tableLayout: 'fixed' }}>
              {colGroup}
              {theadRow}
            </table>
          </div>
          {/* 数据体 —— 独立滚动，滚动条只出现在此区域 */}
          <div ref={bodyRef} className="flex-1 overflow-auto" onScroll={handleBodyScroll}>
            <table className="w-full min-w-max" style={{ tableLayout: 'fixed' }}>
              {colGroup}
              {tbody}
            </table>
          </div>
        </div>
      )}

      {/* 分页 */}
      {pagination}
    </div>
  );
}

/** 生成分页页码 */
function generatePageNumbers(current: number, total: number): (number | string)[] {
  if (total <= 7) return Array.from({ length: total }, (_, i) => i + 1);
  const pages: (number | string)[] = [1];
  if (current > 3) pages.push('...');
  for (let i = Math.max(2, current - 1); i <= Math.min(total - 1, current + 1); i++) {
    pages.push(i);
  }
  if (current < total - 2) pages.push('...');
  pages.push(total);
  return pages;
}
