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

function formatCompactThousands(value?: number) {
  const normalized = Math.max(0, Math.trunc(value ?? 0));
  if (normalized < 1000) return `${normalized}`;
  const thousands = normalized / 1000;
  const precision = thousands >= 100 || Number.isInteger(thousands) ? 0 : 1;
  return `${thousands.toFixed(precision)}k`;
}

function formatRequestRejectionUsage(
  cybersecurityRisk?: number,
  invalidPrompt?: number,
  total?: number,
  capacity?: number,
) {
  const cyber = Math.max(0, Math.trunc(cybersecurityRisk ?? 0));
  const prompt = Math.max(0, Math.trunc(invalidPrompt ?? 0));
  const size = Math.max(0, Math.trunc(total ?? 0));
  const cap = Math.max(0, Math.trunc(capacity ?? 0));
  return `(${cyber}|${prompt})${size}/${cap}`;
}

function DetailSeparator() {
  return <span className="justify-self-center font-bold text-text-secondary">|</span>;
}

function SampleFailureColumn({
  currentErrorCount,
  currentErrorRate,
  currentSampleCount,
  longErrorCount,
  longErrorRate,
  longSampleCount,
}: {
  currentErrorCount?: number;
  currentErrorRate?: number;
  currentSampleCount?: number;
  longErrorCount?: number;
  longErrorRate?: number;
  longSampleCount?: number;
}) {
  const rows = [
    { errorCount: currentErrorCount, errorRate: currentErrorRate, sampleCount: currentSampleCount, window: '5m' },
    { errorCount: longErrorCount, errorRate: longErrorRate, sampleCount: longSampleCount, window: '1h' },
  ];
  return (
    <span className="grid min-w-0 grid-cols-[auto_auto_auto_auto_minmax(0,1fr)] items-center">
      {rows.map((row) => {
        const failures = Math.max(0, row.errorCount ?? 0);
        const effectiveSamples = Math.max(0, row.sampleCount ?? 0) + failures;
        return (
          <Fragment key={row.window}>
            <span className="whitespace-pre">{`${row.window}  `}</span>
            <span className="text-right tabular-nums">{failures}</span>
            <span className="justify-self-center">/</span>
            <span className="text-right tabular-nums">{effectiveSamples}</span>
            <span className="min-w-0 truncate tabular-nums">({formatPercent(row.errorRate)})</span>
          </Fragment>
        );
      })}
    </span>
  );
}

function SampleFailureDetails({
  latency,
  latency1H,
}: {
  latency?: MonitorRuntimeResp['latency'];
  latency1H?: MonitorRuntimeResp['latency_1h'];
}) {
  return (
    <span className="grid min-w-0 grid-cols-[minmax(0,1fr)_0.75rem_minmax(0,1fr)] items-stretch">
      <SampleFailureColumn
        currentErrorCount={latency?.text_error_count}
        currentErrorRate={latency?.text_error_rate}
        currentSampleCount={latency?.text_sample_count}
        longErrorCount={latency1H?.text_error_count}
        longErrorRate={latency1H?.text_error_rate}
        longSampleCount={latency1H?.text_sample_count}
      />
      <span className="grid grid-rows-2 items-center">
        <DetailSeparator />
        <DetailSeparator />
      </span>
      <SampleFailureColumn
        currentErrorCount={latency?.image_error_count}
        currentErrorRate={latency?.image_error_rate}
        currentSampleCount={latency?.image_sample_count}
        longErrorCount={latency1H?.image_error_count}
        longErrorRate={latency1H?.image_error_rate}
        longSampleCount={latency1H?.image_sample_count}
      />
    </span>
  );
}

function RuntimeMetric({ label, value }: { label: string; value: string }) {
  return (
    <span className="grid min-w-0 grid-cols-[minmax(0,1fr)_auto] items-center gap-1">
      <span className="min-w-0 overflow-hidden text-ellipsis whitespace-pre">{label}</span>
      <span className="whitespace-nowrap text-right tabular-nums">{value}</span>
    </span>
  );
}

