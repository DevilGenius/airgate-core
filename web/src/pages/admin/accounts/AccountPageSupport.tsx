import { memo, startTransition, useCallback, useEffect, useRef, useState, useSyncExternalStore, type CSSProperties, type MouseEvent as ReactMouseEvent, type ReactElement, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import { useTranslation } from 'react-i18next';
import { Eraser, RefreshCw, Trash2 } from 'lucide-react';
import { NativeSwitch } from '../../../shared/components/NativeSwitch';
import type { AccountResp } from '../../../shared/types';

export interface AccountTableColumn {
  key: string;
  title: ReactNode;
  width?: string;
  mobileWidth?: string;
  maxWidth?: string;
  align?: 'left' | 'center' | 'right';
  render: (row: AccountResp, rowMeta?: unknown) => ReactNode;
}

export const UNGROUPED_GROUP_FILTER = '__ungrouped__';
type SelectionListener = () => void;
export type AccountTypeFilterOption = {
  id: string;
  label: string;
  planLabel?: string;
  platformLabel?: string;
};
export type AccountUsageTodayStats = { requests: number; tokens: number; account_cost: number; user_cost: number };
export type AccountUsageCredits = { balance: number; unlimited: boolean };
export type AccountUsageWindow = {
  key?: string;
  label: string;
  display_label?: string;
  slot?: string;
  group?: string;
  sort_order?: number;
  used_percent: number;
  reset_at?: string;
  reset_after_seconds?: number;
  reset_seconds?: number;
};
export type AccountUsageInfo = {
  windows?: AccountUsageWindow[];
  credits?: AccountUsageCredits | null;
  today_stats?: AccountUsageTodayStats | null;
  updated_at?: string;
};
export type AccountUsageData = { accounts?: Record<string, AccountUsageInfo>; refreshing?: boolean };
export type CachedUsageWindow = {
  resetAtMs: number;
  usedPercent: number;
  window: AccountUsageWindow;
};
export type AccountUsageWindowCache = Map<string, CachedUsageWindow>;

type NativeSoftChipTone = 'accent' | 'default' | 'success';

function NativeSoftChip({
  children,
  className,
  tone,
}: {
  children: ReactNode;
  className?: string;
  tone: NativeSoftChipTone;
}) {
  return (
    <span className={`ag-native-soft-chip ${className ?? ''}`} data-tone={tone}>
      <span className="ag-native-soft-chip__label">{children}</span>
    </span>
  );
}

export function renderAccountTypeFilterOption(option: AccountTypeFilterOption, showOAuthLabel = true): ReactNode {
  if (!option.planLabel) return option.label;
  return (
    <span className="inline-flex min-w-0 items-center gap-1.5">
      {option.platformLabel ? <span className="truncate">{option.platformLabel}</span> : null}
      {showOAuthLabel ? <span className="truncate">OAuth</span> : null}
      <NativeSoftChip className="ag-account-type-plan-chip" tone="accent">
        {option.planLabel}
      </NativeSoftChip>
    </span>
  );
}

function getUsageWindowIdentity(window: AccountUsageWindow) {
  const normalized = normalizeUsageWindow(window);
  const group = normalized.group?.trim();
  const slot = normalizeUsageWindowSortToken(normalized.slot);
  if (group || slot) return `${group || 'base'}:${slot || normalized.display_label?.trim() || normalized.label.trim()}`;
  const key = normalized.key?.trim();
  if (key) return key;
  return normalized.label.trim();
}

function getUsageWindowCacheKey(accountId: string, window: AccountUsageWindow) {
  return `${accountId}:${getUsageWindowIdentity(window)}`;
}

function getUsageWindowResetAtMs(window: AccountUsageWindow, now: number) {
  if (window.reset_at) {
    const parsed = Date.parse(window.reset_at);
    if (Number.isFinite(parsed) && parsed > now) return parsed;
  }
  const resetSeconds = Number(window.reset_seconds ?? 0);
  if (resetSeconds > 0) return now + resetSeconds * 1000;
  const resetAfterSeconds = Number(window.reset_after_seconds ?? 0);
  if (resetAfterSeconds > 0) return now + resetAfterSeconds * 1000;
  return 0;
}

function getUsageWindowUsedPercent(value: unknown) {
  if (typeof value === 'number' && Number.isFinite(value)) return value;
  if (typeof value === 'string' && value.trim()) {
    const parsed = Number(value);
    if (Number.isFinite(parsed)) return parsed;
  }
  return undefined;
}

function normalizeUsageWindowSortToken(value?: string) {
  return value?.trim().toLowerCase().replace(/_/g, '-') || '';
}

function inferUsageWindowSlot(window: AccountUsageWindow) {
  const slot = normalizeUsageWindowSortToken(window.slot);
  if (slot) return slot;

  const key = normalizeUsageWindowSortToken(window.key);
  const label = normalizeUsageWindowSortToken(window.label);
  if (key === '5h' || key.includes(':5h') || key.startsWith('5h-') || label.startsWith('5h')) {
    return '5h';
  }
  if (key === '7d' || key.includes(':7d') || key.startsWith('7d-') || label.startsWith('7d')) {
    return '7d';
  }
  if (key === 'monthly' || key.includes('monthly') || label.includes('monthly')) {
    return 'monthly';
  }
  if (key) return key;
  return label.split(/\s+/)[0] || '';
}

function usageWindowGroupSlug(value: string) {
  return value.trim().toLowerCase().replace(/_/g, '-').replace(/\s+/g, '-');
}

function usageWindowLabelSuffix(label: string, slot: string) {
  const parts = label.trim().split(/\s+/);
  if (parts.length <= 1 || normalizeUsageWindowSortToken(parts[0]) !== slot) return '';
  return parts.slice(1).join(' ').trim();
}

function inferUsageWindowGroup(window: AccountUsageWindow, slot: string) {
  const group = window.group?.trim();
  if (group) return group;

  const key = window.key?.trim() || '';
  if (key.startsWith('model:')) {
    const rest = key.slice('model:'.length);
    const prefix = `${slot}:`;
    const suffix = `:${slot}`;
    if (slot && rest.startsWith(prefix)) return `model:${rest.slice(prefix.length)}`;
    if (slot && rest.endsWith(suffix)) return `model:${rest.slice(0, -suffix.length)}`;
    return key.replace(/^model::/, 'model:');
  }

  const suffix = usageWindowLabelSuffix(window.label || '', slot);
  if (suffix) return `model:${usageWindowGroupSlug(suffix)}`;

  const normalizedKey = normalizeUsageWindowSortToken(key);
  if (normalizedKey.startsWith('5h-') || normalizedKey.startsWith('7d-')) {
    const suffixStart = normalizedKey.indexOf('-') + 1;
    const keySuffix = normalizedKey.slice(suffixStart);
    if (keySuffix) return `model:${usageWindowGroupSlug(keySuffix)}`;
  }

  return 'base';
}

function inferUsageWindowDisplayLabel(window: AccountUsageWindow, slot: string) {
  const displayLabel = window.display_label?.trim();
  if (displayLabel) return displayLabel;

  const label = window.label?.trim() || '';
  if (slot === 'monthly' && label.toLowerCase().startsWith('cr ')) {
    return 'Cr';
  }
  return slot || window.key?.trim() || label;
}

function inferUsageWindowKey(window: AccountUsageWindow, group: string, slot: string, label: string) {
  const key = window.key?.trim();
  if (key) return key;
  if (!slot) return label.trim();
  if (!group || group === 'base') return slot;
  return `${group}:${slot}`;
}

function normalizeUsageWindow(window: AccountUsageWindow): AccountUsageWindow {
  const slot = inferUsageWindowSlot(window);
  const group = inferUsageWindowGroup(window, slot);
  const displayLabel = inferUsageWindowDisplayLabel(window, slot);
  const label = window.label?.trim() || displayLabel;
  return {
    ...window,
    key: inferUsageWindowKey(window, group, slot, label),
    label,
    display_label: displayLabel,
    slot,
    group,
  };
}

function usageWindowSlotSortRank(window: AccountUsageWindow) {
  switch (inferUsageWindowSlot(window)) {
    case '5h':
      return 0;
    case '7d':
      return 1;
    case 'monthly':
      return 2;
    default:
      return 3;
  }
}

function usageWindowSortOrder(window: AccountUsageWindow) {
  const value = Number(window.sort_order);
  return Number.isFinite(value) && value !== 0 ? value : Number.MAX_SAFE_INTEGER;
}

function sortUsageWindows(windows: AccountUsageWindow[]) {
  return [...windows].sort((left, right) => {
    const leftOrder = usageWindowSortOrder(left);
    const rightOrder = usageWindowSortOrder(right);
    if (leftOrder !== rightOrder) return leftOrder - rightOrder;

    const leftSlot = usageWindowSlotSortRank(left);
    const rightSlot = usageWindowSlotSortRank(right);
    if (leftSlot !== rightSlot) return leftSlot - rightSlot;

    const leftGroup = inferUsageWindowGroup(left, inferUsageWindowSlot(left));
    const rightGroup = inferUsageWindowGroup(right, inferUsageWindowSlot(right));
    if (leftGroup !== rightGroup) return leftGroup.localeCompare(rightGroup);

    const leftKey = left.key || '';
    const rightKey = right.key || '';
    if (leftKey !== rightKey) return leftKey.localeCompare(rightKey);

    return (left.label || '').localeCompare(right.label || '');
  });
}

function windowWithCachedReset(window: AccountUsageWindow, resetAtMs: number, now: number): AccountUsageWindow {
  if (resetAtMs <= now) {
    return {
      ...window,
      reset_seconds: 0,
    };
  }
  return {
    ...window,
    reset_at: new Date(resetAtMs).toISOString(),
    reset_seconds: Math.max(0, Math.ceil((resetAtMs - now) / 1000)),
  };
}

export function mergeCachedUsageWindows(data: AccountUsageData | undefined, cache: AccountUsageWindowCache): AccountUsageData | undefined {
  if (!data?.accounts) return data;

  const now = Date.now();
  const accounts: Record<string, AccountUsageInfo> = {};
  const liveCacheKeys = new Set<string>();
  const cachedWindowsByAccount = new Map<string, Map<string, CachedUsageWindow>>();

  for (const entry of Array.from(cache.entries())) {
    const accountIdEnd = entry[0].indexOf(':');
    if (accountIdEnd <= 0) continue;
    const accountId = entry[0].slice(0, accountIdEnd);
    const normalizedCached: CachedUsageWindow = {
      ...entry[1],
      window: normalizeUsageWindow(entry[1].window),
    };
    const cacheKey = getUsageWindowCacheKey(accountId, normalizedCached.window);
    if (cacheKey !== entry[0]) {
      cache.delete(entry[0]);
      cache.set(cacheKey, normalizedCached);
    }
    let cachedWindows = cachedWindowsByAccount.get(accountId);
    if (!cachedWindows) {
      cachedWindows = new Map<string, CachedUsageWindow>();
      cachedWindowsByAccount.set(accountId, cachedWindows);
    }
    const existing = cachedWindows.get(cacheKey);
    if (!existing || normalizedCached.resetAtMs > existing.resetAtMs) {
      cachedWindows.set(cacheKey, normalizedCached);
    }
  }

  for (const [accountId, usage] of Object.entries(data.accounts)) {
    const rawWindows = Array.isArray(usage?.windows) ? usage.windows : [];
    const mergedWindows: AccountUsageWindow[] = [];

    for (const rawWindow of rawWindows) {
      const window = normalizeUsageWindow(rawWindow);
      const cacheKey = getUsageWindowCacheKey(accountId, window);
      const resetAtMs = getUsageWindowResetAtMs(window, now);
      const cached = cache.get(cacheKey);
      const usedPercent = getUsageWindowUsedPercent(window.used_percent)
        ?? cached?.usedPercent
        ?? 0;
      const effectiveResetAtMs = resetAtMs > now
        ? resetAtMs
        : cached && cached.resetAtMs > now
          ? cached.resetAtMs
          : 0;
      const windowWithCachedUsage = {
        ...window,
        used_percent: usedPercent,
      };
      const nextWindow = effectiveResetAtMs > now
        ? windowWithCachedReset(windowWithCachedUsage, effectiveResetAtMs, now)
        : {
            ...windowWithCachedUsage,
            reset_seconds: Number(window.reset_seconds ?? 0),
          };

      cache.set(cacheKey, {
        resetAtMs: effectiveResetAtMs > now ? effectiveResetAtMs : 0,
        usedPercent,
        window: nextWindow,
      });
      liveCacheKeys.add(cacheKey);
      mergedWindows.push(nextWindow);
    }

    for (const [cacheKey, cached] of cachedWindowsByAccount.get(accountId)?.entries() ?? []) {
      if (liveCacheKeys.has(cacheKey)) {
        continue;
      }
      if (cached.resetAtMs <= now) {
        continue;
      }
      const nextWindow = windowWithCachedReset(cached.window, cached.resetAtMs, now);
      cache.set(cacheKey, {
        ...cached,
        window: nextWindow,
      });
      liveCacheKeys.add(cacheKey);
      mergedWindows.push(nextWindow);
    }

    accounts[accountId] = {
      ...usage,
      windows: sortUsageWindows(mergedWindows),
    };
  }

  for (const cacheKey of cache.keys()) {
    if (!liveCacheKeys.has(cacheKey)) {
      cache.delete(cacheKey);
    }
  }

  return {
    ...data,
    accounts,
  };
}

export function runAfterInputFrame(work: () => void) {
  if (typeof window === 'undefined') {
    startTransition(work);
    return;
  }

  window.requestAnimationFrame(() => {
    window.setTimeout(() => startTransition(work), 0);
  });
}

export function useLatestRef<T>(value: T) {
  const ref = useRef(value);
  ref.current = value;
  return ref;
}

export class AccountSelectionStore {
  private selectedIds = new Set<number>();
  private version = 0;
  private listeners = new Set<SelectionListener>();
  private rowListeners = new Map<number, Set<SelectionListener>>();
  private rowInputs = new Map<number, HTMLInputElement>();

  subscribe = (listener: SelectionListener) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };

  subscribeRow = (id: number, listener: SelectionListener) => {
    let listeners = this.rowListeners.get(id);
    if (!listeners) {
      listeners = new Set();
      this.rowListeners.set(id, listeners);
    }
    listeners.add(listener);
    return () => {
      listeners?.delete(listener);
      if (listeners?.size === 0) {
        this.rowListeners.delete(id);
      }
    };
  };

  getSnapshot = () => this.version;

  getSelectedCount = () => this.selectedIds.size;

  registerRowInput(id: number, input: HTMLInputElement | null) {
    if (!input) {
      this.rowInputs.delete(id);
      return;
    }
    this.rowInputs.set(id, input);
    input.checked = this.selectedIds.has(id);
  }

  has(id: number) {
    return this.selectedIds.has(id);
  }

  getSelectedIds() {
    return Array.from(this.selectedIds);
  }

  countVisible(ids: number[]) {
    let count = 0;
    for (const id of ids) {
      if (this.selectedIds.has(id)) count += 1;
    }
    return count;
  }

  setRow(id: number, isSelected: boolean) {
    const alreadySelected = this.selectedIds.has(id);
    if (alreadySelected === isSelected) return;

    if (isSelected) {
      this.selectedIds.add(id);
    } else {
      this.selectedIds.delete(id);
    }
    this.notify([id]);
  }

  setRows(ids: number[], isSelected: boolean) {
    const changedIds: number[] = [];
    for (const id of ids) {
      const alreadySelected = this.selectedIds.has(id);
      if (alreadySelected === isSelected) continue;
      if (isSelected) {
        this.selectedIds.add(id);
      } else {
        this.selectedIds.delete(id);
      }
      changedIds.push(id);
    }
    if (changedIds.length > 0) {
      this.syncRowInputs(changedIds);
      this.notify(changedIds, false);
    }
  }

  clear() {
    if (this.selectedIds.size === 0) return;
    const changedIds = Array.from(this.selectedIds);
    this.selectedIds.clear();
    this.syncRowInputs(changedIds);
    this.notify(changedIds, false);
  }

  private syncRowInputs(changedIds: number[]) {
    for (const id of changedIds) {
      const input = this.rowInputs.get(id);
      if (input) {
        input.checked = this.selectedIds.has(id);
      }
    }
  }

  private notify(changedIds: number[], notifyRows = true) {
    this.version += 1;
    if (notifyRows) {
      for (const id of changedIds) {
        this.rowListeners.get(id)?.forEach((listener) => listener());
      }
    }
    this.listeners.forEach((listener) => listener());
  }
}

