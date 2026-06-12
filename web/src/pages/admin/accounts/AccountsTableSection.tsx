import { memo, useCallback, useSyncExternalStore } from 'react';
import { EmptyState } from '@heroui/react';
import type { AccountResp } from '../../../shared/types';
import { BulkActionsBar } from './BulkActionsBar';
import {
  ACCOUNT_SELECTION_COLUMN_STYLE,
  AccountSelectionStore,
  AccountTableRow,
  AccountsTableLoadingRow,
  TableSelectionCheckbox,
  columnAlignClass,
  columnWidthStyle,
  type AccountTableColumn,
} from './AccountPageSupport';

const AccountsBulkActionsOverlay = memo(function AccountsBulkActionsOverlay({
  onBulkClearRateLimitMarkers,
  onBulkDelete,
  onBulkDisable,
  onBulkEdit,
  onBulkEnable,
  onBulkRefresh,
  onClearSelection,
  selectionStore,
}: {
  onBulkClearRateLimitMarkers: () => void;
  onBulkDelete: () => void;
  onBulkDisable: () => void;
  onBulkEdit: () => void;
  onBulkEnable: () => void;
  onBulkRefresh: () => void;
  onClearSelection: () => void;
  selectionStore: AccountSelectionStore;
}) {
  const selectedCount = useSyncExternalStore(
    selectionStore.subscribe,
    selectionStore.getSelectedCount,
    selectionStore.getSelectedCount,
  );

  if (selectedCount === 0) return null;

  return (
    <div onClick={(event) => event.stopPropagation()}>
      <BulkActionsBar
        overlay
        selectedCount={selectedCount}
        onClear={onClearSelection}
        onEdit={onBulkEdit}
        onEnable={onBulkEnable}
        onDisable={onBulkDisable}
        onRefreshQuota={onBulkRefresh}
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

  return (
    <div className="inline-flex" onClick={(event) => event.stopPropagation()}>
      <TableSelectionCheckbox
        ariaLabel={selectAllAriaLabel}
        isIndeterminate={someVisibleSelected}
        isSelected={allVisibleSelected}
        onChange={onVisibleRowsSelected}
      />
    </div>
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
  onClearSelection,
  onRowSelected,
  onVisibleRowsSelected,
  rows,
  rowMetaById,
  selectAllAriaLabel,
  selectionStore,
  selectRowAriaLabel,
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
  onClearSelection: () => void;
  onRowSelected: (id: number, isSelected: boolean) => void;
  onVisibleRowsSelected: (isSelected: boolean) => void;
  rows: AccountResp[];
  rowMetaById: ReadonlyMap<number, unknown>;
  selectAllAriaLabel: string;
  selectionStore: AccountSelectionStore;
  selectRowAriaLabel: string;
  tableAriaLabel: string;
  tableEmptyText: string;
  visibleRowIds: number[];
}) {
  return (
    <div className="ag-resource-table ag-accounts-table">
      <div className="ag-resource-table-scroll" data-slot="wrapper">
        <AccountsBulkActionsOverlay
          selectionStore={selectionStore}
          onClearSelection={onClearSelection}
          onBulkEdit={onBulkEdit}
          onBulkEnable={onBulkEnable}
          onBulkDisable={onBulkDisable}
          onBulkRefresh={onBulkRefresh}
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
              {columns.map((column) => (
                <th
                  data-slot="th"
                  id={column.key}
                  key={column.key}
                  scope="col"
                  className={columnAlignClass(column.align)}
                  style={columnWidthStyle(column)}
                >
                  {column.title}
                </th>
              ))}
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
