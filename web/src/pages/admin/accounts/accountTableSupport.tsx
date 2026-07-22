import { memo, useCallback, useEffect, useLayoutEffect, useRef, useState, type CSSProperties, type MouseEvent as ReactMouseEvent, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { Eraser, RefreshCw, Trash2 } from 'lucide-react';
import { NativeSwitch } from '../../../shared/components/NativeSwitch';
import type { AccountResp } from '../../../shared/types';
import { AccountSelectionStore } from './accountRuntimeStores';

export interface AccountTableColumn {
  key: string;
  title: ReactNode;
  width?: string;
  mobileWidth?: string;
  maxWidth?: string;
  align?: 'left' | 'center' | 'right';
  sortKey?: string;
  render: (row: AccountResp, rowMeta?: unknown) => ReactNode;
}

export type AccountTableSortDirection = 'asc' | 'desc';

export function TableSelectionCheckbox({
  ariaLabel,
  defaultSelected,
  inputRef,
  isIndeterminate,
  isSelected,
  onChange,
}: {
  ariaLabel: string;
  defaultSelected?: boolean;
  inputRef?: (input: HTMLInputElement | null) => void;
  isIndeterminate?: boolean;
  isSelected?: boolean;
  onChange: (isSelected: boolean) => void;
}) {
  const checkboxRef = useRef<HTMLInputElement>(null);
  const setCheckboxRef = useCallback((input: HTMLInputElement | null) => {
    checkboxRef.current = input;
    inputRef?.(input);
  }, [inputRef]);

  useLayoutEffect(() => {
    const checkbox = checkboxRef.current;
    if (!checkbox) return;
    if (isSelected !== undefined) {
      checkbox.checked = isSelected;
    }
    checkbox.indeterminate = !!isIndeterminate;
  }, [isIndeterminate, isSelected]);

  return (
    <input
      ref={setCheckboxRef}
      type="checkbox"
      aria-label={ariaLabel}
      defaultChecked={defaultSelected ?? isSelected ?? false}
      className="ag-table-selection-checkbox"
      onClick={(event) => event.stopPropagation()}
      onChange={(event) => onChange(event.currentTarget.checked)}
    />
  );
}

export function columnAlignClass(align?: AccountTableColumn['align']) {
  if (align === 'right') return 'text-right';
  if (align === 'left') return 'text-left';
  return 'text-center';
}

export const ACCOUNT_SELECTION_COLUMN_STYLE: CSSProperties = {
  minWidth: 'var(--ag-accounts-selection-column-width)',
  width: 'var(--ag-accounts-selection-column-width)',
};

export function columnWidthStyle(column: AccountTableColumn): CSSProperties | undefined {
  if (!column.width) return undefined;
  const width = column.mobileWidth
    ? `var(--ag-accounts-col-${column.key}-width, ${column.width})`
    : column.width;
  return {
    minWidth: width,
    width,
    maxWidth: column.maxWidth,
  };
}

export const AccountRowSelectionCell = memo(function AccountRowSelectionCell({
  ariaLabel,
  selectionStore,
  rowId,
  onSelectedChange,
}: {
  ariaLabel: string;
  selectionStore: AccountSelectionStore;
  rowId: number;
  onSelectedChange: (id: number, isSelected: boolean) => void;
}) {
  const handleChange = useCallback((nextSelected: boolean) => {
    onSelectedChange(rowId, nextSelected);
  }, [onSelectedChange, rowId]);
  const registerInput = useCallback((input: HTMLInputElement | null) => {
    selectionStore.registerRowInput?.(rowId, input);
  }, [rowId, selectionStore]);

  return (
    <TableSelectionCheckbox
      ariaLabel={ariaLabel}
      defaultSelected={selectionStore.has(rowId)}
      onChange={handleChange}
      inputRef={registerInput}
    />
  );
});

const AccountTableCellContent = memo(function AccountTableCellContent({
  column,
  row,
  rowMeta,
}: {
  column: AccountTableColumn;
  row: AccountResp;
  rowMeta?: unknown;
}) {
  return <>{column.render(row, rowMeta)}</>;
}, (prev, next) => {
  if (prev.column !== next.column) return false;
  return accountTableCellRowsEqual(prev.column.key, prev.row, next.row)
    && accountTableCellMetaEqual(prev.column.key, prev.rowMeta, next.rowMeta);
});

function sameAccountExceptCapacity(left: AccountResp, right: AccountResp) {
  return left.id === right.id
    && left.name === right.name
    && left.email === right.email
    && left.platform === right.platform
    && left.type === right.type
    && left.credentials === right.credentials
    && left.model_policy === right.model_policy
    && left.state === right.state
    && left.state_until === right.state_until
    && left.priority === right.priority
    && left.max_concurrency === right.max_concurrency
    && left.proxy_id === right.proxy_id
    && left.rate_multiplier === right.rate_multiplier
    && left.error_msg === right.error_msg
    && left.upstream_is_pool === right.upstream_is_pool
    && left.extra === right.extra
    && left.last_used_at === right.last_used_at
    && left.group_ids === right.group_ids
    && left.family_cooldowns === right.family_cooldowns
    && left.today_image_count === right.today_image_count
    && left.total_image_count === right.total_image_count
    && left.created_at === right.created_at
    && left.updated_at === right.updated_at;
}

function accountTableCellRowsEqual(columnKey: string, left: AccountResp, right: AccountResp) {
  if (left === right) return true;

  switch (columnKey) {
    case 'name':
      return left.name === right.name
        && left.email === right.email;
    case 'platform':
    case 'actions':
      return sameAccountExceptCapacity(left, right);
    case 'groups':
      return left.id === right.id
        && left.group_ids === right.group_ids;
    case 'capacity':
      return left.current_concurrency === right.current_concurrency
        && left.max_concurrency === right.max_concurrency;
    case 'status':
      return left.state === right.state
        && left.state_until === right.state_until
        && left.error_msg === right.error_msg
        && left.family_cooldowns === right.family_cooldowns;
    case 'scheduling':
      return left.id === right.id
        && left.state === right.state;
    case 'priority':
      return left.priority === right.priority;
    case 'rate_multiplier':
      return left.rate_multiplier === right.rate_multiplier;
    case 'usage_window':
    case 'last_used_at':
      return left.id === right.id;
    default:
      return false;
  }
}

function accountTableCellMetaEqual(columnKey: string, left: unknown, right: unknown) {
  if (left === right) return true;
  switch (columnKey) {
    case 'groups': {
      const leftMeta = left as {
        groupNames?: string[];
        hiddenGroupCount?: number;
        visibleGroups?: string[];
      } | undefined;
      const rightMeta = right as {
        groupNames?: string[];
        hiddenGroupCount?: number;
        visibleGroups?: string[];
      } | undefined;
      if (!leftMeta || !rightMeta) return false;
      return leftMeta.hiddenGroupCount === rightMeta.hiddenGroupCount
        && stringListEqual(leftMeta.groupNames, rightMeta.groupNames)
        && stringListEqual(leftMeta.visibleGroups, rightMeta.visibleGroups);
    }
    case 'usage_window':
      return (left as { usage?: unknown } | undefined)?.usage === (right as { usage?: unknown } | undefined)?.usage;
    case 'last_used_at':
      return Boolean(left && right)
        && (left as { lastUsedRelative?: string }).lastUsedRelative === (right as { lastUsedRelative?: string }).lastUsedRelative
        && (left as { lastUsedTitle?: string }).lastUsedTitle === (right as { lastUsedTitle?: string }).lastUsedTitle;
    default:
      return true;
  }
}

function stringListEqual(left: string[] | undefined, right: string[] | undefined) {
  if (left === right) return true;
  if (!left || !right || left.length !== right.length) return false;
  return left.every((value, index) => value === right[index]);
}

export const AccountSchedulingSwitch = memo(function AccountSchedulingSwitch({
  ariaLabel,
  isSelected,
  rowId,
  onToggle,
}: {
  ariaLabel: string;
  isSelected: boolean;
  rowId: number;
  onToggle: (id: number) => void;
}) {
  const handleClick = useCallback(() => {
    onToggle(rowId);
  }, [onToggle, rowId]);

  return (
    <NativeSwitch
      ariaLabel={ariaLabel}
      isSelected={isSelected}
      onChange={handleClick}
    />
  );
}, (prev, next) => (
  prev.ariaLabel === next.ariaLabel
  && prev.isSelected === next.isSelected
  && prev.rowId === next.rowId
  && prev.onToggle === next.onToggle
));

export const AccountRowActions = memo(function AccountRowActions({
  row,
  labels,
  onEdit,
  onDelete,
  onTest,
  onStats,
  onRefreshToken,
  onClearCooldowns,
}: {
  row: AccountResp;
  labels: {
    actions: string;
    clearCooldowns: string;
    delete: string;
    edit: string;
    editShort: string;
    more: string;
    refreshToken: string;
    stats: string;
    statsShort: string;
    test: string;
    testShort: string;
  };
  onEdit: (row: AccountResp) => void;
  onDelete: (row: AccountResp) => void;
  onTest: (row: AccountResp) => void;
  onStats: (id: number) => void;
  onRefreshToken: (id: number) => void;
  onClearCooldowns: (id: number) => void;
}) {
  return (
    <div className="ag-table-row-actions ag-account-row-actions mx-auto flex w-[124px] items-center justify-center gap-1">
      <button
        type="button"
        aria-label={labels.edit}
        title={labels.edit}
        className="ag-account-row-action-button ag-account-row-action-label"
        onClick={(event) => {
          event.stopPropagation();
          onEdit(row);
        }}
      >
        {labels.editShort}
      </button>
      <button
        type="button"
        aria-label={labels.test}
        title={labels.test}
        className="ag-account-row-action-button ag-account-row-action-label"
        onClick={(event) => {
          event.stopPropagation();
          onTest(row);
        }}
      >
        {labels.testShort}
      </button>
      <button
        type="button"
        aria-label={labels.stats}
        title={labels.stats}
        className="ag-account-row-action-button ag-account-row-action-button--stats ag-account-row-action-label"
        onClick={(event) => {
          event.stopPropagation();
          onStats(row.id);
        }}
      >
        {labels.statsShort}
      </button>
      <AccountRowOverflowMenu
        row={row}
        labels={labels}
        onDelete={onDelete}
        onRefreshToken={onRefreshToken}
        onClearCooldowns={onClearCooldowns}
      />
    </div>
  );
}, (prev, next) => (
  prev.row === next.row
  && prev.labels === next.labels
  && prev.onEdit === next.onEdit
  && prev.onDelete === next.onDelete
  && prev.onTest === next.onTest
  && prev.onStats === next.onStats
  && prev.onRefreshToken === next.onRefreshToken
  && prev.onClearCooldowns === next.onClearCooldowns
));

type AccountRowMenuPosition = {
  bottom?: number;
  right: number;
  top?: number;
};

function getAccountRowMenuPosition(trigger: HTMLElement, itemCount: number): AccountRowMenuPosition {
  const rect = trigger.getBoundingClientRect();
  const gap = 6;
  const edge = 8;
  const estimatedMenuHeight = 8 + itemCount * 34;
  const right = Math.max(edge, window.innerWidth - rect.right);
  const top = rect.bottom + gap;

  if (top + estimatedMenuHeight > window.innerHeight - edge) {
    return {
      bottom: Math.max(edge, window.innerHeight - rect.top + gap),
      right,
    };
  }

  return {
    right,
    top,
  };
}

const AccountRowOverflowMenu = memo(function AccountRowOverflowMenu({
  row,
  labels,
  onDelete,
  onRefreshToken,
  onClearCooldowns,
}: {
  row: AccountResp;
  labels: {
    actions: string;
    clearCooldowns: string;
    delete: string;
    more: string;
    refreshToken: string;
  };
  onDelete: (row: AccountResp) => void;
  onRefreshToken: (id: number) => void;
  onClearCooldowns: (id: number) => void;
}) {
  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const [position, setPosition] = useState<AccountRowMenuPosition | null>(null);
  const isOpen = position !== null;

  const close = useCallback(() => {
    setPosition(null);
  }, []);

  const openFromTrigger = useCallback((trigger: HTMLElement) => {
    const itemCount = row.type === 'oauth' ? 3 : 2;
    setPosition(getAccountRowMenuPosition(trigger, itemCount));
  }, [row.type]);

  const toggleMenu = useCallback((event: ReactMouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    if (isOpen) {
      close();
      return;
    }
    openFromTrigger(event.currentTarget);
  }, [close, isOpen, openFromTrigger]);

  useEffect(() => {
    if (!isOpen) return undefined;

    const handlePointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (!(target instanceof Node)) return;
      if (menuRef.current?.contains(target) || triggerRef.current?.contains(target)) return;
      close();
    };
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') close();
    };

    document.addEventListener('pointerdown', handlePointerDown, true);
    document.addEventListener('keydown', handleKeyDown);
    window.addEventListener('resize', close);
    window.addEventListener('scroll', close, true);
    return () => {
      document.removeEventListener('pointerdown', handlePointerDown, true);
      document.removeEventListener('keydown', handleKeyDown);
      window.removeEventListener('resize', close);
      window.removeEventListener('scroll', close, true);
    };
  }, [close, isOpen]);

  const runAction = useCallback((action: () => void) => (event: ReactMouseEvent<HTMLButtonElement>) => {
    event.stopPropagation();
    close();
    action();
  }, [close]);

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        aria-expanded={isOpen}
        aria-haspopup="menu"
        aria-label={labels.more}
        title={labels.more}
        className="ag-account-row-more-trigger ag-account-row-action-button"
        onClick={toggleMenu}
      />
      {isOpen && position && typeof document !== 'undefined' ? createPortal(
        <div
          ref={menuRef}
          role="menu"
          aria-label={labels.actions}
          className="ag-account-row-menu"
          style={position}
          onClick={(event) => event.stopPropagation()}
        >
          {row.type === 'oauth' ? (
            <button
              type="button"
              role="menuitem"
              className="ag-account-row-menu-item"
              onClick={runAction(() => onRefreshToken(row.id))}
            >
              <RefreshCw className="w-3.5 h-3.5 ag-account-row-menu-icon ag-account-row-menu-icon--success" />
              <span>{labels.refreshToken}</span>
            </button>
          ) : null}
          <button
            type="button"
            role="menuitem"
            className="ag-account-row-menu-item"
            onClick={runAction(() => onClearCooldowns(row.id))}
          >
            <Eraser className="w-3.5 h-3.5 ag-account-row-menu-icon ag-account-row-menu-icon--warning" />
            <span>{labels.clearCooldowns}</span>
          </button>
          <button
            type="button"
            role="menuitem"
            className="ag-account-row-menu-item ag-account-row-menu-item--danger"
            onClick={runAction(() => onDelete(row))}
          >
            <Trash2 className="w-3.5 h-3.5 ag-account-row-menu-icon" />
            <span>{labels.delete}</span>
          </button>
        </div>,
        document.body,
      ) : null}
    </>
  );
}, (prev, next) => (
  prev.row === next.row
  && prev.labels === next.labels
  && prev.onDelete === next.onDelete
  && prev.onRefreshToken === next.onRefreshToken
  && prev.onClearCooldowns === next.onClearCooldowns
));