export class AccountCapacityStore {
  private counts = new Map<number, number>();
  private listeners = new Map<number, Set<SelectionListener>>();

  subscribe = (id: number, listener: SelectionListener) => {
    let listeners = this.listeners.get(id);
    if (!listeners) {
      listeners = new Set();
      this.listeners.set(id, listeners);
    }
    listeners.add(listener);
    return () => {
      listeners?.delete(listener);
      if (listeners?.size === 0) {
        this.listeners.delete(id);
      }
    };
  };

  getCurrent(id: number, fallback: number) {
    return this.counts.get(id) ?? fallback;
  }

  setCount(id: number, count: number) {
    if (!Number.isFinite(id) || !Number.isFinite(count)) return;
    const normalizedCount = Math.max(0, Math.trunc(count));
    if (this.counts.get(id) === normalizedCount) return;
    this.counts.set(id, normalizedCount);
    this.listeners.get(id)?.forEach((listener) => listener());
  }

  setCounts(nextCounts: Record<string, number>) {
    const changedIds: number[] = [];
    const nextIds = new Set<number>();
    for (const [rawId, count] of Object.entries(nextCounts)) {
      const id = Number(rawId);
      if (!Number.isFinite(id)) continue;
      nextIds.add(id);
      if (this.counts.get(id) === count) continue;
      this.counts.set(id, count);
      changedIds.push(id);
    }
    for (const id of Array.from(this.counts.keys())) {
      if (nextIds.has(id)) continue;
      this.counts.delete(id);
      changedIds.push(id);
    }
    for (const id of changedIds) {
      this.listeners.get(id)?.forEach((listener) => listener());
    }
  }
}

