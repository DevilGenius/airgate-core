import { memo, useCallback, useMemo, useSyncExternalStore } from 'react';
import { EmptyState } from '@heroui/react';
import { ArrowDown, ArrowUp, ArrowUpDown } from 'lucide-react';
import type { AccountResp } from '../../../shared/types';
import { useMediaQuery } from '../../../shared/hooks/useMediaQuery';
import { MobileRecordList, type MobileRecordField, type MobileRecordItem } from '../../../shared/components/MobileRecordList';
import { BulkActionsBar } from './BulkActionsBar';
import {
  ACCOUNT_SELECTION_COLUMN_STYLE,
  AccountRowSelectionCell,
  AccountSelectionStore,
  AccountTableRow,
  AccountsTableLoadingRow,
  TableSelectionCheckbox,
  columnAlignClass,
  columnWidthStyle,
  type AccountTableColumn,
  type AccountTableSortDirection,
} from './AccountPageSupport';

// 移动端卡片字段顺序：两列网格依次排列，用量窗口独占整行。
const ACCOUNT_MOBILE_FIELD_KEYS = ['platform', 'groups', 'capacity', 'scheduling', 'priority', 'last_used_at'] as const;

const AccountsBulkActionsOverlay = memo(function AccountsBulkActionsOverlay({
  onBulkClearRateLimitMarkers,
  onBulkDelete,
  onBulkDisable,
  onBulkEdit,
  onBulkEnable,
  onBulkRefresh,
  onBulkTest,
  onVisibleRowsSelected,
  overlay = true,
  selectionStore,
  visibleRowIds,
}: {
  onBulkClearRateLimitMarkers: () => void;
  onBulkDelete: () => void;
  onBulkDisable: () => void;
  onBulkEdit: () => void;
  onBulkEnable: () => void;
  onBulkRefresh: () => void;
  onBulkTest: () => void;
  onVisibleRowsSelected: (isSelected: boolean) => void;
  overlay?: boolean;
  selectionStore: AccountSelectionStore;
  visibleRowIds: number[];
}) {
  const selectedCount = useSyncExternalStore(
    selectionStore.subscribe,
    selectionStore.getSelectedCount,
    selectionStore.getSelectedCount,
  );
  const selectedVisibleCount = useSyncExternalStore(
    useCallback((listener) => selectionStore.subscribe(listener), [selectionStore]),
    useCallback(() => selectionStore.countVisible(visibleRowIds), [selectionStore, visibleRowIds]),
    () => 0,
  );
  const allVisibleSelected = visibleRowIds.length > 0 && selectedVisibleCount === visibleRowIds.length;
  const someVisibleSelected = selectedVisibleCount > 0 && !allVisibleSelected;

  return (
    <div onClick={(event) => event.stopPropagation()}>
      <BulkActionsBar
        overlay={overlay}
        allVisibleSelected={allVisibleSelected}
        isActive={selectedCount > 0}
        selectedCount={selectedCount}
        someVisibleSelected={someVisibleSelected}
        onEdit={onBulkEdit}
        onEnable={onBulkEnable}
        onDisable={onBulkDisable}
        onTest={onBulkTest}
        onRefreshQuota={onBulkRefresh}
        onSelectAllChange={onVisibleRowsSelected}
        onClearRateLimitMarkers={onBulkClearRateLimitMarkers}
        onDelete={onBulkDelete}
      />
    </div>
  );
});

const AccountsSelectAllHeaderCell = memo(function AccountsSelectAllHeaderCell({
  onVisibleRowsSelected,
  selectAllAriaLabel,
  selectionStore,
  visibleRowIds,
}: {
  onVisibleRowsSelected: (isSelected: boolean) => void;
  selectAllAriaLabel: string;
  selectionStore: AccountSelectionStore;
  visibleRowIds: number[];
}) {
  const selectedVisibleCount = useSyncExternalStore(
    useCallback((listener) => selectionStore.subscribe(listener), [selectionStore]),
    useCallback(() => selectionStore.countVisible(visibleRowIds), [selectionStore, visibleRowIds]),
    () => 0,
  );
  const allVisibleSelected = visibleRowIds.length > 0 && selectedVisibleCount === visibleRowIds.length;
  const someVisibleSelected = selectedVisibleCount > 0 && !allVisibleSelected;

  // 表头全选框常驻：批量栏只覆盖其右侧单元格，避免复选框被替换导致视觉漂移。
  return (
    <TableSelectionCheckbox
      ariaLabel={selectAllAriaLabel}
      isIndeterminate={someVisibleSelected}
      isSelected={allVisibleSelected}
      onChange={onVisibleRowsSelected}
    />
  );
});

