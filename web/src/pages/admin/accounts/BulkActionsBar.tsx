import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Eraser, Pencil, Play, Power, PowerOff, RefreshCw, Trash2 } from 'lucide-react';
import { TableSelectionCheckbox } from './accountTableSupport';

/**
 * 批量操作工具栏：仅在 selectedCount > 0 时渲染。
 * 不提供单独的"清除"按钮——再次点击全选复选框反选即清空选择。
 *
 * - inline:   内联在工具栏行内（挤占空间）
 * - overlay:  绝对定位覆盖在父容器上方，不推动表格（推荐）。
 *             覆盖层从选择列右侧开始，全选复选框由表头常驻单元格承担，
 *             因此 overlay 变体不渲染 selectAllControl。
 */
export function BulkActionsBar({
  allVisibleSelected,
  inline = false,
  isActive,
  overlay = false,
  selectedCount,
  someVisibleSelected,
  onEdit,
  onEnable,
  onDisable,
  onTest,
  onRefreshToken,
  onSelectAllChange,
  onClearRateLimitMarkers,
  onDelete,
}: {
  allVisibleSelected: boolean;
  inline?: boolean;
  isActive?: boolean;
  overlay?: boolean;
  selectedCount: number;
  someVisibleSelected: boolean;
  onEdit: () => void;
  onEnable: () => void;
  onDisable: () => void;
  onTest: () => void;
  onRefreshToken: () => void;
  onSelectAllChange: (isSelected: boolean) => void;
  onClearRateLimitMarkers: () => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation();
  const hasSelection = selectedCount > 0;
  const active = isActive ?? hasSelection;
  const selectedText = t('accounts.bulk_selected', { count: selectedCount });
  const selectedCountText = String(selectedCount);
  const countIndex = selectedText.indexOf(selectedCountText);
  const selectedTextBefore = countIndex >= 0 ? selectedText.slice(0, countIndex) : selectedText;
  const selectedTextAfter = countIndex >= 0 ? selectedText.slice(countIndex + selectedCountText.length) : '';

  if (!hasSelection && !overlay) return null;

  const selectedCountLabel = (
    <span className="flex h-8 shrink-0 items-center gap-0.5 whitespace-nowrap text-[15px] font-semibold leading-none text-primary">
      <span>{selectedTextBefore}</span>
      {countIndex >= 0 ? (
        <span className="ag-bulk-selected-count">
          {selectedCountText}
        </span>
      ) : null}
      <span>{selectedTextAfter}</span>
    </span>
  );
  const selectAllControl = (
    <label className="ag-bulk-select-all">
      <TableSelectionCheckbox
        ariaLabel={t('accounts.bulk_select_all')}
        isIndeterminate={someVisibleSelected}
        isSelected={allVisibleSelected}
        onChange={onSelectAllChange}
      />
      <span>{t('accounts.bulk_select_all')}</span>
    </label>
  );

  const actionButtons = (
    <>
      <ActionButton disabled={!active} icon={<Pencil className="w-3.5 h-3.5" />} label={t('accounts.bulk_edit')} onClick={onEdit} />
      <ActionButton disabled={!active} icon={<Power className="w-3.5 h-3.5" />} label={t('accounts.bulk_enable')} onClick={onEnable} />
      <ActionButton disabled={!active} icon={<PowerOff className="w-3.5 h-3.5" />} label={t('accounts.bulk_disable')} onClick={onDisable} />
      <ActionButton disabled={!active} icon={<Play className="w-3.5 h-3.5 text-primary" />} label={t('accounts.bulk_test')} onClick={onTest} />
      <ActionButton
        disabled={!active}
        icon={<RefreshCw className="w-3.5 h-3.5 text-success" />}
        label={t('accounts.bulk_refresh_token')}
        onClick={onRefreshToken}
      />
      <ActionButton
        disabled={!active}
        icon={<Eraser className="w-3.5 h-3.5 text-warning" />}
        label={t('accounts.bulk_clear_family_cooldowns')}
        onClick={onClearRateLimitMarkers}
      />
      <ActionButton
        disabled={!active}
        icon={<Trash2 className="w-3.5 h-3.5" />}
        label={t('accounts.bulk_delete')}
        onClick={onDelete}
        danger
      />
    </>
  );

  if (overlay) {
    return (
      <div
        aria-hidden={!active}
        className="ag-accounts-bulk-header-bar absolute top-0 z-20 flex items-center gap-2 overflow-hidden border border-border bg-background px-2"
        data-active={active ? 'true' : 'false'}
        style={{
          background: 'linear-gradient(90deg, var(--surface-tertiary) 0%, var(--surface-secondary) 42%, var(--background) 100%)',
          backgroundColor: 'var(--background)',
        }}
      >
        {selectedCountLabel}

        <div className="h-4 w-px shrink-0 bg-border" />

        <div className="flex min-w-0 items-center gap-2">
          {actionButtons}
        </div>
      </div>
    );
  }

  if (inline) {
    return (
      <div
        className="inline-flex min-h-12 max-w-full flex-wrap items-center gap-2 rounded-[var(--radius)] border border-border bg-background px-4 py-2"
        style={{
          background: 'linear-gradient(90deg, var(--surface-tertiary) 0%, var(--surface-secondary) 42%, var(--background) 100%)',
          backgroundColor: 'var(--background)',
        }}
      >
        {selectAllControl}
        {selectedCountLabel}
        <div className="hidden h-5 w-px bg-border sm:block" />
        <div className="flex min-w-0 flex-wrap items-center gap-1.5">
          {actionButtons}
        </div>
      </div>
    );
  }

  return (
    <div
      className="mb-3 flex min-h-12 flex-wrap items-center gap-2 rounded-[var(--radius)] border border-border bg-background px-2 py-2"
      style={{
        background: 'linear-gradient(90deg, var(--surface-tertiary) 0%, var(--surface-secondary) 42%, var(--background) 100%)',
        backgroundColor: 'var(--background)',
      }}
    >
      {selectAllControl}
      {selectedCountLabel}

      <div className="hidden h-5 w-px bg-border sm:block" />

      {actionButtons}
    </div>
  );
}

function ActionButton({
  disabled = false,
  icon,
  label,
  onClick,
  danger = false,
}: {
  disabled?: boolean;
  icon: ReactNode;
  label: string;
  onClick: () => void;
  danger?: boolean;
}) {
  return (
    <button
      type="button"
      className={`ag-bulk-action-button min-w-0 max-w-full shrink-0 items-center gap-1.5 leading-none ${danger ? 'ag-bulk-action-button--danger' : ''}`}
      disabled={disabled}
      onClick={() => {
        if (!disabled) onClick();
      }}
    >
      {icon}
      <span className="min-w-0 truncate leading-none">{label}</span>
    </button>
  );
}
