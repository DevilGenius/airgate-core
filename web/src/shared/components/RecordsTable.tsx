import { memo, useCallback, useEffect, useMemo, useRef, useState, type AnimationEvent, type CSSProperties, type ReactNode } from 'react';
import { Inbox } from 'lucide-react';
import { useMediaQuery } from '../hooks/useMediaQuery';
import { DEFAULT_PAGINATION_PAGE_SIZE_OPTIONS, getTotalPages } from '../utils/pagination';
import { MobileRecordList, type MobileRecordItem } from './MobileRecordList';
import { TableLoadingRow } from './TableLoadingRow';
import { TablePaginationFooter } from './TablePaginationFooter';

const FULL_CELL_CONTENT_COLUMNS = new Set(['cost', 'tokens']);
const LEFT_ALIGNED_CONTENT_COLUMNS = new Set<string>(['model', 'user_agent', 'event', 'subject', 'source', 'locator', 'detail']);
const NEW_ROW_ANIMATION_NAME = 'ag-usage-row-new-enter';
const RECORDS_PAGE_SIZE_OPTIONS = DEFAULT_PAGINATION_PAGE_SIZE_OPTIONS;
const DEFAULT_RECORDS_PAGE_SIZE = RECORDS_PAGE_SIZE_OPTIONS[0];
type RecordsMobileLayout = 'default' | 'usageGrid' | 'usageGridWithUser';
type RecordRow = { id: string | number };

interface RecordColumnConfig<T extends RecordRow = RecordRow> {
  key: string;
  title: ReactNode;
  width?: string;
  hideOnMobile?: boolean;
  render: (row: T) => ReactNode;
}

function cx(...classes: Array<string | false | null | undefined>) {
  return classes.filter(Boolean).join(' ');
}

function parseColumnWidth(width?: string): number {
  const match = width?.match(/^(\d+(?:\.\d+)?)px$/);
  return match ? Number(match[1]) : 128;
}

function ColumnHeader({ children }: { children: ReactNode }) {
  return (
    <span className="flex h-full w-full items-center justify-center gap-2 whitespace-nowrap px-2.5 text-center text-xs font-semibold leading-none">
      {children}
    </span>
  );
}

function getColumnClassName(key: string) {
  return `ag-usage-col-${key.replace(/[^a-zA-Z0-9_-]/g, '-')}`;
}

function useNewRowMarkers<T extends RecordRow>({
  dataVersion,
  enabled,
  paused,
  resetKey,
  rows,
}: {
  dataVersion?: number;
  enabled: boolean;
  paused: boolean;
  resetKey?: string;
  rows: T[];
}) {
  const rowIds = useMemo(() => rows.map((row) => String(row.id)), [rows]);
  const previousRowIdsRef = useRef<Set<string> | null>(null);
  const previousResetKeyRef = useRef<string | undefined>(undefined);
  const [markedRowIds, setMarkedRowIds] = useState<Set<string>>(() => new Set());

  const clearMarkedRowId = useCallback((rowId: string) => {
    setMarkedRowIds((current) => {
      if (!current.has(rowId)) {
        return current;
      }
      const next = new Set(current);
      next.delete(rowId);
      return next;
    });
  }, []);

  useEffect(() => {
    const clearActiveBatch = () => {
      setMarkedRowIds((current) => (current.size === 0 ? current : new Set()));
    };

    if (paused) {
      clearActiveBatch();
      return;
    }

    const currentIds = new Set(rowIds);
    const resetChanged = previousResetKeyRef.current !== resetKey;

    if (resetChanged || !enabled || rowIds.length === 0) {
      previousResetKeyRef.current = resetKey;
      previousRowIdsRef.current = currentIds;
      clearActiveBatch();
      return;
    }

    const previousIds = previousRowIdsRef.current;
    previousResetKeyRef.current = resetKey;
    previousRowIdsRef.current = currentIds;

    if (!previousIds) {
      return;
    }

    const addedIds = rowIds.filter((id) => !previousIds.has(id));
    if (addedIds.length === 0) {
      return;
    }

    setMarkedRowIds(new Set(addedIds));
  }, [dataVersion, enabled, paused, resetKey, rowIds]);

  return { clearMarkedRowId, markedRowIds };
}

