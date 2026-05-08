import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@heroui/react';
import { Eraser, Pencil, Power, PowerOff, RefreshCw, Trash2, X } from 'lucide-react';

/**
 * 批量操作工具栏：仅在 selectedCount > 0 时渲染。
 *
 * - inline:   内联在工具栏行内（挤占空间）
 * - overlay:  绝对定位覆盖在父容器上方，不推动表格（推荐）
 */
export function BulkActionsBar({
  inline = false,
  overlay = false,
  selectedCount,
  onClear,
  onEdit,
  onEnable,
  onDisable,
  onRefreshQuota,
  onClearRateLimitMarkers,
  onDelete,
}: {
  inline?: boolean;
  overlay?: boolean;
  selectedCount: number;
  onClear: () => void;
  onEdit: () => void;
  onEnable: () => void;
  onDisable: () => void;
  onRefreshQuota: () => void;
  onClearRateLimitMarkers: () => void;
  onDelete: () => void;
}) {
  const { t } = useTranslation();
  const countPulseTimerRef = useRef<number | null>(null);
  const [countPulseToken, setCountPulseToken] = useState(0);
  const [isCountPulsing, setIsCountPulsing] = useState(false);

  useEffect(() => {
    if (countPulseTimerRef.current != null) {
      window.clearTimeout(countPulseTimerRef.current);
    }
    setIsCountPulsing(true);
    setCountPulseToken((token) => token + 1);
    countPulseTimerRef.current = window.setTimeout(() => {
      setIsCountPulsing(false);
      countPulseTimerRef.current = null;
    }, 420);
  }, [selectedCount]);

  useEffect(() => () => {
    if (countPulseTimerRef.current != null) {
      window.clearTimeout(countPulseTimerRef.current);
    }
  }, []);

  const selectedText = t('accounts.bulk_selected', { count: selectedCount });
  const selectedCountText = String(selectedCount);
  const countIndex = selectedText.indexOf(selectedCountText);
  const selectedTextBefore = countIndex >= 0 ? selectedText.slice(0, countIndex) : selectedText;
  const selectedTextAfter = countIndex >= 0 ? selectedText.slice(countIndex + selectedCountText.length) : '';

  if (selectedCount === 0) return null;

  if (overlay) {
    return (
      <div
        className="absolute inset-0 z-20 flex min-h-12 items-center gap-2 overflow-hidden rounded-[var(--radius)] border border-border bg-background px-4 py-2 shadow-lg animate-in fade-in slide-in-from-top-1 duration-200"
        style={{
          background: 'linear-gradient(90deg, var(--surface-tertiary) 0%, var(--surface-secondary) 42%, var(--background) 100%)',
          backgroundColor: 'var(--background)',
        }}
      >
        <span className="flex h-8 shrink-0 items-center gap-0.5 whitespace-nowrap text-[15px] font-semibold leading-none text-primary">
          <span>{selectedTextBefore}</span>
          {countIndex >= 0 ? (
            <span
              key={countPulseToken}
              className="ag-bulk-selected-count"
              data-pulse={isCountPulsing || undefined}
            >
              {selectedCountText}
            </span>
          ) : null}
          <span>{selectedTextAfter}</span>
        </span>

        <div className="hidden h-5 w-px bg-border sm:block" />

        <div className="flex h-8 min-w-0 flex-1 items-center gap-1.5 overflow-x-auto">
          <ActionButton icon={<Pencil className="w-3.5 h-3.5" />} label={t('accounts.bulk_edit')} onClick={onEdit} />
          <ActionButton icon={<Power className="w-3.5 h-3.5" />} label={t('accounts.bulk_enable')} onClick={onEnable} />
          <ActionButton icon={<PowerOff className="w-3.5 h-3.5" />} label={t('accounts.bulk_disable')} onClick={onDisable} />
          <ActionButton
            icon={<RefreshCw className="w-3.5 h-3.5 text-success" />}
            label={t('accounts.bulk_refresh_quota')}
            onClick={onRefreshQuota}
          />
          <ActionButton
            icon={<Eraser className="w-3.5 h-3.5 text-warning" />}
            label={t('accounts.bulk_clear_family_cooldowns')}
            onClick={onClearRateLimitMarkers}
          />
        </div>
        <ActionButton
          icon={<Trash2 className="w-3.5 h-3.5" />}
          label={t('accounts.bulk_delete')}
          onClick={onDelete}
          danger
        />
      </div>
    );
  }

  if (inline) {
    return (
      <div className="inline-flex min-h-8 w-full flex-wrap items-center gap-1.5 border-l-0 pl-0 xl:w-auto xl:flex-nowrap xl:border-l xl:border-border-subtle xl:pl-1.5">
        <span className="shrink-0 whitespace-nowrap text-[15px] font-semibold text-text-secondary">
          {selectedText}
        </span>
        <ActionButton icon={<Pencil className="w-3.5 h-3.5" />} label={t('accounts.bulk_edit')} onClick={onEdit} />
        <ActionButton icon={<Power className="w-3.5 h-3.5" />} label={t('accounts.bulk_enable')} onClick={onEnable} />
        <ActionButton icon={<PowerOff className="w-3.5 h-3.5" />} label={t('accounts.bulk_disable')} onClick={onDisable} />
        <ActionButton
          icon={<RefreshCw className="w-3.5 h-3.5 text-success" />}
          label={t('accounts.bulk_refresh_quota')}
          onClick={onRefreshQuota}
        />
        <ActionButton
          icon={<Eraser className="w-3.5 h-3.5 text-warning" />}
          label={t('accounts.bulk_clear_family_cooldowns')}
          onClick={onClearRateLimitMarkers}
        />
        <ActionButton
          icon={<Trash2 className="w-3.5 h-3.5" />}
          label={t('accounts.bulk_delete')}
          onClick={onDelete}
          danger
        />
        <Button
          isIconOnly
          size="sm"
          variant="ghost"
          onPress={onClear}
          aria-label={t('accounts.bulk_clear')}
        >
          <X className="w-3.5 h-3.5" />
        </Button>
      </div>
    );
  }

  return (
    <div
      className="mb-3 flex flex-wrap items-center gap-2 rounded-[var(--radius)] px-4 py-2.5"
      style={{
        background: 'var(--ag-primary-subtle)',
        border: '1px solid color-mix(in oklab, var(--ag-primary) 52%, transparent)',
      }}
    >
      <span className="text-sm font-medium" style={{ color: 'var(--ag-primary)' }}>
        {t('accounts.bulk_selected', { count: selectedCount })}
      </span>
      <Button
        size="sm"
        variant="ghost"
        onPress={onClear}
        aria-label={t('accounts.bulk_clear')}
      >
        <X className="w-3 h-3" />
        {t('accounts.bulk_clear')}
      </Button>

      <div className="hidden h-5 w-px bg-border sm:block" />

      <ActionButton icon={<Pencil className="w-3.5 h-3.5" />} label={t('accounts.bulk_edit')} onClick={onEdit} />
      <ActionButton icon={<Power className="w-3.5 h-3.5" />} label={t('accounts.bulk_enable')} onClick={onEnable} />
      <ActionButton icon={<PowerOff className="w-3.5 h-3.5" />} label={t('accounts.bulk_disable')} onClick={onDisable} />
      <ActionButton
        icon={<RefreshCw className="w-3.5 h-3.5 text-success" />}
        label={t('accounts.bulk_refresh_quota')}
        onClick={onRefreshQuota}
      />
      <ActionButton
        icon={<Eraser className="w-3.5 h-3.5 text-warning" />}
        label={t('accounts.bulk_clear_family_cooldowns')}
        onClick={onClearRateLimitMarkers}
      />
      <div className="flex-1" />
      <ActionButton
        icon={<Trash2 className="w-3.5 h-3.5" />}
        label={t('accounts.bulk_delete')}
        onClick={onDelete}
        danger
      />
    </div>
  );
}

function ActionButton({
  icon,
  label,
  onClick,
  danger = false,
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  danger?: boolean;
}) {
  return (
    <Button
      size="sm"
      variant="outline"
      className={`h-8 min-h-8 shrink-0 items-center gap-1.5 px-2.5 leading-none ${danger ? 'text-danger' : ''}`}
      onPress={onClick}
    >
      {icon}
      <span className="leading-none">{label}</span>
    </Button>
  );
}
