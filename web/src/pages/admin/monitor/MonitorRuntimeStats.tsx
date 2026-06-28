import { Fragment, type ReactNode } from 'react';
import { Card } from '@heroui/react';
import { useTranslation } from 'react-i18next';
import { Activity, AlertTriangle, Cpu, Database } from 'lucide-react';
import { fmtNum } from '../../../shared/columns/usageColumns';
import type { MonitorRuntimeResp, MonitorSummaryResp } from '../../../shared/types';

function formatDuration(ms?: number) {
  if (!ms || ms <= 0) return '-';
  if (ms >= 1000) return `${(ms / 1000).toFixed(ms >= 10_000 ? 1 : 2)}s`;
  return `${Math.round(ms)}ms`;
}

function formatPing(healthy?: boolean, ms?: number) {
  if (!healthy) return '-';
  return `${Math.max(0, Math.round(ms ?? 0))}ms`;
}

function formatPercent(value?: number) {
  if (value == null || !Number.isFinite(value)) return '-';
  return `${(value * 100).toFixed(value >= 0.1 ? 1 : 2)}%`;
}

function formatDeltaPercent(current?: number, baseline?: number) {
  if (!current || !baseline || baseline <= 0) return '-';
  const delta = (current - baseline) / baseline;
  const sign = delta >= 0 ? '+' : '-';
  const abs = Math.abs(delta * 100);
  return `${sign}${abs >= 10 ? abs.toFixed(0) : abs.toFixed(1)}%`;
}

function formatDurationPair(current?: number, baseline?: number) {
  return `${formatDuration(current)}/${formatDuration(baseline)}`;
}

function formatDurationPairWithDelta(current?: number, baseline?: number) {
  return `${formatDurationPair(current, baseline)} ${formatDeltaPercent(current, baseline)}`;
}

function formatCPU(value?: number) {
  if (value == null || !Number.isFinite(value)) return '-';
  return `${value.toFixed(value >= 10 ? 0 : 1)}%`;
}

function formatBytes(value?: number) {
  if (!value || value <= 0) return '-';
  const mib = value / 1024 / 1024;
  if (mib >= 1024) return `${(mib / 1024).toFixed(2)}GB`;
  return `${mib.toFixed(mib >= 100 ? 0 : 1)}MB`;
}

function ratioText(used?: number, total?: number) {
  const left = fmtNum(Math.max(0, used ?? 0));
  if (!total || total <= 0) return `${left} / -`;
  return `${left} / ${fmtNum(total)}`;
}

function DetailSeparator() {
  return <span className="justify-self-center font-bold text-text-secondary">|</span>;
}

function joinDetail(parts: ReactNode[]) {
  const columns = parts.length > 2
    ? 'grid-cols-[minmax(0,1fr)_0.75rem_minmax(0,1fr)_0.75rem_minmax(0,1fr)]'
    : 'grid-cols-[minmax(0,1fr)_0.75rem_minmax(0,1fr)]';
  return (
    <span className={`grid min-w-0 items-center ${columns}`}>
      {parts.map((part, index) => (
        <Fragment key={index}>
          {index > 0 ? <DetailSeparator /> : null}
          <span className="min-w-0 truncate">{part}</span>
        </Fragment>
      ))}
    </span>
  );
}

function RuntimeCard({
  details,
  icon,
  label,
  meta,
  tone,
  value,
}: {
  details: ReactNode[];
  icon: ReactNode;
  label: string;
  meta: ReactNode;
  tone: string;
  value: ReactNode;
}) {
  return (
    <Card className="ag-dashboard-metric h-[150px] 2xl:h-[156px]">
      <Card.Content className="flex h-full flex-col p-3 2xl:p-3.5">
        <div className="flex min-w-0 items-center justify-between gap-3">
          <div className="min-w-0">
            <div className="h-5 truncate text-sm font-semibold leading-5 tracking-normal text-text-tertiary">{label}</div>
            <div className="mt-1 h-6 min-w-0 truncate font-mono text-[21px] font-semibold leading-6 text-text 2xl:h-7 2xl:text-2xl">
              {value}
            </div>
          </div>
          <MetricIcon icon={icon} tone={tone} />
        </div>
        <div className="mt-auto min-w-0">
          <div className="mt-1.5 space-y-0.5 text-xs leading-4 text-text-tertiary">
            <div className="min-w-0 overflow-hidden">{meta}</div>
            {details.map((detail, index) => (
              <div className="min-w-0 overflow-hidden" key={index}>{detail}</div>
            ))}
          </div>
        </div>
      </Card.Content>
    </Card>
  );
}

