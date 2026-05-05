import { useTranslation } from 'react-i18next';
import { Button } from '@heroui/react';
import { Eraser, Pencil, Power, PowerOff, RefreshCw, Trash2, X } from 'lucide-react';

/**
 * 批量操作工具栏：仅在 selectedCount > 0 时渲染。
 */
export function BulkActionsBar({
  selectedCount,
  onClear,
  onEdit,
  onEnable,
  onDisable,
  onRefreshQuota,
  onClearRateLimitMarkers,
  onDelete,
}: {
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

  if (selectedCount === 0) return null;

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
      variant={danger ? 'danger-soft' : 'outline'}
      onPress={onClick}
    >
      {icon}
      <span>{label}</span>
    </Button>
  );
}