function RuntimeCacheDetails({
  contextLabel,
  cyberLabel,
  encryptedLabel,
  imageLabel,
  promptLabel,
  runtime,
  textLabel,
}: {
  contextLabel: string;
  cyberLabel: string;
  encryptedLabel: string;
  imageLabel: string;
  promptLabel: string;
  runtime?: MonitorRuntimeResp['runtime'];
  textLabel: string;
}) {
  return (
    <span className="grid min-w-0 grid-cols-[minmax(0,1fr)_0.75rem_minmax(0,1fr)] items-center gap-y-0.5">
      <RuntimeMetric
        label={textLabel}
        value={formatRequestRejectionUsage(
          runtime?.text_rejection_cybersecurity_risk_len,
          runtime?.text_rejection_invalid_prompt_len,
          runtime?.text_rejection_cache_len,
          runtime?.text_rejection_cache_cap,
        )}
      />
      <DetailSeparator />
      <RuntimeMetric
        label={imageLabel}
        value={`${runtime?.image_rejection_cache_len ?? 0}/${runtime?.image_rejection_cache_cap ?? 0}`}
      />
      <RuntimeMetric
        label={cyberLabel}
        value={`${runtime?.cyber_rejection_cache_len ?? 0}/${runtime?.cyber_rejection_cache_cap ?? 0}`}
      />
      <DetailSeparator />
      <RuntimeMetric
        label={promptLabel}
        value={`${runtime?.prompt_rejection_cache_len ?? 0}/${runtime?.prompt_rejection_cache_cap ?? 0}`}
      />
      <RuntimeMetric
        label={encryptedLabel}
        value={`${Math.max(0, Math.trunc(runtime?.encrypted_content_cache_len ?? 0))}/${formatCompactThousands(runtime?.encrypted_content_cache_cap)}`}
      />
      <DetailSeparator />
      <RuntimeMetric
        label={contextLabel}
        value={`${Math.max(0, Math.trunc(runtime?.context_window_cache_len ?? 0))}/${formatCompactThousands(runtime?.context_window_cache_cap)}`}
      />
    </span>
  );
}

function RuntimeQueueDetails({
  avgWaitLabel,
  capacity,
  capacityLabel,
  enqueuedLabel,
  poolsLabel,
  rejectedLabel,
  timeoutLabel,
  waitersLabel,
  wokenLabel,
}: {
  avgWaitLabel: string;
  capacity?: MonitorRuntimeResp['capacity'];
  capacityLabel: string;
  enqueuedLabel: string;
  poolsLabel: string;
  rejectedLabel: string;
  timeoutLabel: string;
  waitersLabel: string;
  wokenLabel: string;
}) {
  return (
    <span className="grid min-w-0 grid-cols-[minmax(0,1fr)_0.75rem_minmax(0,1fr)] items-center gap-y-0.5">
      <RuntimeMetric
        label={capacityLabel}
        value={ratioText(capacity?.capacity_queue_waiters, capacity?.capacity_queue_max_total_waiters)}
      />
      <DetailSeparator />
      <RuntimeMetric
        label={enqueuedLabel}
        value={`+${fmtNum(capacity?.capacity_queue_enqueued_delta ?? 0)}`}
      />
      <RuntimeMetric
        label={poolsLabel}
        value={`${fmtNum(capacity?.capacity_queue_waiting_pools ?? 0)} (${fmtNum(capacity?.capacity_queue_max_pool_waiters ?? 0)}/${fmtNum(capacity?.capacity_queue_max_waiters_per_pool ?? 0)})`}
      />
      <DetailSeparator />
      <RuntimeMetric
        label={wokenLabel}
        value={`+${fmtNum(capacity?.capacity_queue_woken_delta ?? 0)}`}
      />
      <RuntimeMetric
        label={avgWaitLabel}
        value={formatDuration(capacity?.capacity_queue_wait_avg_ms)}
      />
      <DetailSeparator />
      <RuntimeMetric
        label={`${timeoutLabel}/${rejectedLabel}`}
        value={`+${fmtNum(capacity?.capacity_queue_timeout_delta ?? 0)}/+${fmtNum(capacity?.capacity_queue_rejected_delta ?? 0)}`}
      />
      <RuntimeMetric
        label={waitersLabel}
        value={`${fmtNum(capacity?.message_waiters ?? 0)} (${fmtNum(capacity?.max_account_waiters ?? 0)})`}
      />
      <DetailSeparator />
      <RuntimeMetric
        label="reject"
        value={`+${fmtNum(capacity?.concurrency_reject_delta ?? 0)}`}
      />
    </span>
  );
}