const RecordsTableRow = memo(function RecordsTableRow({
  columns,
  isNew,
  onNewAnimationEnd,
  row,
}: {
  columns: RecordColumnConfig[];
  isNew: boolean;
  onNewAnimationEnd: (rowId: string) => void;
  row: RecordRow;
}) {
  const rowId = String(row.id);
  // 动画挂在每个 cell 上，会向上冒泡 N 次（N = 列数）。用 ref 锁住，确保 parent 回调只触发一次。
  const animationEndedRef = useRef(false);
  useEffect(() => {
    if (!isNew) animationEndedRef.current = false;
  }, [isNew]);
  const handleAnimationEnd = (event: AnimationEvent<HTMLTableRowElement>) => {
    if (animationEndedRef.current) return;
    if (event.animationName !== NEW_ROW_ANIMATION_NAME) return;
    animationEndedRef.current = true;
    onNewAnimationEnd(rowId);
  };

  return (
    <tr
      data-key={rowId}
      data-slot="tr"
      className={isNew ? 'ag-usage-table-row--new' : undefined}
      onAnimationEnd={isNew ? handleAnimationEnd : undefined}
    >
      {columns.map((column) => {
        const fullCellContent = FULL_CELL_CONTENT_COLUMNS.has(column.key);
        const leftAlignedContent = LEFT_ALIGNED_CONTENT_COLUMNS.has(column.key);

        return (
          <td
            data-slot="td"
            key={column.key}
            className={cx(
              getColumnClassName(column.key),
              'ag-usage-cell',
              fullCellContent && 'ag-usage-cell--full',
              leftAlignedContent && 'ag-usage-cell--left',
            )}
          >
            {column.render(row)}
          </td>
        );
      })}
    </tr>
  );
});

