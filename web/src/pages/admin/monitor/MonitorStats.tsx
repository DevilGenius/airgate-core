import type { ReactNode } from 'react';
import { Card } from '@heroui/react';
import { useTranslation } from 'react-i18next';
import { AlertTriangle, ShieldAlert, TriangleAlert } from 'lucide-react';
import { fmtNum } from '../../../shared/columns/usageColumns';
import type { MonitorSummaryResp } from '../../../shared/types';

function StatCard({
  icon,
  label,
  tone,
  value,
}: {
  icon: ReactNode;
  label: string;
  tone: string;
  value: number;
}) {
  return (
    <Card className="ag-dashboard-metric min-h-[72px] 2xl:min-h-[78px]">
      <Card.Content className="ag-dashboard-metric-content p-3 2xl:p-3.5">
        <div className="ag-dashboard-metric-copy">
          <div className="truncate text-sm font-semibold tracking-normal text-text-tertiary">{label}</div>
          <div className="mt-1 font-mono text-[22px] font-semibold leading-none text-text 2xl:text-2xl">{fmtNum(value)}</div>
        </div>
        <span className={`hidden h-11 w-11 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1 shadow-sm 2xl:flex ${tone}`}>
          {icon}
        </span>
      </Card.Content>
    </Card>
  );
}

export function MonitorStats({ summary }: { summary?: MonitorSummaryResp }) {
  const { t } = useTranslation();
  return (
    <div className="mb-6 grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4 2xl:gap-4">
      <StatCard
        icon={<ShieldAlert className="h-5 w-5" />}
        label={t('monitor.active')}
        tone="bg-zinc-100 text-zinc-600 ring-zinc-200 dark:bg-zinc-400/15 dark:text-zinc-300 dark:ring-zinc-400/25"
        value={summary?.active_total ?? 0}
      />
      <StatCard
        icon={<TriangleAlert className="h-5 w-5" />}
        label={t('monitor.critical')}
        tone="bg-danger/10 text-danger ring-danger/20"
        value={summary?.critical_total ?? 0}
      />
      <StatCard
        icon={<AlertTriangle className="h-5 w-5" />}
        label={t('monitor.error')}
        tone="bg-rose-100 text-rose-600 ring-rose-200 dark:bg-rose-400/15 dark:text-rose-300 dark:ring-rose-400/25"
        value={summary?.error_total ?? 0}
      />
      <StatCard
        icon={<AlertTriangle className="h-5 w-5" />}
        label={t('monitor.warning')}
        tone="bg-amber-100 text-amber-600 ring-amber-200 dark:bg-amber-400/15 dark:text-amber-300 dark:ring-amber-400/25"
        value={summary?.warning_total ?? 0}
      />
    </div>
  );
}