function joinDetail(parts: ReactNode[]) {
  if (parts.length <= 1) {
    return <span className="block min-w-0 truncate">{parts[0]}</span>;
  }

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
    <Card className="ag-dashboard-metric ag-monitor-runtime-card h-[190px]">
      <Card.Content className="flex h-full flex-col p-3">
        <div className="flex min-w-0 items-center justify-between gap-3">
          <div className="min-w-0">
            <div className="h-5 truncate text-sm font-semibold leading-5 tracking-normal text-text-tertiary">{label}</div>
            <div className="ag-monitor-runtime-value mt-1 h-6 min-w-0 truncate font-mono text-[21px] font-semibold leading-6 text-text">
              {value}
            </div>
          </div>
          <MetricIcon icon={icon} tone={tone} />
        </div>
        <div className="mt-auto min-w-0">
          <div className="mt-1 space-y-0.5 text-xs leading-4 text-text-tertiary">
            {meta ? <div className="min-w-0 overflow-hidden">{meta}</div> : null}
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
    <span className={`ag-overview-metric-badge h-11 w-11 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1 shadow-sm ${tone}`}>
      {icon}
    </span>
  );
}

function DependencyLatencyValue({
  postgresLatency,
  redisLatency,
}: {
  postgresLatency: string;
  redisLatency: string;
}) {
  return (
    <span className="inline-flex min-w-0 items-center gap-4 align-middle">
      <span className="min-w-0 truncate">
        PG {postgresLatency}
      </span>
      <span className="min-w-0 truncate">
        Redis {redisLatency}
      </span>
    </span>
  );
}

function summaryValue(active: number, total: number, showActiveRatio: boolean) {
  if (!showActiveRatio) return fmtNum(total);
  return `${fmtNum(active)} / ${fmtNum(total)}`;
}