export const AccountsTableSection = memo(function AccountsTableSection({
  columns,
  expandedUsageRowIds,
  isLoading,
  onBulkClearRateLimitMarkers,
  onBulkDelete,
  onBulkDisable,
  onBulkEdit,
  onBulkEnable,
  onBulkRefresh,
  onBulkTest,
  onRowSelected,
  onSortChange,
  onVisibleRowsSelected,
  rows,
  rowMetaById,
  selectAllAriaLabel,
  selectionStore,
  selectRowAriaLabel,
  sortBy,
  sortDir,
  tableAriaLabel,
  tableEmptyText,
  visibleRowIds,
}: {
  columns: AccountTableColumn[];
  expandedUsageRowIds: ReadonlySet<number>;
  isLoading: boolean;
  onBulkClearRateLimitMarkers: () => void;
  onBulkDelete: () => void;
  onBulkDisable: () => void;
  onBulkEdit: () => void;
  onBulkEnable: () => void;
  onBulkRefresh: () => void;
  onBulkTest: () => void;
  onRowSelected: (id: number, isSelected: boolean) => void;
  onSortChange?: (sortKey: string) => void;
  onVisibleRowsSelected: (isSelected: boolean) => void;
  rows: AccountResp[];
  rowMetaById: ReadonlyMap<number, unknown>;
  selectAllAriaLabel: string;
  selectionStore: AccountSelectionStore;
  selectRowAriaLabel: string;
  sortBy?: string;
  sortDir?: AccountTableSortDirection;
  tableAriaLabel: string;
  tableEmptyText: string;
  visibleRowIds: number[];
}) {
  const isMobileLayoutActive = useMediaQuery('(max-width: 767px)');
  const mobileItems = useMemo<MobileRecordItem[]>(() => {
    if (!isMobileLayoutActive) return [];
    const columnByKey = new Map(columns.map((column) => [column.key, column]));
    const nameColumn = columnByKey.get('name');
    if (!nameColumn) return [];
    const statusColumn = columnByKey.get('status');
    const usageColumn = columnByKey.get('usage_window');
    const actionsColumn = columnByKey.get('actions');
    const fieldColumns = ACCOUNT_MOBILE_FIELD_KEYS
      .map((key) => columnByKey.get(key))
      .filter((column): column is AccountTableColumn => Boolean(column));

    return rows.map((row) => {
      const rowMeta = rowMetaById.get(row.id);
      const fields: MobileRecordField[] = fieldColumns.map((column) => ({
        label: column.title,
        value: column.render(row, rowMeta),
      }));
      if (usageColumn) {
        fields.push({
          className: 'ag-mobile-record-field--wide ag-accounts-mobile-field--usage',
          label: usageColumn.title,
          value: usageColumn.render(row, rowMeta),
        });
      }
      return {
        cardRef: (card: HTMLElement | null) => selectionStore.registerRowCard(row.id, card),
        id: row.id,
        title: (
          <div className="ag-accounts-mobile-title">
            <AccountRowSelectionCell
              ariaLabel={selectRowAriaLabel}
              rowId={row.id}
              selectionStore={selectionStore}
              onSelectedChange={onRowSelected}
            />
            <div className="ag-accounts-mobile-title-text">
              {nameColumn.render(row, rowMeta)}
            </div>
          </div>
        ),
        meta: statusColumn ? statusColumn.render(row, rowMeta) : undefined,
        fields,
        actions: actionsColumn ? actionsColumn.render(row, rowMeta) : undefined,
        onClick: () => {
          if (selectionStore.getSelectedCount() === 0) return;
          onRowSelected(row.id, !selectionStore.has(row.id));
        },
        onLongPress: () => onRowSelected(row.id, !selectionStore.has(row.id)),
      };
    });
  }, [columns, isMobileLayoutActive, onRowSelected, rowMetaById, rows, selectRowAriaLabel, selectionStore]);

  if (isMobileLayoutActive) {
    return (
      <div className="ag-resource-table ag-accounts-table">
        <div className="ag-accounts-table-mobile">
          <AccountsBulkActionsOverlay
            overlay={false}
            selectionStore={selectionStore}
            visibleRowIds={visibleRowIds}
            onBulkEdit={onBulkEdit}
            onBulkEnable={onBulkEnable}
            onBulkDisable={onBulkDisable}
            onBulkRefresh={onBulkRefresh}
            onBulkTest={onBulkTest}
            onVisibleRowsSelected={onVisibleRowsSelected}
            onBulkClearRateLimitMarkers={onBulkClearRateLimitMarkers}
            onBulkDelete={onBulkDelete}
          />
          <MobileRecordList
            emptyTitle={tableEmptyText}
            isLoading={isLoading}
            items={mobileItems}
          />
        </div>
      </div>
    );
  }

  return (
    <div className="ag-resource-table ag-accounts-table">
      <div className="ag-resource-table-scroll ag-accounts-table-desktop" data-slot="wrapper">
        <AccountsBulkActionsOverlay
          selectionStore={selectionStore}
          visibleRowIds={visibleRowIds}
          onBulkEdit={onBulkEdit}
          onBulkEnable={onBulkEnable}
          onBulkDisable={onBulkDisable}
          onBulkRefresh={onBulkRefresh}
          onBulkTest={onBulkTest}
          onVisibleRowsSelected={onVisibleRowsSelected}
          onBulkClearRateLimitMarkers={onBulkClearRateLimitMarkers}
          onBulkDelete={onBulkDelete}
        />
        <table
          aria-label={tableAriaLabel}
          className="ag-resource-table-content ag-accounts-table-content"
          data-slot="table"
          style={{ minWidth: 'var(--ag-accounts-current-table-width)' }}
        >
          <thead data-slot="thead">
            <tr data-slot="tr">
              <th data-slot="th" scope="col" className="text-center" style={ACCOUNT_SELECTION_COLUMN_STYLE}>
                <AccountsSelectAllHeaderCell
                  selectAllAriaLabel={selectAllAriaLabel}
                  selectionStore={selectionStore}
                  visibleRowIds={visibleRowIds}
                  onVisibleRowsSelected={onVisibleRowsSelected}
                />
              </th>
              {columns.map((column) => {
                const isSorted = Boolean(column.sortKey && column.sortKey === sortBy);
                const ariaSort = column.sortKey
                  ? isSorted && sortDir === 'asc'
                    ? 'ascending'
                    : isSorted
                      ? 'descending'
                      : 'none'
                  : undefined;
                const SortIcon = isSorted
                  ? sortDir === 'asc'
                    ? ArrowUp
                    : ArrowDown
                  : ArrowUpDown;
                return (
                  <th
                    data-slot="th"
                    id={column.key}
                    key={column.key}
                    scope="col"
                    aria-sort={ariaSort}
                    className={columnAlignClass(column.align)}
                    style={columnWidthStyle(column)}
                  >
                    {column.sortKey && onSortChange ? (
                      <button
                        type="button"
                        className="ag-accounts-table-sort-button"
                        data-active={isSorted ? 'true' : undefined}
                        onClick={() => onSortChange(column.sortKey as string)}
                      >
                        <span className="ag-accounts-table-sort-title">{column.title}</span>
                        <SortIcon className="ag-accounts-table-sort-icon" aria-hidden="true" />
                      </button>
                    ) : column.title}
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody data-slot="tbody">
            {isLoading ? (
              <AccountsTableLoadingRow colSpan={columns.length + 1} />
            ) : rows.length === 0 ? (
              <tr data-slot="tr" data-key="empty">
                <td data-slot="td" colSpan={columns.length + 1}>
                  <EmptyState>
                    <div className="text-sm text-default-500">{tableEmptyText}</div>
                  </EmptyState>
                </td>
              </tr>
            ) : (
              rows.map((row) => (
                <AccountTableRow
                  key={row.id}
                  columns={columns}
                  isUsageExpanded={expandedUsageRowIds.has(row.id)}
                  row={row}
                  rowMeta={rowMetaById.get(row.id)}
                  selectRowAriaLabel={selectRowAriaLabel}
                  selectionStore={selectionStore}
                  onSelectedChange={onRowSelected}
                />
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
});