function MetricIcon({ icon, tone }: { icon: ReactNode; tone: string }) {
  return (
    <span className={`hidden h-11 w-11 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1 shadow-sm 2xl:flex ${tone}`}>
      {icon}
    </span>
  );
}

function summaryValue(active: number, total: number, showActiveRatio: boolean) {
  if (!showActiveRatio) return fmtNum(total);
  return `${fmtNum(active)} / ${fmtNum(total)}`;
}

function SummaryMiniStat({
  label,
  tone,
  value,
}: {
  label: string;
  tone: string;
  value: string;
}) {
  return (
    <div className="grid min-h-8 min-w-0 grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-1.5 rounded-[var(--field-radius)] bg-surface-secondary px-2 py-0.5">
      <span className={`h-2.5 w-2.5 shrink-0 rounded-full ring-1 ${tone}`} />
      <span className="min-w-0 truncate text-xs font-medium leading-4 text-text-tertiary">{label}</span>
      <span className="truncate font-mono text-sm font-semibold leading-5 text-text">{value}</span>
    </div>
  );
}

function MonitorSummaryCard({
  showActiveCounts,
  summary,
}: {
  showActiveCounts: boolean;
  summary?: MonitorSummaryResp;
}) {
  const { t } = useTranslation();
  return (
    <Card className="ag-dashboard-metric h-[150px] 2xl:h-[156px]">
      <Card.Content className="flex h-full flex-col p-3 2xl:p-3.5">
        <div className="flex min-w-0 items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="h-5 truncate text-sm font-semibold leading-5 tracking-normal text-text-tertiary">
              {t('monitor.runtime_event_counts')}
            </div>
          </div>
          <MetricIcon
            icon={<AlertTriangle className="h-5 w-5" />}
            tone="bg-rose-100 text-rose-600 ring-rose-200 dark:bg-rose-400/15 dark:text-rose-300 dark:ring-rose-400/25"
          />
        </div>
        <div className="mt-auto grid min-h-0 grid-cols-2 gap-1.5">
          <SummaryMiniStat
            label={t('monitor.critical')}
            tone="bg-black text-white ring-black dark:bg-black dark:text-white dark:ring-zinc-600"
            value={summaryValue(summary?.critical_active_total ?? 0, summary?.critical_total ?? 0, showActiveCounts)}
          />
          <SummaryMiniStat
            label={t('monitor.error')}
            tone="bg-rose-100 text-rose-600 ring-rose-200 dark:bg-rose-400/15 dark:text-rose-300 dark:ring-rose-400/25"
            value={summaryValue(summary?.error_active_total ?? 0, summary?.error_total ?? 0, showActiveCounts)}
          />
          <SummaryMiniStat
            label={t('monitor.warning')}
            tone="bg-amber-100 text-amber-600 ring-amber-200 dark:bg-amber-400/15 dark:text-amber-300 dark:ring-amber-400/25"
            value={fmtNum(summary?.warning_total ?? 0)}
          />
          <SummaryMiniStat
            label={t('monitor.severity_info')}
            tone="bg-sky-100 text-sky-600 ring-sky-200 dark:bg-sky-400/15 dark:text-sky-300 dark:ring-sky-400/25"
            value={fmtNum(summary?.info_total ?? 0)}
          />
        </div>
      </Card.Content>
    </Card>
  );
}