export function RecordsTable<T extends RecordRow>({
  ariaLabel,
  columns,
  dataVersion,
  emptyDescription,
  emptyTitle,
  highlightNewRows = false,
  highlightResetKey,
  hasMore,
  isLoading,
  footer,
  suppressHighlight = false,
  page,
  pageSize,
  rows,
  setPage,
  setPageSize,
  summaryTotal,
  summaryTotalExact,
  total,
  totalExact,
  mobileLayout = 'default',
}: {
  ariaLabel: string;
  columns: RecordColumnConfig<T>[];
  dataVersion?: number;
  emptyDescription?: string;
  emptyTitle: string;
  footer?: ReactNode | false;
  highlightNewRows?: boolean;
  highlightResetKey?: string;
  hasMore?: boolean;
  isLoading: boolean;
  suppressHighlight?: boolean;
  page: number;
  pageSize: number;
  rows: T[];
  setPage: (page: number) => void;
  setPageSize: (pageSize: number) => void;
  summaryTotal?: number;
  summaryTotalExact?: boolean;
  total: number;
  totalExact?: boolean;
  mobileLayout?: RecordsMobileLayout;
}) {
  const isMobileLayoutActive = useMediaQuery('(max-width: 767px)');
  const totalPages = getTotalPages(total, pageSize);
  useEffect(() => {
    if (!RECORDS_PAGE_SIZE_OPTIONS.some((option) => option === pageSize)) {
      setPageSize(DEFAULT_RECORDS_PAGE_SIZE);
    }
  }, [pageSize, setPageSize]);
  const tableMinWidth = useMemo(
    () => Math.max(760, columns.reduce((sum, column) => sum + parseColumnWidth(column.width), 0) + 24),
    [columns],
  );
  const tableMobileWidthDelta = useMemo(
    () => columns.reduce((sum, column) => {
      if (column.key !== 'created_at') return sum;
      return sum + Math.max(0, parseColumnWidth(column.width) - 92);
    }, 0),
    [columns],
  );
  const tableStyle = useMemo(
    () => ({
      minWidth: tableMinWidth,
      '--ag-usage-table-min-width': `${tableMinWidth}px`,
      '--ag-usage-table-mobile-delta': `${tableMobileWidthDelta}px`,
    }) as CSSProperties,
    [tableMinWidth, tableMobileWidthDelta],
  );
  const { clearMarkedRowId, markedRowIds } = useNewRowMarkers({
    dataVersion,
    enabled: highlightNewRows,
    paused: isLoading || suppressHighlight,
    resetKey: highlightResetKey,
    rows,
  });
  const mobileColumns = useMemo(() => {
    if (!isMobileLayoutActive) return [];
    if (mobileLayout === 'usageGrid' || mobileLayout === 'usageGridWithUser') return columns;
    return columns.filter((column) => !column.hideOnMobile);
  }, [columns, isMobileLayoutActive, mobileLayout]);
  // 基础 items 只依赖行数据与列定义；新行高亮合并到下方单独的廉价 memo 中，
  // 避免每次自动刷新（markedRowIds 变化）都重新执行全量 column.render。
  const mobileBaseItems = useMemo<MobileRecordItem[]>(() => {
    if (!isMobileLayoutActive) return [];

    if (mobileLayout === 'usageGrid' || mobileLayout === 'usageGridWithUser') {
      const showUserInHeader = mobileLayout === 'usageGridWithUser';
      const columnByKey = new Map(mobileColumns.map((column) => [column.key, column]));
      const userColumn = columnByKey.get('user_id');
      const timeColumn = columnByKey.get('created_at');
      const fieldColumns = ['api_key', 'model', 'first_event_ms', 'duration_ms', 'tokens', 'cost']
        .map((key) => columnByKey.get(key))
        .filter((column): column is RecordColumnConfig<T> => Boolean(column));
      const hasAPIKeyColumn = Boolean(columnByKey.get('api_key'));
      const hasTokensColumn = Boolean(columnByKey.get('tokens'));

      if (userColumn || timeColumn || fieldColumns.length > 0) {
        return rows.map((row) => ({
          className: 'ag-mobile-record-card--usage-grid',
          id: row.id,
          title: showUserInHeader
            ? userColumn?.render(row) ?? timeColumn?.render(row) ?? '-'
            : timeColumn?.render(row) ?? '-',
          meta: showUserInHeader && userColumn && timeColumn ? timeColumn.render(row) : undefined,
          fields: fieldColumns.map((column) => ({
            className: [
              `ag-mobile-record-field--usage-${column.key}`,
              column.key === 'model' && !hasAPIKeyColumn && 'ag-mobile-record-field--usage-model-primary',
              column.key === 'cost' && !hasTokensColumn && 'ag-mobile-record-field--usage-cost-primary',
              column.key === 'tokens' && 'ag-mobile-record-field--tokens',
            ].filter(Boolean).join(' '),
            label: column.title,
            value: column.render(row),
          })),
        }));
      }
    }

    const primaryColumn = mobileColumns.find((column) => !FULL_CELL_CONTENT_COLUMNS.has(column.key)) ?? mobileColumns[0];
    if (!primaryColumn) return [];
    const fieldColumns = mobileColumns.filter((column) => column !== primaryColumn);
    const compactSummaryCount = fieldColumns.filter((column) => FULL_CELL_CONTENT_COLUMNS.has(column.key)).length;
    const firstCompactSummaryIndex = fieldColumns.findIndex((column) => FULL_CELL_CONTENT_COLUMNS.has(column.key));
    const wideBeforeCompactSummaryColumn = compactSummaryCount > 1
      && firstCompactSummaryIndex > 0
      && firstCompactSummaryIndex % 2 === 1
      ? fieldColumns[firstCompactSummaryIndex - 1]
      : undefined;

    return rows.map((row) => ({
      id: row.id,
      title: primaryColumn.render(row),
      fields: fieldColumns.map((column) => ({
        className: [
          column.key === 'tokens' && 'ag-mobile-record-field--tokens',
          column === wideBeforeCompactSummaryColumn && 'ag-mobile-record-field--wide',
        ].filter(Boolean).join(' ') || undefined,
        label: column.title,
        value: column.render(row),
      })),
    }));
  }, [isMobileLayoutActive, mobileColumns, mobileLayout, rows]);
  const mobileItems = useMemo(() => {
    if (!isMobileLayoutActive || markedRowIds.size === 0) return mobileBaseItems;
    return mobileBaseItems.map((item) => {
      const rowId = String(item.id);
      if (!markedRowIds.has(rowId)) return item;
      return {
        ...item,
        className: cx(item.className, 'ag-mobile-record-card--new'),
        onAnimationEnd: (event: AnimationEvent<HTMLElement>) => {
          if (event.animationName !== NEW_ROW_ANIMATION_NAME) return;
          clearMarkedRowId(rowId);
        },
      };
    });
  }, [clearMarkedRowId, isMobileLayoutActive, markedRowIds, mobileBaseItems]);

  const emptyState = (
    <div className="flex min-h-[220px] w-full flex-col items-center justify-center gap-3 text-center">
      <div className="flex h-11 w-11 items-center justify-center rounded-[var(--field-radius)] bg-default text-muted shadow-sm">
        <Inbox className="h-5 w-5" />
      </div>
      <div className="space-y-1">
        <div className="text-sm font-medium text-text">{emptyTitle}</div>
        {emptyDescription ? (
          <div className="text-xs text-text-tertiary">{emptyDescription}</div>
        ) : null}
      </div>
    </div>
  );

  const paginationFooter = footer === undefined ? (
    <TablePaginationFooter
      page={page}
      pageSize={pageSize}
      pageSizeOptions={RECORDS_PAGE_SIZE_OPTIONS}
      setPage={setPage}
      setPageSize={setPageSize}
      summaryTotal={summaryTotal}
      summaryTotalExact={summaryTotalExact}
      total={total}
      hasMore={hasMore}
      totalExact={totalExact}
      totalPages={totalPages}
    />
  ) : footer;

  return (
    <div className="ag-usage-records-table min-h-[240px]">
      {!isMobileLayoutActive ? (
        <div className="ag-usage-table-scroll ag-usage-table-desktop" data-slot="wrapper">
          <table
            aria-label={ariaLabel}
            className="ag-usage-table"
            data-slot="table"
            style={tableStyle}
          >
            <thead data-slot="thead">
              <tr data-slot="tr">
                {columns.map((column, index) => (
                  <th
                    data-row-header={index === 0 || undefined}
                    data-slot="th"
                    id={column.key}
                    key={column.key}
                    scope="col"
                    className={cx(
                      getColumnClassName(column.key),
                      index === 0 && 'after:hidden',
                    )}
                    style={column.width ? { width: column.width } : undefined}
                  >
                    <ColumnHeader>{column.title}</ColumnHeader>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody data-slot="tbody">
              {isLoading
                ? <TableLoadingRow colSpan={columns.length} />
                : rows.length === 0
                  ? (
                    <tr data-key="empty" data-slot="tr">
                      <td colSpan={columns.length} data-slot="td">
                        {emptyState}
                      </td>
                    </tr>
                  )
                : rows.map((row) => (
                    <RecordsTableRow
                      key={row.id}
                      columns={columns as RecordColumnConfig[]}
                      isNew={markedRowIds.has(String(row.id))}
                      onNewAnimationEnd={clearMarkedRowId}
                      row={row}
                    />
                  ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="ag-usage-table-mobile">
          <MobileRecordList
            emptyDescription={emptyDescription}
            emptyTitle={emptyTitle}
            isLoading={isLoading}
            items={mobileItems}
          />
        </div>
      )}
      {paginationFooter ? (
        <div className="table__footer" data-slot="table-footer">
          {paginationFooter}
        </div>
      ) : null}
    </div>
  );
}
