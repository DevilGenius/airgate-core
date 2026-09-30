import { Panel } from '../shared/components/Panel';
import { TokenTrendChart } from '../shared/charts/TokenTrendChart';
import { TimeSeriesChart } from '../shared/charts/TimeSeriesChart';
import { buildAPIKeyTrendModel } from '../shared/charts/apiKeyTrend';
import { Fragment, useMemo, useState, type CSSProperties, type ReactNode } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Card } from '../shared/components/Card';
import { Alert, Button, Skeleton, Tabs } from '@heroui/react';
import {
  Activity,
  Astroid,
  Bell,
  Calculator,
  CalendarDays,
  Clock,
  KeyRound,
  MoveDown,
  MoveRight,
  MoveUp,
  RefreshCw,
  ToggleRight,
  Zap,
} from 'lucide-react';
import { dashboardApi } from '../shared/api/dashboard';
import { queryKeys } from '../shared/queryKeys';
import { DISTRIBUTION_COLORS } from '../shared/constants';
import { AutoRefreshControl } from '../shared/components/AutoRefreshControl';
import { CompactDataTable } from '../shared/components/CompactDataTable';
import { CostPair, CostValue } from '../shared/components/CostValue';
import { SimpleSelect } from '../shared/components/SimpleSelect';
import { UserSearchFilterComboBox } from '../shared/components/UserSearchFilterComboBox';
import { usePersistentAutoRefresh } from '../shared/hooks/usePersistentAutoRefresh';
import { STORAGE_KEYS } from '../shared/storageKeys';
import { type MetricTone, METRIC_TONE_CLASSES, METRIC_TONE_STYLES } from '../shared/ui/metricTones';
import type { DashboardStatsResp, DashboardTrendResp, DashboardUsageEstimate } from '../shared/types';

const DISTRIBUTION_DOT_COLORS = DISTRIBUTION_COLORS;
const DASHBOARD_AUTO_REFRESH_STORAGE_KEY = STORAGE_KEYS.ui.adminDashboardAutoRefresh;
const DASHBOARD_AUTO_REFRESH_OPTIONS = [0, 5, 15, 30] as const;

type RangePreset = 'today' | '7d' | '30d' | '90d';
type Granularity = 'hour' | 'day';

const RANGE_PRESETS = ['today', '7d', '30d', '90d'] as const;
type MetaTone = 'default' | 'success' | 'warning' | 'danger' | 'accent';

const META_TONE_CLASSES: Record<MetaTone, string> = {
  accent: 'text-primary',
  danger: 'text-danger',
  default: 'text-text',
  success: 'text-emerald-600 dark:text-emerald-400',
  warning: 'text-amber-600 dark:text-amber-400',
};