export function MonitorRuntimeStats({
  showActiveCounts = true,
  snapshot,
  summary,
}: {
  showActiveCounts?: boolean;
  snapshot?: MonitorRuntimeResp;
  summary?: MonitorSummaryResp;
}) {
  const { t } = useTranslation();
  const latency = snapshot?.latency;
  const latency1H = snapshot?.latency_1h;
  const capacity = snapshot?.capacity;
  const postgres = snapshot?.dependencies?.postgres;
  const redis = snapshot?.dependencies?.redis;
  const runtime = snapshot?.runtime;

  const dependencyStatus = (healthy?: boolean) => (healthy ? t('monitor.runtime_ok') : t('monitor.runtime_down'));
  const latencyFRTValue = [
    t('monitor.runtime_frt_avg'),
    formatDurationPairWithDelta(latency?.frt_avg_ms, latency1H?.frt_avg_ms),
  ].join(' ');
  const latencySamples = `${t('monitor.runtime_samples')} ${fmtNum(latency?.sample_count ?? 0)}/${fmtNum(latency1H?.sample_count ?? 0)}`;
  const latencyErrors = `${t('monitor.runtime_errors')} ${formatPercent(latency?.error_rate)}/${formatPercent(latency1H?.error_rate)}`;

  return (
    <div className="mb-6 grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4 2xl:gap-4">
      <MonitorSummaryCard showActiveCounts={showActiveCounts} summary={summary} />
      <RuntimeCard
        details={[
          `P95 ${formatDurationPairWithDelta(latency?.frt_p95_ms, latency1H?.frt_p95_ms)}`,
          `P99 ${formatDurationPairWithDelta(latency?.frt_p99_ms, latency1H?.frt_p99_ms)}`,
          joinDetail([
            latencySamples,
            latencyErrors,
          ]),
        ]}
        icon={<Activity className="h-5 w-5" />}
        label={t('monitor.runtime_latency')}
        meta={joinDetail([
          `P50 ${formatDurationPairWithDelta(latency?.frt_p50_ms, latency1H?.frt_p50_ms)}`,
          ...(latency?.stale || latency1H?.stale ? [t('monitor.runtime_stale')] : []),
        ])}
        tone="bg-sky-100 text-sky-700 ring-sky-200 dark:bg-sky-400/15 dark:text-sky-300 dark:ring-sky-400/25"
        value={latencyFRTValue}
      />
      <RuntimeCard
        details={[
          joinDetail([
            `PG ${postgres?.active ?? 0}/${postgres?.open ?? 0}`,
            `Redis ${redis?.active ?? 0}/${redis?.total ?? 0}`,
          ]),
          joinDetail([
            `PG wait +${fmtNum(postgres?.wait_count_delta ?? 0)}`,
            `Redis timeout +${fmtNum(redis?.timeout_delta ?? 0)}`,
          ]),
        ]}
        icon={<Database className="h-5 w-5" />}
        label={t('monitor.runtime_dependencies')}
        meta={joinDetail([
          `PG ${formatPing(postgres?.healthy, postgres?.ping_ms)}`,
          `Redis ${formatPing(redis?.healthy, redis?.ping_ms)}`,
        ])}
        tone="bg-emerald-100 text-emerald-700 ring-emerald-200 dark:bg-emerald-400/15 dark:text-emerald-300 dark:ring-emerald-400/25"
        value={`PG ${dependencyStatus(postgres?.healthy)} Redis ${dependencyStatus(redis?.healthy)}`}
      />
      <RuntimeCard
        details={[
          joinDetail([
            `${t('monitor.runtime_capacity')} ${ratioText(capacity?.account_in_use, capacity?.account_capacity)}`,
            `${t('monitor.runtime_working')} ${fmtNum(capacity?.working_accounts ?? 0)}`,
          ]),
          joinDetail([
            `${t('monitor.runtime_waiters')} ${fmtNum(capacity?.message_waiters ?? 0)} (p-max ${fmtNum(capacity?.max_account_waiters ?? 0)})`,
            `reject +${fmtNum(capacity?.concurrency_reject_delta ?? 0)}`,
          ]),
          joinDetail([
            `billing ${runtime?.billing_queue_len ?? 0}/${runtime?.billing_queue_cap ?? 0}`,
            `monitor ${runtime?.monitor_queue_len ?? 0}/${runtime?.monitor_queue_cap ?? 0}`,
          ]),
        ]}
        icon={<Cpu className="h-5 w-5" />}
        label={t('monitor.runtime_process')}
        meta={joinDetail([
          `CPU ${formatCPU(runtime?.cpu_percent)}`,
          `heap ${formatBytes(runtime?.heap_alloc_bytes)}`,
        ])}
        tone="bg-violet-100 text-violet-700 ring-violet-200 dark:bg-violet-400/15 dark:text-violet-300 dark:ring-violet-400/25"
        value={`${fmtNum(runtime?.goroutines ?? 0)} goroutines`}
      />
    </div>
  );
}