function summaryWindowValue(shortTotal?: number, longTotal?: number) {
  const short = Math.max(0, Math.trunc(shortTotal ?? 0));
  const long = Math.max(0, Math.trunc(longTotal ?? 0));
  return `${short} / ${long}`;
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
    <Card className="ag-dashboard-metric ag-monitor-runtime-card h-[190px]">
      <Card.Content className="flex h-full flex-col p-3">
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
          <SummaryMiniStat
            label={`${t('monitor.warning')}(5m/1h)`}
            tone="bg-amber-100 text-amber-600 ring-amber-200 dark:bg-amber-400/15 dark:text-amber-300 dark:ring-amber-400/25"
            value={summaryWindowValue(summary?.warning_5m_total, summary?.warning_1h_total)}
          />
          <SummaryMiniStat
            label={`${t('monitor.severity_info')}(5m/1h)`}
            tone="bg-sky-100 text-sky-600 ring-sky-200 dark:bg-sky-400/15 dark:text-sky-300 dark:ring-sky-400/25"
            value={summaryWindowValue(summary?.info_5m_total, summary?.info_1h_total)}
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

  const latencyFRTValue = [
    t('monitor.runtime_frt_avg'),
    formatDurationPairWithDelta(latency?.frt_avg_ms, latency1H?.frt_avg_ms),
  ].join(' ');
  const percentileDetail = (
    percentile: 'P50' | 'P95' | 'P99',
    textCurrent?: number,
    textBaseline?: number,
    imageCurrent?: number,
    imageBaseline?: number,
  ) => joinDetail([
    `${percentile} ${formatDurationPairWithDelta(textCurrent, textBaseline)}`,
    `${percentile} ${formatDurationPairWithDelta(imageCurrent, imageBaseline)}`,
  ]);
  const stale = latency?.stale || latency1H?.stale;

  return (
    <div className="ag-overview-metrics-grid mb-6 grid gap-3">
      <MonitorSummaryCard showActiveCounts={showActiveCounts} summary={summary} />
      <RuntimeCard
        details={[
          percentileDetail(
            'P95',
            latency?.frt_p95_ms,
            latency1H?.frt_p95_ms,
            latency?.image_duration_p95_ms,
            latency1H?.image_duration_p95_ms,
          ),
          percentileDetail(
            'P99',
            latency?.frt_p99_ms,
            latency1H?.frt_p99_ms,
            latency?.image_duration_p99_ms,
            latency1H?.image_duration_p99_ms,
          ),
          <SampleFailureDetails
            latency={latency}
            latency1H={latency1H}
          />,
        ]}
        icon={<Activity className="h-5 w-5" />}
        label={`${t('monitor.runtime_latency')}${stale ? ` · ${t('monitor.runtime_stale')}` : ''}`}
        meta={percentileDetail(
          'P50',
          latency?.frt_p50_ms,
          latency1H?.frt_p50_ms,
          latency?.image_duration_p50_ms,
          latency1H?.image_duration_p50_ms,
        )}
        tone="bg-sky-100 text-sky-700 ring-sky-200 dark:bg-sky-400/15 dark:text-sky-300 dark:ring-sky-400/25"
        value={<span className="ag-monitor-latency-value text-[17px] tracking-tight">{latencyFRTValue}</span>}
      />
      <RuntimeCard
        details={[
          joinDetail([
            `${t('monitor.runtime_capacity')} ${ratioText(capacity?.account_in_use, capacity?.account_capacity)}`,
            `${t('monitor.runtime_working')} ${fmtNum(capacity?.working_accounts ?? 0)}`,
          ]),
          joinDetail([
            `billing ${runtime?.billing_queue_len ?? 0}/${runtime?.billing_queue_cap ?? 0}`,
            `monitor ${runtime?.monitor_queue_len ?? 0}/${runtime?.monitor_queue_cap ?? 0}`,
          ]),
          <RuntimeCacheDetails
            contextLabel={t('monitor.runtime_context_window_cache')}
            cyberLabel={t('monitor.runtime_cyber_rejection_cache')}
            encryptedLabel={t('monitor.runtime_encrypted_content_cache')}
            imageLabel={t('monitor.runtime_image_rejection_cache')}
            promptLabel={t('monitor.runtime_prompt_rejection_cache')}
            runtime={runtime}
            textLabel={t('monitor.runtime_text_rejection_cache')}
          />,
        ]}
        icon={<Cpu className="h-5 w-5" />}
        label={`${t('monitor.runtime_process')}(CPU ${formatCPU(runtime?.cpu_percent)}/heap ${formatBytes(runtime?.heap_alloc_bytes)})`}
        meta={null}
        tone="bg-violet-100 text-violet-700 ring-violet-200 dark:bg-violet-400/15 dark:text-violet-300 dark:ring-violet-400/25"
        value={`${fmtNum(runtime?.goroutines ?? 0)} goroutines`}
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
          <RuntimeQueueDetails
            avgWaitLabel={t('monitor.runtime_queue_wait_avg')}
            capacity={capacity}
            capacityLabel={t('monitor.runtime_capacity_queue')}
            enqueuedLabel={t('monitor.runtime_queue_enqueued')}
            poolsLabel={t('monitor.runtime_waiting_pools')}
            rejectedLabel={t('monitor.runtime_queue_rejected')}
            timeoutLabel={t('monitor.runtime_queue_timeout')}
            waitersLabel={t('monitor.runtime_waiters')}
            wokenLabel={t('monitor.runtime_queue_woken')}
          />,
        ]}
        icon={<Database className="h-5 w-5" />}
        label={t('monitor.runtime_dependencies')}
        meta={null}
        tone="bg-emerald-100 text-emerald-700 ring-emerald-200 dark:bg-emerald-400/15 dark:text-emerald-300 dark:ring-emerald-400/25"
        value={(
          <DependencyLatencyValue
            postgresLatency={formatPing(postgres?.healthy, postgres?.ping_ms)}
            redisLatency={formatPing(redis?.healthy, redis?.ping_ms)}
          />
        )}
      />
    </div>
  );
}