function StatusPill({
  label,
  status,
  tooltip,
}: {
  label: string;
  status: 'active' | 'disabled';
  tooltip?: string;
}) {
  const chip = (
    <NativeSoftChip className="ag-account-status-pill" tone={status === 'active' ? 'success' : 'default'}>
      {label}
    </NativeSoftChip>
  );

  if (!tooltip) return chip;
  return <span className="inline-flex" title={tooltip}>{chip}</span>;
}

export function TableSelectionCheckbox({
  ariaLabel,
  inputRef,
  isIndeterminate,
  isSelected,
  onChange,
}: {
  ariaLabel: string;
  inputRef?: (input: HTMLInputElement | null) => void;
  isIndeterminate?: boolean;
  isSelected: boolean;
  onChange: (isSelected: boolean) => void;
}) {
  const checkboxRef = useRef<HTMLInputElement>(null);
  const setCheckboxRef = useCallback((input: HTMLInputElement | null) => {
    checkboxRef.current = input;
    inputRef?.(input);
  }, [inputRef]);

  useEffect(() => {
    if (checkboxRef.current) {
      checkboxRef.current.indeterminate = !!isIndeterminate;
    }
  }, [isIndeterminate]);

  return (
    <input
      ref={setCheckboxRef}
      type="checkbox"
      aria-label={ariaLabel}
      checked={isSelected}
      className="ag-table-selection-checkbox"
      onChange={(event) => onChange(event.currentTarget.checked)}
    />
  );
}