function fmtNum(n: number | undefined | null): string {
  if (n == null) return '0';
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(2)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(2)}K`;
  return n.toLocaleString();
}

function fmtDurationMs(ms: number | undefined | null): string {
  if (ms == null || ms <= 0) return '0s';
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const seconds = ms / 1000;
  if (seconds >= 100) return `${Math.round(seconds)}s`;
  return `${seconds.toFixed(seconds >= 10 ? 1 : 2)}s`;
}

/** 速率指标紧凑格式化：最多 1 位小数（截断防进位），整数省略 .0 */
function fmtRate(n: number | undefined | null): string {
  if (n == null || n <= 0) return '0';
  const units: Array<[divisor: number, suffix: string]> = [
    [1_000_000_000, 'B'],
    [1_000_000, 'M'],
    [1_000, 'K'],
  ];
  for (const [divisor, suffix] of units) {
    if (n >= divisor) {
      const truncated = Math.floor((n / divisor) * 10) / 10;
      return `${Number.isInteger(truncated) ? truncated.toFixed(0) : truncated.toFixed(1)}${suffix}`;
    }
  }
  return String(Math.floor(n));
}

function fmtCoefficient(n: number | undefined | null): string {
  if (n == null || n <= 0) return '0.00';
  return n.toFixed(2);
}

export function fmtCostPerMinute(value: number | undefined | null): string {
  if (value == null || !Number.isFinite(value) || value <= 0) return '$0';
  return `$${value.toLocaleString(undefined, { maximumFractionDigits: 2 })}`;
}

export function fmtUsageEstimateDuration(minutes: number | undefined): string {
  if (minutes == null || !Number.isFinite(minutes) || minutes < 0) return '';
  const totalMinutes = Math.round(minutes);
  // 超过 1000h 时封顶显示 ">1000h"，避免超长文本撑破卡片布局。
  if (totalMinutes > 1000 * 60) return '>1000h';
  const hours = Math.floor(totalMinutes / 60);
  const remainingMinutes = totalMinutes % 60;
  if (hours <= 0) return `${remainingMinutes}m`;
  if (remainingMinutes === 0) return `${hours}h`;
  return `${hours}h${remainingMinutes}m`;
}

export function fmtUsageEstimateCost(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '$0';
  if (value < 1000) return `$${Math.round(value)}`;
  // 千位以上按 K/M/B 归一化（保留一位小数，整数去尾零），控制卡片内容宽度。
  const units: Array<{ divisor: number; suffix: string }> = [
    { divisor: 1e9, suffix: 'B' },
    { divisor: 1e6, suffix: 'M' },
    { divisor: 1e3, suffix: 'K' },
  ];
  for (const unit of units) {
    if (value < unit.divisor) continue;
    const scaled = Math.round((value / unit.divisor) * 10) / 10;
    // 四舍五入撞上下一档边界（如 999.95K → 1000.0K）时晋升到更大单位。
    if (scaled < 1000) {
      return `$${Number.isInteger(scaled) ? scaled : scaled.toFixed(1)}${unit.suffix}`;
    }
  }
  return `$${Math.round(value / 1e9)}B`;
}

/** 金额数字继承文字颜色，$ 默认绿色，也可单独指定颜色。 */
function GreenCost({ text, symbolClassName = 'text-success' }: { text: string; symbolClassName?: string }) {
  if (!text.startsWith('$')) return <>{text}</>;
  return (
    <>
      <span className={symbolClassName}>$</span>
      {text.slice(1)}
    </>
  );
}

/** 总量和可用时间使用卡片主字号，5h 小计与相邻卡片金额字体一致。 */
export function UsageEstimateCell({ estimate }: { estimate?: DashboardUsageEstimate }) {
  const { t } = useTranslation();
  const total = estimate?.total;
  const fiveHour = estimate?.five_hour;
  if (total?.status !== 'ready' || total.remaining_cost == null) {
    return <span className="font-sans text-xs font-semibold text-text">{t('dashboard.usage_estimate_insufficient')}</span>;
  }
  const duration = total.remaining_minutes == null
    ? '>1000h'
    : fmtUsageEstimateDuration(total.remaining_minutes);
  const cost = fmtUsageEstimateCost(total.remaining_cost);
  if (!duration) return <span className="font-sans text-xs font-semibold text-text">{t('dashboard.usage_estimate_insufficient')}</span>;
  return (
    <span className="inline-flex items-baseline gap-x-1.5 whitespace-nowrap font-mono font-semibold leading-none text-text">
      <span className="ag-dashboard-metric-value text-xl leading-none"><GreenCost text={cost} /></span>{' '}
      <span className="font-sans text-xs font-semibold">
        <GreenCost text={fiveHour?.status === 'ready' && fiveHour.remaining_cost != null
          ? fmtUsageEstimateCost(fiveHour.remaining_cost) : '-'} />
      </span>{' '}
      <span className="ag-dashboard-metric-value text-xl leading-none">{duration}</span>
    </span>
  );
}

function MetricCard({
  icon,
  meta,
  metaTone = 'default',
  title,
  tone,
  value,
  valueSuffix,
}: {
  icon: ReactNode;
  meta: ReactNode;
  metaTone?: MetaTone;
  title: string;
  tone: MetricTone;
  value: ReactNode;
  valueSuffix?: string;
}) {
  return (
    <Card density="compact" className="ag-dashboard-metric min-h-[72px]">
      <Card.Content className="ag-dashboard-metric-content p-3">
        <div className="ag-dashboard-metric-copy flex flex-col self-stretch">
          <div className="flex h-5 min-w-0 items-center truncate text-sm font-semibold tracking-normal text-text">{title}</div>
          <div className="mt-auto flex min-w-0 items-baseline gap-2 pt-1">
            <div className="ag-dashboard-metric-value flex min-w-0 items-baseline font-mono text-xl font-semibold leading-none text-text">
              {value}
              {valueSuffix ? <span className="ml-1.5 text-[11px] font-medium leading-none text-text">{valueSuffix}</span> : null}
            </div>
            <div className={`min-w-0 truncate text-xs font-semibold ${META_TONE_CLASSES[metaTone]}`}>{meta}</div>
          </div>
        </div>
        <span
          className={`ag-dashboard-metric-badge h-11 w-11 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1 shadow-sm ${METRIC_TONE_CLASSES[tone]}`}
          style={METRIC_TONE_STYLES[tone]}
        >
          {icon}
        </span>
      </Card.Content>
    </Card>
  );
}

/** 指标徽章的高位告警色（红/黑），RPM 负载徽章与用量估算 Bell 徽章共用，保证语义一致。 */
const METRIC_BADGE_DANGER_CLASS = 'bg-red-100 text-red-600 ring-red-200 dark:bg-red-400/15 dark:text-red-300 dark:ring-red-400/25';
const METRIC_BADGE_CRITICAL_CLASS = 'bg-zinc-900 text-white ring-zinc-700 dark:bg-zinc-950 dark:text-zinc-100 dark:ring-zinc-700';

/** RPM 负载等级对应的图标徽章配色：0 灰，≤50 同账号卡片(emerald)，≤200 同总Token卡片(stream)，≤500 同今日Token卡片(amber)，≤1000 红，>1000 黑 */
const RPM_BADGE_LEVELS: Array<{ max: number; className: string; style?: CSSProperties }> = [
  { max: 0, className: METRIC_TONE_CLASSES.gray, style: METRIC_TONE_STYLES.gray },
  { max: 50, className: METRIC_TONE_CLASSES.emerald, style: METRIC_TONE_STYLES.emerald },
  { max: 200, className: METRIC_TONE_CLASSES.stream, style: METRIC_TONE_STYLES.stream },
  { max: 500, className: METRIC_TONE_CLASSES.amber },
  { max: 1000, className: METRIC_BADGE_DANGER_CLASS },
];
const RPM_BADGE_MAX_CLASS = METRIC_BADGE_CRITICAL_CLASS;

function rpmBadge(rpm: number): { className: string; style?: CSSProperties } {
  for (const level of RPM_BADGE_LEVELS) {
    if (rpm <= level.max) return level;
  }
  return { className: RPM_BADGE_MAX_CLASS };
}

/** 用量估算 Bell 徽章配色：按所有非 Free 账号的短期可用总量分档。 */
const USAGE_ESTIMATE_BADGE_LEVELS: Array<{ maxExclusive: number; className: string }> = [
  { maxExclusive: 100, className: METRIC_BADGE_CRITICAL_CLASS },
  { maxExclusive: 200, className: METRIC_BADGE_DANGER_CLASS },
  { maxExclusive: 300, className: METRIC_TONE_CLASSES.amber },
];

export function usageEstimateBadgeClass(remainingCost: number | undefined): string {
  if (remainingCost == null || !Number.isFinite(remainingCost)) return METRIC_TONE_CLASSES.violet;
  for (const level of USAGE_ESTIMATE_BADGE_LEVELS) {
    if (remainingCost < level.maxExclusive) return level.className;
  }
  return METRIC_TONE_CLASSES.violet;
}

function PerformanceMetricCard({
  icon,
  rpm1m,
  rpm10m,
  title,
  tpm1m,
  tpm10m,
}: {
  icon: ReactNode;
  rpm1m: number;
  rpm10m: number;
  title: string;
  tpm1m: number;
  tpm10m: number;
}) {
  const { t } = useTranslation();
  const rpmTexts = [fmtRate(rpm1m), fmtRate(rpm10m)];
  const tpmTexts = [fmtRate(tpm1m), fmtRate(tpm10m)];
  const rpm1mInteger = Math.trunc(rpm1m);
  const rpm10mInteger = Math.trunc(rpm10m);
  const rpmTrend = rpm1mInteger > rpm10mInteger ? 'up' : rpm1mInteger < rpm10mInteger ? 'down' : 'flat';
  const badge = rpmBadge(rpm1m);
  return (
    <Card density="compact" className="ag-dashboard-metric min-h-[72px]">
      <Card.Content className="ag-dashboard-metric-content p-3">
        <div className="ag-dashboard-metric-copy flex flex-col self-stretch">
          <div className="flex h-5 min-w-0 items-center gap-1 text-sm font-semibold tracking-normal text-text">
            <span className="truncate">{title}</span>
            {rpmTrend === 'up' ? (
              <MoveUp className="h-3.5 w-3.5 shrink-0 text-success" />
            ) : rpmTrend === 'down' ? (
              <MoveDown className="h-3.5 w-3.5 shrink-0 text-danger" />
            ) : (
              <MoveRight className="h-3.5 w-3.5 shrink-0 text-black" />
            )}
          </div>
          <div className="mt-auto flex min-w-0 items-baseline gap-x-2 whitespace-nowrap pt-1">
            {rpmTexts.map((rpmText, index) => (
              <Fragment key={index}>
                {index > 0 ? (
                  <span aria-hidden="true" className="font-mono text-base leading-none text-text">/</span>
                ) : null}
                <span className="flex items-baseline gap-x-1.5">
                  <span className="flex items-baseline gap-1">
                    <span className="ag-dashboard-metric-value font-mono text-xl font-semibold leading-none text-text">
                      {rpmText}
                    </span>
                    <span className="text-[11px] font-medium leading-none text-text">{t('dashboard.rpm')}</span>
                  </span>
                  <span className="flex items-baseline gap-1">
                    <span className="font-mono text-sm font-semibold leading-none text-text">
                      {tpmTexts[index]}
                    </span>
                    <span className="text-[11px] font-medium leading-none text-text">{t('dashboard.tpm')}</span>
                  </span>
                </span>
              </Fragment>
            ))}
          </div>
        </div>
        <span
          className={`ag-dashboard-metric-badge h-11 w-11 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1 shadow-sm ${badge.className}`}
          style={badge.style}
        >
          {icon}
        </span>
      </Card.Content>
    </Card>
  );
}

function StatsSkeleton() {
  return (
    <div className="ag-dashboard-metrics-grid grid auto-rows-fr gap-3">
      {Array.from({ length: 8 }).map((_, index) => (
        <Card density="compact" className="ag-dashboard-metric min-h-[72px]" key={index}>
          <Card.Content className="ag-dashboard-metric-content p-3">
            <div className="ag-dashboard-metric-copy space-y-2">
              <Skeleton className="h-3 w-24" />
              <div className="flex items-baseline gap-2">
                <Skeleton className="h-6 w-24" />
                <Skeleton className="h-3 w-32" />
              </div>
            </div>
            <Skeleton className="ag-dashboard-metric-badge ag-dashboard-metric-badge-skeleton h-11 w-11 shrink-0 rounded-[var(--field-radius)]" />
          </Card.Content>
        </Card>
      ))}
    </div>
  );
}

function StatsCards({ stats }: { stats: DashboardStatsResp }) {
  const { t } = useTranslation();
  const todayImageRequests = stats.today_image_requests ?? 0;
  const todayTextRequests = Math.max(0, (stats.today_requests ?? 0) - todayImageRequests);
  const usageEstimate = stats.usage_estimate;
  const total = usageEstimate?.total;
  const totalRemainingCost = total?.status === 'ready' ? total.remaining_cost : undefined;
  return (
    <div className="ag-dashboard-metrics-grid grid auto-rows-fr gap-3">
      <Card density="compact" className="ag-dashboard-metric min-h-[72px]">
        <Card.Content className="ag-dashboard-metric-content p-3">
        <div className="ag-dashboard-metric-copy flex flex-col self-stretch">
          <div className="flex h-5 min-w-0 items-center truncate text-sm font-semibold tracking-normal text-text">
            {t('dashboard.users_summary', { active: stats.active_users, total: stats.total_users })} {t('dashboard.new_users', { count: stats.new_users_today })}
          </div>
          <div className="mt-auto flex min-w-0 items-baseline gap-x-2 whitespace-nowrap pt-1">
              <span className="flex items-baseline gap-1">
                <span className="ag-dashboard-metric-value font-mono text-xl font-semibold leading-none text-text">{stats.enabled_api_keys}</span>
                <span className="text-xs font-semibold leading-none text-text">{t('dashboard.enabled_label')}</span>
              </span>
              <span className="flex items-baseline gap-1">
                <span className="ag-dashboard-metric-value font-mono text-xl font-semibold leading-none text-text">{stats.total_api_keys}</span>
                <span className="text-xs font-semibold leading-none text-text">{t('dashboard.total_label')}</span>
              </span>
            </div>
          </div>
          <span
            className={`ag-dashboard-metric-badge h-11 w-11 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1 shadow-sm ${METRIC_TONE_CLASSES.gray}`}
            style={METRIC_TONE_STYLES.gray}
          >
            <KeyRound className="h-5 w-5" />
          </span>
        </Card.Content>
      </Card>
      <MetricCard
        icon={<ToggleRight className="h-5 w-5" />}
        tone="emerald"
        title={t('dashboard.accounts')}
        value={stats.total_accounts}
        meta={t('dashboard.accounts_status', { closed: stats.closed_accounts, enabled: stats.enabled_accounts, errors: stats.error_accounts })}
      />
      <MetricCard
        icon={<Activity className="h-5 w-5" />}
        tone="cyan"
        title={t('dashboard.today_requests')}
        value={`${fmtNum(todayTextRequests)}/${fmtNum(todayImageRequests)}`}
        valueSuffix={t('dashboard.image_suffix')}
        meta={t('dashboard.alltime_requests', { count: fmtNum(stats.alltime_requests) } as Record<string, string>)}
      />
      <MetricCard
        icon={<Clock className="h-5 w-5" />}
        tone="rose"
        title={t('dashboard.avg_response')}
        value={`${fmtDurationMs(stats.avg_first_event_ms)}/${fmtDurationMs(stats.avg_duration_ms)}`}
        meta={
          (stats.avg_image_duration_ms ?? 0) > 0
            ? t('dashboard.image_response_time', { time: fmtDurationMs(stats.avg_image_duration_ms) })
            : undefined
        }
      />
      <PerformanceMetricCard
        icon={<Zap className="h-5 w-5" />}
        title={t('dashboard.performance_window', {
          coefficient1m: fmtCoefficient(stats.tpm_per_rpm_coefficient_1m),
          coefficient10m: fmtCoefficient(stats.tpm_per_rpm_coefficient_10m),
        })}
        rpm1m={stats.rpm_1m ?? 0}
        tpm1m={stats.tpm_1m ?? 0}
        rpm10m={stats.rpm_10m ?? 0}
        tpm10m={stats.tpm_10m ?? 0}
      />
      <Card density="compact" className="ag-dashboard-metric min-h-[72px]">
        <Card.Content className="ag-dashboard-metric-content p-3">
          <div className="ag-dashboard-metric-copy flex flex-col self-stretch">
            <div className="flex h-5 min-w-0 items-center truncate text-sm font-semibold tracking-normal text-text">
              {t('dashboard.usage_estimate')} (1min-<GreenCost text={fmtCostPerMinute(stats.account_cost_per_minute_1m)} />/10min-<GreenCost text={fmtCostPerMinute(stats.account_cost_per_minute_10m)} />)
            </div>
            <div className="mt-auto flex min-w-0 items-baseline whitespace-nowrap pt-1">
              <UsageEstimateCell estimate={usageEstimate} />
            </div>
          </div>
          <span
            className={`ag-dashboard-metric-badge h-11 w-11 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1 shadow-sm ${usageEstimateBadgeClass(totalRemainingCost)}`}
          >
            <Bell className="h-5 w-5" />
          </span>
        </Card.Content>
      </Card>
      <MetricCard
        icon={<Astroid className="h-5 w-5" />}
        tone="amber"
        title={t('dashboard.today_tokens')}
        value={fmtNum(stats.today_tokens)}
        meta={<CostPair actual={stats.today_cost} standard={stats.today_standard_cost} />}
      />
      <MetricCard
        icon={<Calculator className="h-5 w-5" />}
        tone="stream"
        title={t('dashboard.total_tokens')}
        value={fmtNum(stats.alltime_tokens)}
        meta={<CostPair actual={stats.alltime_cost} standard={stats.alltime_standard_cost} />}
      />
    </div>
  );
}

type DashboardDistributionTableRow = {
  actualCost: number;
  key: string | number;
  name: string;
  requests: number;
  standardCost: number;
  tokens: number;
};

const DASHBOARD_DISTRIBUTION_COLUMN_WIDTHS = {
  name: '32%',
  requests: '16%',
  tokens: '18%',
  actual: '17%',
  standard: '17%',
} as const;

function ModelDistributionCard({ trend }: { trend: DashboardTrendResp }) {
  const { t } = useTranslation();
  const [tab, setTab] = useState<'model' | 'user'>('model');
  const models = trend.model_distribution ?? [];
  const users = trend.user_ranking ?? [];
  const activeTitle = tab === 'model' ? t('dashboard.model_distribution') : t('dashboard.user_ranking');
  const tableRows: DashboardDistributionTableRow[] = useMemo(
    () => (
      tab === 'model'
        ? models.map((item, index) => ({
            actualCost: item.actual_cost,
            key: item.model || index,
            name: item.model,
            requests: item.requests,
            standardCost: item.standard_cost,
            tokens: item.tokens,
          }))
        : users.map((item, index) => ({
            actualCost: item.actual_cost,
            key: item.user_id || index,
            name: item.email,
            requests: item.requests,
            standardCost: item.standard_cost,
            tokens: item.tokens,
          }))
    ),
    [models, tab, users],
  );
  const firstColumnTitle = tab === 'model' ? t('dashboard.model') : t('dashboard.email');
  const distributionTabs = (
    <Tabs className="ag-segmented-tabs ag-segmented-tabs-compact" selectedKey={tab} onSelectionChange={(key) => setTab(key as 'model' | 'user')}>
      <Tabs.List>
        <Tabs.Tab id="model">
          <Tabs.Indicator />
          <span>{t('dashboard.model_distribution')}</span>
        </Tabs.Tab>
        <Tabs.Tab id="user">
          <Tabs.Separator />
          <Tabs.Indicator />
          <span>{t('dashboard.user_ranking')}</span>
        </Tabs.Tab>
      </Tabs.List>
    </Tabs>
  );

  return (
    <Panel title={activeTitle} extra={distributionTabs}>
      <div className="ag-distribution-table-scroll">
        <CompactDataTable
          ariaLabel={activeTitle}
          className="ag-compact-data-table--dense"
          emptyText={t('common.no_data')}
          minWidth={480}
          rowKey={(row) => row.key}
          rows={tableRows}
          columns={[
            {
              key: 'name',
              title: firstColumnTitle,
              width: DASHBOARD_DISTRIBUTION_COLUMN_WIDTHS.name,
              render: (row, index) => (
                <>
                  <span className="shrink-0 font-mono text-[11px] font-semibold text-text">#{index + 1}</span>
                  <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: DISTRIBUTION_DOT_COLORS[index % DISTRIBUTION_DOT_COLORS.length] }} />
                  <span className="min-w-0 truncate font-medium text-text" title={row.name}>{row.name}</span>
                </>
              ),
            },
            {
              align: 'end',
              key: 'requests',
              title: t('dashboard.requests'),
              width: DASHBOARD_DISTRIBUTION_COLUMN_WIDTHS.requests,
              render: (row) => <span className="truncate font-mono text-text">{row.requests.toLocaleString()}</span>,
            },
            {
              align: 'end',
              key: 'tokens',
              title: t('dashboard.tokens'),
              width: DASHBOARD_DISTRIBUTION_COLUMN_WIDTHS.tokens,
              render: (row) => <span className="truncate font-mono text-text">{fmtNum(row.tokens)}</span>,
            },
            {
              align: 'end',
              key: 'actual',
              title: t('dashboard.actual'),
              width: DASHBOARD_DISTRIBUTION_COLUMN_WIDTHS.actual,
              render: (row) => <CostValue className="truncate font-mono" value={row.actualCost} tone="actual" />,
            },
            {
              align: 'end',
              key: 'standard',
              title: t('dashboard.standard'),
              width: DASHBOARD_DISTRIBUTION_COLUMN_WIDTHS.standard,
              render: (row) => <CostValue className="truncate font-mono" value={row.standardCost} tone="standard" />,
            },
          ]}
        />
      </div>
    </Panel>
  );
}

function TokenTrendCard({ trend }: { trend: DashboardTrendResp }) {
  const { t } = useTranslation();
  const data = useMemo(() => (trend.token_trend ?? []).map(item => ({
    time: item.time, input: item.input_tokens, output: item.output_tokens,
    cacheRead: item.cached_input, cacheCreation: item.cache_creation,
    actualCost: item.actual_cost, standardCost: item.standard_cost,
  })), [trend.token_trend]);
  return (
    <Panel title={t('dashboard.token_trend')}>
      <div className="ag-dashboard-token-trend-chart h-[248px] w-full min-w-0">
        {data.length > 0 ? <TokenTrendChart data={data} /> : (
          <div className="flex h-full items-center justify-center text-sm text-text">{t('common.no_data')}</div>
        )}
      </div>
    </Panel>
  );
}

function TopAPIKeysCard({ trend }: { trend: DashboardTrendResp }) {
  const { t } = useTranslation();
  const model = useMemo(() => buildAPIKeyTrendModel(trend.top_api_keys ?? [], t('usage.api_key_plugin_call')), [trend.top_api_keys, t]);
  return (
    <Panel title={t('dashboard.top_api_keys')}>
      <div className="ag-dashboard-api-key-trend-chart h-[268px] w-full min-w-0">
        {model.times.length > 0 ? <TimeSeriesChart model={model} label={t('dashboard.top_api_keys')} /> : (
          <div className="flex h-full items-center justify-center text-sm text-text">{t('common.no_data')}</div>
        )}
      </div>
    </Panel>
  );
}

function TrendCharts({ trend }: { trend: DashboardTrendResp }) {
  return (
    <div className="ag-dashboard-trends">
      <div className="ag-dashboard-trend-grid grid grid-cols-1 gap-4">
        <ModelDistributionCard trend={trend} />
        <TokenTrendCard trend={trend} />
      </div>
      <TopAPIKeysCard trend={trend} />
    </div>
  );
}

export default function DashboardPage() {
  const { t } = useTranslation();
  const [range, setRange] = useState<RangePreset>('today');
  const [granularity, setGranularity] = useState<Granularity>('day');
  const [selectedUserId, setSelectedUserId] = useState<number | undefined>();
  const [selectedUserLabel, setSelectedUserLabel] = useState('');
  const [autoRefresh, setAutoRefresh] = usePersistentAutoRefresh(DASHBOARD_AUTO_REFRESH_STORAGE_KEY, 0, DASHBOARD_AUTO_REFRESH_OPTIONS);
  const granularityOptions = [
    { id: 'day', label: t('dashboard.granularity_day') },
    { id: 'hour', label: t('dashboard.granularity_hour') },
  ];
  const isTodayRange = range === 'today';
  const selectedGranularity = isTodayRange ? 'hour' : granularity;
  const selectedGranularityLabel = granularityOptions.find((item) => item.id === selectedGranularity)?.label ?? '';
  const userFilter = selectedUserId ? { user_id: selectedUserId } : undefined;

  const statsQuery = useQuery({
    queryKey: queryKeys.dashboard(selectedUserId),
    queryFn: () => dashboardApi.stats(userFilter),
  });

  const trendParams = useMemo(() => ({
    range,
    granularity: isTodayRange ? 'hour' as const : granularity,
    ...(selectedUserId ? { user_id: selectedUserId } : {}),
  }), [range, isTodayRange, granularity, selectedUserId]);

  const trendQuery = useQuery({
    queryKey: queryKeys.dashboardTrend(trendParams),
    queryFn: () => dashboardApi.trend(trendParams),
    placeholderData: keepPreviousData,
  });

  const refresh = () => {
    statsQuery.refetch();
    trendQuery.refetch();
  };
  const isDashboardRefreshing = statsQuery.isFetching || trendQuery.isFetching;

  return (
    <div className="ag-dashboard-page">
      {statsQuery.error ? (
        <Alert status="danger">
          {t('dashboard.load_failed', { error: statsQuery.error instanceof Error ? statsQuery.error.message : '' })}
        </Alert>
      ) : null}

      {statsQuery.isLoading ? <StatsSkeleton /> : statsQuery.data ? <StatsCards stats={statsQuery.data} /> : null}

      <div className="ag-dashboard-toolbar py-1">
        <div className="ag-dashboard-toolbar-col-1">
          <Tabs
            className="ag-segmented-tabs ag-segmented-tabs-dashboard w-full"
            selectedKey={range}
            onSelectionChange={(key) => setRange(key as RangePreset)}
          >
            <Tabs.List className="w-full">
              {RANGE_PRESETS.map((item, index) => (
                <Tabs.Tab id={item} key={item} className="flex-1 min-w-0">
                  {index > 0 ? <Tabs.Separator /> : null}
                  <Tabs.Indicator />
                  <span>{t(`dashboard.range_${item}`)}</span>
                </Tabs.Tab>
              ))}
            </Tabs.List>
          </Tabs>
        </div>

        <div className="ag-dashboard-toolbar-col-3">
          <Button
            isIconOnly
            aria-label={t('common.refresh', 'Refresh')}
            isDisabled={isDashboardRefreshing}
            size="sm"
            variant="ghost"
            className="ag-auto-refresh-refresh--dashboard-compact shrink-0"
            onPress={refresh}
          >
            <RefreshCw className={`h-4 w-4 ${isDashboardRefreshing ? 'animate-spin' : ''}`} />
          </Button>
          <div className="ag-dashboard-toolbar-auto-refresh">
            <AutoRefreshControl
              value={autoRefresh}
              options={DASHBOARD_AUTO_REFRESH_OPTIONS}
              label={t('dashboard.auto_refresh')}
              offLabel={t('dashboard.auto_refresh_off')}
              showRefreshButton={false}
              triggerClassName="ag-auto-refresh-trigger--dashboard-compact w-full"
              ariaLabel={t('dashboard.auto_refresh')}
              refreshAriaLabel={t('common.refresh', 'Refresh')}
              onChange={setAutoRefresh}
              onAutoRefresh={refresh}
              onRefresh={refresh}
              isRefreshing={isDashboardRefreshing}
              isAutoRefreshing={isDashboardRefreshing}
              isAutoRefreshDisabled={!isTodayRange}
            />
          </div>
        </div>

        <div className="ag-dashboard-toolbar-col-4">
          <div className="ag-dashboard-toolbar-control">
            <UserSearchFilterComboBox
              ariaLabel={t('dashboard.all_users')}
              emptyPrompt={t('dashboard.all_users')}
              loadingLabel={t('common.loading')}
              noDataLabel={t('common.no_data')}
              placeholder={t('dashboard.all_users')}
              selectedKey={selectedUserId ? String(selectedUserId) : null}
              selectedLabel={selectedUserLabel}
              onSelectionChange={(value, label) => {
                if (!value) {
                  setSelectedUserId(undefined);
                  setSelectedUserLabel('');
                  return;
                }
                setSelectedUserId(Number(value));
                setSelectedUserLabel(label);
              }}
            />
          </div>

          <div className="ag-dashboard-toolbar-control">
            <SimpleSelect
              ariaLabel={t('dashboard.granularity')}
              fullWidth
              isDisabled={isTodayRange}
              items={granularityOptions.map((item) => ({ key: item.id, label: item.label }))}
              selectedKey={selectedGranularity}
              selectedLabel={(
                <span className="inline-flex min-w-0 items-center gap-1.5">
                  <CalendarDays className="h-3.5 w-3.5 shrink-0 text-text-tertiary" />
                  <span className="min-w-0 truncate">{selectedGranularityLabel}</span>
                </span>
              )}
              onSelectionChange={(key) => setGranularity(key as Granularity)}
            />
          </div>
        </div>
      </div>

      {trendQuery.isLoading && !trendQuery.data ? (
        <div className="ag-dashboard-trends">
          <div className="ag-dashboard-trend-grid grid grid-cols-1 gap-4">
            {Array.from({ length: 2 }).map((_, index) => (
              <Card className="ag-dashboard-panel" key={index}>
                <Card.Content>
                  <Skeleton className="ag-dashboard-token-trend-skeleton h-[280px] w-full" />
                </Card.Content>
              </Card>
            ))}
          </div>
          <Card className="ag-dashboard-panel">
            <Card.Content>
              <Skeleton className="ag-dashboard-api-key-trend-skeleton h-[300px] w-full" />
            </Card.Content>
          </Card>
        </div>
      ) : trendQuery.data ? (
        <TrendCharts trend={trendQuery.data} />
      ) : null}
    </div>
  );
}