export const AccountTableRow = memo(function AccountTableRow({
  columns,
  isUsageExpanded,
  row,
  rowMeta,
  selectRowAriaLabel,
  selectionStore,
  onSelectedChange,
}: {
  columns: AccountTableColumn[];
  isUsageExpanded: boolean;
  row: AccountResp;
  rowMeta?: unknown;
  selectRowAriaLabel: string;
  selectionStore: AccountSelectionStore;
  onSelectedChange: (id: number, isSelected: boolean) => void;
}) {
  return (
    <tr data-slot="tr" data-key={row.id} data-usage-expanded={isUsageExpanded ? 'true' : undefined}>
      <td data-slot="td" className="text-center" style={ACCOUNT_SELECTION_COLUMN_STYLE}>
        <AccountRowSelectionCell
          ariaLabel={selectRowAriaLabel}
          rowId={row.id}
          selectionStore={selectionStore}
          onSelectedChange={onSelectedChange}
        />
      </td>
      {columns.map((column) => (
        <td
          data-slot="td"
          key={column.key}
          className={columnAlignClass(column.align)}
          style={columnWidthStyle(column)}
        >
          <AccountTableCellContent column={column} row={row} rowMeta={rowMeta} />
        </td>
      ))}
    </tr>
  );
}, (prev, next) => (
  prev.columns === next.columns
  && prev.isUsageExpanded === next.isUsageExpanded
  && prev.row === next.row
  && prev.rowMeta === next.rowMeta
  && prev.selectRowAriaLabel === next.selectRowAriaLabel
  && prev.selectionStore === next.selectionStore
  && prev.onSelectedChange === next.onSelectedChange
));

export function AccountsTableLoadingRow({ colSpan, minHeight = 220 }: { colSpan: number; minHeight?: number }) {
  return (
    <tr data-slot="tr" data-key="loading">
      <td data-slot="td" colSpan={colSpan}>
        <div aria-busy="true" aria-live="polite" className="w-full" style={{ minHeight }}>
          <span className="sr-only">Loading</span>
        </div>
      </td>
    </tr>
  );
}