export function columnAlignClass(align?: AccountTableColumn['align']) {
  if (align === 'right') return 'text-right';
  if (align === 'left') return 'text-left';
  return 'text-center';
}

function cellJustifyClass(align?: AccountTableColumn['align']) {
  if (align === 'right') return 'justify-end';
  if (align === 'left') return 'justify-start';
  return 'justify-center';
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

const AccountRowSelectionCell = memo(function AccountRowSelectionCell({
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
  const isSelected = useSyncExternalStore(
    useCallback((listener) => selectionStore.subscribeRow(rowId, listener), [rowId, selectionStore]),
    useCallback(() => selectionStore.has(rowId), [rowId, selectionStore]),
    () => false,
  );
  const handleChange = useCallback((nextSelected: boolean) => {
    onSelectedChange(rowId, nextSelected);
  }, [onSelectedChange, rowId]);
  const registerInput = useCallback((input: HTMLInputElement | null) => {
    selectionStore.registerRowInput?.(rowId, input);
  }, [rowId, selectionStore]);

  return (
    <div className="inline-flex" onClick={(event) => event.stopPropagation()}>
      <TableSelectionCheckbox
        ariaLabel={ariaLabel}
        isSelected={isSelected}
        onChange={handleChange}
        inputRef={registerInput}
      />
    </div>
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
  return (
    <div className={`flex w-full min-w-0 items-center ${cellJustifyClass(column.align)}`}>
      {column.render(row, rowMeta)}
    </div>
  );
}, (prev, next) => {
  if (prev.column !== next.column) return false;
  return accountTableCellRowsEqual(prev.column.key, prev.row, next.row)
    && accountTableCellMetaEqual(prev.column.key, prev.rowMeta, next.rowMeta);
});

function sameAccountExceptCapacity(left: AccountResp, right: AccountResp) {
  return left.id === right.id
    && left.name === right.name
    && left.platform === right.platform
    && left.type === right.type
    && left.credentials === right.credentials
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
        && left.credentials?.email === right.credentials?.email;
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
  onRefreshQuota,
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
    refreshQuota: string;
    stats: string;
    statsShort: string;
    test: string;
    testShort: string;
  };
  onEdit: (row: AccountResp) => void;
  onDelete: (row: AccountResp) => void;
  onTest: (row: AccountResp) => void;
  onStats: (id: number) => void;
  onRefreshQuota: (id: number) => void;
  onClearCooldowns: (id: number) => void;
}) {
  return (
    <div className="ag-table-row-actions ag-account-row-actions mx-auto flex w-[124px] items-center justify-center gap-1">
      <button
        type="button"
        aria-label={labels.edit}
        title={labels.edit}
        className="ag-account-row-action-button"
        onClick={(event) => {
          event.stopPropagation();
          onEdit(row);
        }}
      >
        <span className="ag-account-row-action-label">{labels.editShort}</span>
      </button>
      <button
        type="button"
        aria-label={labels.test}
        title={labels.test}
        className="ag-account-row-action-button"
        onClick={(event) => {
          event.stopPropagation();
          onTest(row);
        }}
      >
        <span className="ag-account-row-action-label">{labels.testShort}</span>
      </button>
      <button
        type="button"
        aria-label={labels.stats}
        title={labels.stats}
        className="ag-account-row-action-button ag-account-row-action-button--stats"
        onClick={(event) => {
          event.stopPropagation();
          onStats(row.id);
        }}
      >
        <span className="ag-account-row-action-label">{labels.statsShort}</span>
      </button>
      <AccountRowOverflowMenu
        row={row}
        labels={labels}
        onDelete={onDelete}
        onRefreshQuota={onRefreshQuota}
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
  && prev.onRefreshQuota === next.onRefreshQuota
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
  onRefreshQuota,
  onClearCooldowns,
}: {
  row: AccountResp;
  labels: {
    actions: string;
    clearCooldowns: string;
    delete: string;
    more: string;
    refreshQuota: string;
  };
  onDelete: (row: AccountResp) => void;
  onRefreshQuota: (id: number) => void;
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
      >
        <span aria-hidden="true" className="ag-account-row-more-dots" />
      </button>
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
              onClick={runAction(() => onRefreshQuota(row.id))}
            >
              <RefreshCw className="w-3.5 h-3.5 ag-account-row-menu-icon ag-account-row-menu-icon--success" />
              <span>{labels.refreshQuota}</span>
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
  && prev.onRefreshQuota === next.onRefreshQuota
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

// formatCountdown 把剩余毫秒格式化成 "Xd Yh"/"Xh Ym"/"Ym" 样式，
// 与 sub2api 的"限流中 10h 16m 自动恢复"徽标一致。
function formatCountdown(ms: number): string {
  if (ms <= 0) return '';
  const s = Math.floor(ms / 1000);
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m`;
  return `${sec}s`;
}

function accountHasLiveCooldown(row: AccountResp, now: number): boolean {
  const stateUntil = row.state_until ? Date.parse(row.state_until) : 0;
  if (stateUntil > now) return true;
  return (row.family_cooldowns || []).some((fc) => Date.parse(fc.until) > now);
}

let cooldownClockNow = Date.now();
let cooldownClockTimer: number | null = null;
const cooldownClockListeners = new Set<() => void>();

function subscribeCooldownClock(listener: () => void) {
  cooldownClockNow = Date.now();
  cooldownClockListeners.add(listener);
  if (cooldownClockTimer == null) {
    cooldownClockTimer = window.setInterval(() => {
      cooldownClockNow = Date.now();
      cooldownClockListeners.forEach((notify) => notify());
    }, 1000);
  }

  return () => {
    cooldownClockListeners.delete(listener);
    if (cooldownClockListeners.size === 0 && cooldownClockTimer != null) {
      window.clearInterval(cooldownClockTimer);
      cooldownClockTimer = null;
    }
  };
}

function subscribeIdleClock() {
  return () => {};
}

function getCooldownClockSnapshot() {
  return cooldownClockNow;
}

function useCooldownClock(enabled: boolean): number {
  return useSyncExternalStore(
    enabled ? subscribeCooldownClock : subscribeIdleClock,
    getCooldownClockSnapshot,
    getCooldownClockSnapshot,
  );
}

let usageResetClockNow = Date.now();
let usageResetClockTimer: number | null = null;
const usageResetClockListeners = new Set<() => void>();

function subscribeUsageResetClock(listener: () => void) {
  usageResetClockNow = Date.now();
  usageResetClockListeners.add(listener);
  if (usageResetClockTimer == null) {
    usageResetClockTimer = window.setInterval(() => {
      usageResetClockNow = Date.now();
      usageResetClockListeners.forEach((notify) => notify());
    }, 30_000);
  }

  return () => {
    usageResetClockListeners.delete(listener);
    if (usageResetClockListeners.size === 0 && usageResetClockTimer != null) {
      window.clearInterval(usageResetClockTimer);
      usageResetClockTimer = null;
    }
  };
}

function getUsageResetClockSnapshot() {
  return usageResetClockNow;
}

export function useUsageResetClock(enabled: boolean): number {
  return useSyncExternalStore(
    enabled ? subscribeUsageResetClock : subscribeIdleClock,
    getUsageResetClockSnapshot,
    getUsageResetClockSnapshot,
  );
}

/**
 * AccountStatusCell 渲染账号状态徽标，按 state + state_until 动态展示：
 *   active       → 绿色 "活跃"
 *   rate_limited → 橙色 "限流中 Xh Ym"（state_until 倒计时）
 *   degraded     → 黄色 "降级 Xm"（上游退避，倒计时）
 *   disabled     → 红色 "已禁用"（tooltip 显示 error_msg）
 * 到期的 rate_limited / degraded 视作 active（后端 lazy 回收，前端可先显示 active）。
 *
 * 同一行还会叠加家族级冷却（family_cooldowns）：账号 state 可能仍是 active，
 * 但某个 family（如 gpt-image）在 Redis 上仍处冷却中。用一个橙色小 pill
 * 标出"限流家族数"，hover tooltip 列出每个家族剩余时间。
 */
export function AccountStatusCell({ row }: { row: AccountResp }) {
  const { t } = useTranslation();
  const hasLiveCooldown = accountHasLiveCooldown(row, Date.now());
  const [isCooldownHovered, setIsCooldownHovered] = useState(false);
  const hoverNowRef = useRef<number | null>(null);
  const tickingNow = useCooldownClock(hasLiveCooldown && !isCooldownHovered);
  const liveNow = hasLiveCooldown ? tickingNow : Date.now();
  const now = isCooldownHovered && hoverNowRef.current != null ? hoverNowRef.current : liveNow;
  const untilMs = row.state_until ? Date.parse(row.state_until) : 0;
  const remainingMs = untilMs - now;
  const hasCountdown = untilMs > 0 && remainingMs > 0;

  // 过滤出仍生效的家族冷却（后端可能返回刚到期的）。
  const liveFamilyCooldowns = (row.family_cooldowns || []).filter(
    (fc) => Date.parse(fc.until) > now,
  );

  const pill = (label: string, bg: string, fg: string, tooltip?: string) => (
    <span
      className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-[11px] font-semibold border whitespace-nowrap"
      style={{ background: bg, color: fg, borderColor: bg }}
      title={tooltip}
    >
      <span className="w-1.5 h-1.5 rounded-full" style={{ background: fg }} />
      {label}
    </span>
  );

  const freezeCooldownHoverProps = hasLiveCooldown
    ? {
      onMouseEnter: () => {
        hoverNowRef.current = liveNow;
        setIsCooldownHovered(true);
      },
      onMouseLeave: () => {
        hoverNowRef.current = null;
        setIsCooldownHovered(false);
      },
    }
    : undefined;

  // 主 state 徽标
  let mainBadge: ReactElement;
  if (row.state === 'rate_limited' && hasCountdown) {
    mainBadge = pill(
      `${t('accounts.rate_limited_label', '限流中')} ${formatCountdown(remainingMs)}`,
      'var(--ag-warning-subtle)',
      'var(--ag-warning)',
      t('accounts.rate_limited_tooltip', '上游限流，到期自动恢复，不影响调度开关'),
    );
  } else if (row.state === 'degraded' && hasCountdown) {
    mainBadge = pill(
      `${t('accounts.degraded_label', '降级')} ${formatCountdown(remainingMs)}`,
      'var(--ag-warning-subtle)',
      'var(--ag-warning)',
      t('accounts.degraded_tooltip', '退避中，暂停调度，到期自动恢复'),
    );
  } else if (row.state === 'disabled') {
    const reason = row.error_msg?.trim() === '管理员手动关闭调度' ? '手动关闭' : row.error_msg?.trim();
    mainBadge = (
      <div className="inline-flex min-w-0 max-w-full flex-col items-center gap-0.5">
        <StatusPill label={t('status.disabled')} status="disabled" tooltip={reason || undefined} />
        {reason && (
          <span className="block max-w-[5.75rem] truncate text-center text-[10px] leading-none text-[var(--ag-muted)]" title={reason}>
            {reason}
          </span>
        )}
      </div>
    );
  } else {
    // active，或 rate_limited/degraded 已到期（lazy 恢复）
    mainBadge = <StatusPill label={t('status.active')} status="active" />;
  }

  if (liveFamilyCooldowns.length === 0) {
    if (!freezeCooldownHoverProps) return mainBadge;
    return (
      <span className="inline-flex max-w-full" {...freezeCooldownHoverProps}>
        {mainBadge}
      </span>
    );
  }

  // tooltip 多行：每个家族 + 剩余时间，rate-limit 原因截断到 80 字符避免过宽
  const familyTooltip = liveFamilyCooldowns
    .map((fc) => {
      const ms = Date.parse(fc.until) - now;
      const reason = fc.reason ? ` — ${fc.reason.slice(0, 80)}` : '';
      return `${fc.family} ${formatCountdown(ms)}${reason}`;
    })
    .join('\n');

  const familyLabel = t(
    'accounts.family_cooldown_label',
    '{{count}} 家族限流',
    { count: liveFamilyCooldowns.length },
  );

  return (
    <div
      className="flex w-full max-w-full flex-wrap items-center justify-center gap-1 text-center"
      {...freezeCooldownHoverProps}
    >
      {mainBadge}
      {pill(
        familyLabel,
        'var(--ag-warning-subtle)',
        'var(--ag-warning)',
        familyTooltip,
      )}
    </div>
  );
}

export function AccountCapacityChip({ current, max }: { current: number; max: number }) {
  const previousCurrentRef = useRef(current);
  const pulseTimerRef = useRef<number | null>(null);
  const [isPulsing, setIsPulsing] = useState(false);
  const [pulseTone, setPulseTone] = useState<'success' | 'warning'>('success');
  const [pulseToken, setPulseToken] = useState(0);
  const state = current <= 0 ? 'idle' : current >= max ? 'full' : 'active';

  useEffect(() => {
    if (previousCurrentRef.current === current) return;
    const previousCurrent = previousCurrentRef.current;
    previousCurrentRef.current = current;

    if (pulseTimerRef.current != null) {
      window.clearTimeout(pulseTimerRef.current);
    }

    setPulseTone(current < previousCurrent ? 'warning' : 'success');
    setIsPulsing(true);
    setPulseToken((token) => token + 1);
    pulseTimerRef.current = window.setTimeout(() => {
      setIsPulsing(false);
      pulseTimerRef.current = null;
    }, 520);
  }, [current]);

  useEffect(() => () => {
    if (pulseTimerRef.current != null) {
      window.clearTimeout(pulseTimerRef.current);
    }
  }, []);

  return (
    <span
      key={pulseToken}
      className="ag-account-capacity"
      data-state={state}
      data-pulse={isPulsing || undefined}
      data-pulse-tone={pulseTone}
      title={`${current} / ${max}`}
    >
      <span className="ag-account-capacity-current">{current}</span>
      <span className="ag-account-capacity-divider">/</span>
      <span className="ag-account-capacity-max">{max}</span>
    </span>
  );
}

export const AccountCapacityLiveChip = memo(function AccountCapacityLiveChip({
  current,
  max,
  rowId,
  store,
}: {
  current: number;
  max: number;
  rowId: number;
  store: AccountCapacityStore;
}) {
  const liveCurrent = useSyncExternalStore(
    useCallback((listener) => store.subscribe(rowId, listener), [rowId, store]),
    useCallback(() => store.getCurrent(rowId, current), [current, rowId, store]),
    () => current,
  );

  return <AccountCapacityChip current={liveCurrent} max={max} />;
});
