import { useTranslation } from 'react-i18next';

interface BadgeProps {
  children: string;
  variant?: 'default' | 'success' | 'warning' | 'danger' | 'info';
}

const variantStyles: Record<string, string> = {
  default: 'bg-bg-hover text-text-secondary border-glass-border',
  success: 'bg-success-subtle text-success border-success-subtle',
  warning: 'bg-warning-subtle text-warning border-warning-subtle',
  danger: 'bg-danger-subtle text-danger border-danger-subtle',
  info: 'bg-info-subtle text-info border-info-subtle',
};

export function Badge({ children, variant = 'default' }: BadgeProps) {
  return (
    <span
      className={`inline-flex items-center px-2 py-0.5 rounded-full text-[11px] font-medium border ${variantStyles[variant]}`}
    >
      <span className="w-1.5 h-1.5 rounded-full mr-1.5 opacity-80" style={{ background: 'currentColor' }} />
      {children}
    </span>
  );
}

const statusMap: Record<string, { variant: BadgeProps['variant']; label: string }> = {
  active: { variant: 'success', label: 'status.active' },
  enabled: { variant: 'success', label: 'status.enabled' },
  disabled: { variant: 'default', label: 'status.disabled' },
  error: { variant: 'danger', label: 'status.error' },
  expired: { variant: 'warning', label: 'status.expired' },
  suspended: { variant: 'warning', label: 'status.suspended' },
  pending: { variant: 'info', label: 'status.pending' },
  paid: { variant: 'success', label: 'status.paid' },
  failed: { variant: 'danger', label: 'status.failed' },
  installed: { variant: 'info', label: 'status.installed' },
};

export function StatusBadge({ status }: { status: string }) {
  const { t } = useTranslation();
  const config = statusMap[status] || { variant: 'default' as const, label: status };
  return <Badge variant={config.variant}>{t(config.label)}</Badge>;
}
