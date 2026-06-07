import { useTranslation } from 'react-i18next';

type NativeStatusTone = 'accent' | 'danger' | 'default' | 'success' | 'warning';

const statusMap: Record<string, { label: string; tone: NativeStatusTone }> = {
  active: { label: 'status.active', tone: 'success' },
  disabled: { label: 'status.disabled', tone: 'default' },
  enabled: { label: 'status.enabled', tone: 'success' },
  error: { label: 'status.error', tone: 'danger' },
  expired: { label: 'status.expired', tone: 'warning' },
  failed: { label: 'status.failed', tone: 'danger' },
  installed: { label: 'status.installed', tone: 'accent' },
  paid: { label: 'status.paid', tone: 'success' },
  pending: { label: 'status.pending', tone: 'accent' },
  suspended: { label: 'status.suspended', tone: 'warning' },
};

export function NativeStatusChip({ status }: { status: string }) {
  const { t } = useTranslation();
  const config = statusMap[status] ?? { label: status, tone: 'default' as const };

  return (
    <span className="ag-native-status-chip" data-tone={config.tone}>
      <span className="ag-native-status-chip__label">{t(config.label)}</span>
    </span>
  );
}
