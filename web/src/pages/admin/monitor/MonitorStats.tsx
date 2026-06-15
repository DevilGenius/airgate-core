import type { ReactNode } from 'react';
import { Card } from '@heroui/react';
import { useTranslation } from 'react-i18next';
import { AlertTriangle, Info, TriangleAlert } from 'lucide-react';
import { fmtNum } from '../../../shared/columns/usageColumns';
import type { MonitorSummaryResp } from '../../../shared/types';

function StatCard({
  icon,
  active,
  label,
  showActiveRatio = true,
  tone,
  total,
}: {
  icon: ReactNode;
  active: number;
  label: string;
  showActiveRatio?: boolean;
  tone: string;
  total: number;
}) {
  return (
    <Card className="ag-dashboard-metric min-h-[72px] 2xl:min-h-[78px]">
      <Card.Content className="ag-dashboard-metric-content p-3 2xl:p-3.5">
        <div className="ag-dashboard-metric-copy">
          <div className="truncate text-sm font-semibold tracking-normal text-text-tertiary">{label}</div>
          <div className="mt-1 flex min-w-0 items-baseline gap-1 font-mono text-[22px] font-semibold leading-none text-text 2xl:text-2xl">
            {showActiveRatio ? (
              <>
                <span className="truncate">{fmtNum(active)}</span>
                <span className="text-base text-text-tertiary 2xl:text-lg">/</span>
                <span className="truncate">{fmtNum(total)}</span>
              </>
            ) : (
              <span className="truncate">{fmtNum(total)}</span>
            )}
          </div>
        </div>
        <span className={`hidden h-11 w-11 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1 shadow-sm 2xl:flex ${tone}`}>
          {icon}
        </span>
      </Card.Content>
    </Card>
  );
}

export function MonitorStats({
  showActiveCounts = true,
  summary,
}: {
  showActiveCounts?: boolean;
  summary?: MonitorSummaryResp;
}) {
  const { t } = useTranslation();
  return (
    <div className="mb-6 grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4 2xl:gap-4">
      <StatCard
        active={summary?.critical_active_total ?? 0}
        icon={<TriangleAlert className="h-5 w-5" />}
        label={t('monitor.critical')}
        showActiveRatio={showActiveCounts}
        tone="bg-black text-white ring-black dark:bg-black dark:text-white dark:ring-zinc-600"
        total={summary?.critical_total ?? 0}
      />
      <StatCard
        active={summary?.error_active_total ?? 0}
        icon={<AlertTriangle className="h-5 w-5" />}
        label={t('monitor.error')}
        showActiveRatio={showActiveCounts}
        tone="bg-rose-100 text-rose-600 ring-rose-200 dark:bg-rose-400/15 dark:text-rose-300 dark:ring-rose-400/25"
        total={summary?.error_total ?? 0}
      />
      <StatCard
        active={summary?.warning_active_total ?? 0}
        icon={<AlertTriangle className="h-5 w-5" />}
        label={t('monitor.warning')}
        showActiveRatio={false}
        tone="bg-amber-100 text-amber-600 ring-amber-200 dark:bg-amber-400/15 dark:text-amber-300 dark:ring-amber-400/25"
        total={summary?.warning_total ?? 0}
      />
      <StatCard
        active={summary?.info_active_total ?? 0}
        icon={<Info className="h-5 w-5" />}
        label={t('monitor.severity_info')}
        showActiveRatio={false}
        tone="bg-sky-100 text-sky-600 ring-sky-200 dark:bg-sky-400/15 dark:text-sky-300 dark:ring-sky-400/25"
        total={summary?.info_total ?? 0}
      />
    </div>
  );
}
