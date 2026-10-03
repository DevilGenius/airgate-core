import { memo, useLayoutEffect, useRef, useState, type CSSProperties, type ReactNode } from 'react';
import styles from './CompactDataTable.module.css';

type RowKey = string | number;

export interface CompactDataTableColumn<T> {
  align?: 'start' | 'center' | 'end';
  key: string;
  render: (row: T, index: number) => ReactNode;
  title: ReactNode;
  width?: CSSProperties['width'];
}

interface CompactDataTableProps<T> {
  ariaLabel: string;
  className?: string;
  columns: CompactDataTableColumn<T>[];
  emptyText: ReactNode;
  minWidth?: CSSProperties['minWidth'];
  rowKey: (row: T, index: number) => RowKey;
  rows: T[];
  virtualize?: boolean;
}

function cx(...classes: Array<string | false | null | undefined>) {
  return classes.filter(Boolean).join(' ');
}

function alignTextClass(align?: 'start' | 'center' | 'end') {
  return align === 'end' ? 'text-right' : align === 'center' ? 'text-center' : undefined;
}

function alignCellClass(align?: 'start' | 'center' | 'end') {
  return align === 'end'
    ? 'justify-end text-right'
    : align === 'center'
      ? 'justify-center text-center'
      : 'justify-start text-left';
}

function CompactDataTableComponent<T>({
  ariaLabel,
  className,
  columns,
  emptyText,
  minWidth,
  rowKey,
  rows,
  virtualize = false,
}: CompactDataTableProps<T>) {
  const wrapperRef = useRef<HTMLDivElement>(null);
  const headerRef = useRef<HTMLTableSectionElement>(null);
  const rowRef = useRef<HTMLTableRowElement>(null);
  const [viewport, setViewport] = useState({ height: 256, rowHeight: 16, headerHeight: 18, scrollTop: 0 });
  const windowed = virtualize && rows.length > 50;
  const visibleCount = Math.ceil(viewport.height / viewport.rowHeight);
  const firstVisible = Math.floor(Math.max(0, viewport.scrollTop - viewport.headerHeight) / viewport.rowHeight);
  const start = windowed ? Math.min(Math.max(0, rows.length - visibleCount), Math.max(0, firstVisible - 6)) : 0;
  const end = windowed ? Math.min(rows.length, start + visibleCount + 12) : rows.length;
  const visibleRows = windowed ? rows.slice(start, end) : rows;

  useLayoutEffect(() => {
    if (!windowed) return;
    const wrapper = wrapperRef.current;
    if (!wrapper) return;
    const measure = () => {
      const height = wrapper.clientHeight;
      const rowHeight = rowRef.current?.getBoundingClientRect().height ?? 0;
      const headerHeight = headerRef.current?.getBoundingClientRect().height ?? 0;
      setViewport((current) => {
        const next = {
          height: height || current.height,
          rowHeight: rowHeight || current.rowHeight,
          headerHeight: headerHeight || current.headerHeight,
          scrollTop: wrapper.scrollTop,
        };
        return next.height === current.height && next.rowHeight === current.rowHeight
          && next.headerHeight === current.headerHeight && next.scrollTop === current.scrollTop ? current : next;
      });
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(wrapper);
    if (rowRef.current) observer.observe(rowRef.current);
    if (headerRef.current) observer.observe(headerRef.current);
    return () => observer.disconnect();
  }, [windowed, rows.length]);

  const spacer = (height: number) => height > 0 ? (
    <tr aria-hidden="true" className={styles.spacer}>
      <td colSpan={columns.length} style={{ height }} />
    </tr>
  ) : null;

  return (
    <div className={cx('ag-compact-data-table', className)}>
      <div
        data-slot="wrapper"
        className={windowed ? styles.virtualViewport : undefined}
        tabIndex={windowed ? 0 : undefined}
        ref={wrapperRef}
        onScroll={windowed ? (event) => {
          const scrollTop = event.currentTarget.scrollTop;
          setViewport((current) => current.scrollTop === scrollTop ? current : { ...current, scrollTop });
        } : undefined}
      >
        <table
          aria-label={ariaLabel}
          aria-rowcount={windowed ? rows.length + 1 : undefined}
          className="ag-compact-data-table-content"
          data-slot="table"
          style={minWidth ? { minWidth } : undefined}
        >
          <thead data-slot="thead" ref={headerRef}>
            <tr data-slot="tr">
              {columns.map((column, index) => (
                <th
                  data-row-header={index === 0 || undefined}
                  data-slot="th"
                  id={column.key}
                  key={column.key}
                  scope="col"
                  className={alignTextClass(column.align)}
                  style={column.width ? { width: column.width } : undefined}
                >
                  <span
                    className={cx(
                      'ag-compact-data-table-heading',
                      alignCellClass(column.align),
                    )}
                  >
                    {column.title}
                  </span>
                </th>
              ))}
            </tr>
          </thead>
          <tbody data-slot="tbody">
            {windowed ? spacer(start * viewport.rowHeight) : null}
            {rows.length === 0 ? (
              <tr data-key="empty" data-slot="tr">
                <td colSpan={columns.length} data-slot="td">
                  <div className="ag-compact-data-table-empty">{emptyText}</div>
                </td>
              </tr>
            ) : visibleRows.map((row, index) => {
                const rowIndex = start + index;
                const key = rowKey(row, rowIndex);

                return (
                  <tr data-key={String(key)} data-slot="tr" key={key} ref={index === 0 ? rowRef : undefined} aria-rowindex={windowed ? rowIndex + 2 : undefined}>
                    {columns.map((column) => (
                      <td
                        data-slot="td"
                        key={column.key}
                        className={alignTextClass(column.align)}
                      >
                        <div
                          className={cx(
                            'ag-compact-data-table-cell',
                            alignCellClass(column.align),
                          )}
                        >
                          {column.render(row, rowIndex)}
                        </div>
                      </td>
                    ))}
                  </tr>
                );
              })}
            {windowed ? spacer((rows.length - end) * viewport.rowHeight) : null}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export const CompactDataTable = memo(CompactDataTableComponent) as typeof CompactDataTableComponent;
