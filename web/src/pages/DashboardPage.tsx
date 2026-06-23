import { useMemo, useState, type ReactNode } from 'react';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Card, Skeleton, Tabs } from '@heroui/react';
import {
  Activity,
  Astroid,
  CalendarDays,
  Clock,
  KeyRound,
  RefreshCw,
  Sigma,
  ToggleRight,
  Users,
  Zap,
} from 'lucide-react';
import {
  CartesianGrid,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip as RechartsTooltip,
  XAxis,
  YAxis,
} from 'recharts';
import { decorativePalette } from '@devilgenius/airgate-theme';
import { dashboardApi } from '../shared/api/dashboard';
import { queryKeys } from '../shared/queryKeys';
import { DISTRIBUTION_COLORS, USAGE_TOKEN_COLORS } from '../shared/constants';
import { CompactDataTable } from '../shared/components/CompactDataTable';
import { CostPair, CostValue } from '../shared/components/CostValue';
import { SimpleSelect } from '../shared/components/SimpleSelect';
import { UserSearchFilterComboBox } from '../shared/components/UserSearchFilterComboBox';
import { type MetricTone, METRIC_TONE_CLASSES, METRIC_TONE_STYLES } from '../shared/ui/metricTones';
import type { DashboardStatsResp, DashboardTrendResp } from '../shared/types';

const DISTRIBUTION_DOT_COLORS = DISTRIBUTION_COLORS;
const USER_COLORS = [...decorativePalette];
const TOKEN_TREND_LINE_ORDER: Array<keyof typeof USAGE_TOKEN_COLORS> = ['input', 'output', 'cacheCreation', 'cacheRead', 'cacheRatio', 'cacheCumulativeRatio'];
const TOKEN_TREND_RATIO_KEYS = new Set<keyof typeof USAGE_TOKEN_COLORS>(['cacheRatio', 'cacheCumulativeRatio']);
const DASHBOARD_TOKEN_TREND_INITIAL_DIMENSION = { width: 600, height: 248 };
const DASHBOARD_TOP_USERS_INITIAL_DIMENSION = { width: 1200, height: 268 };
const DASHBOARD_TOKEN_Y_AXIS_WIDTH = 56;
const DASHBOARD_RATIO_Y_AXIS_WIDTH = 36;
const DASHBOARD_TIME_AXIS_HEIGHT = 40;

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

function getUserTrendColor(index: number) {
  return USER_COLORS[index % USER_COLORS.length] ?? 'var(--ag-primary)';
}

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

type DashboardTimeLabel = {
  primary: string;
  secondary?: string;
  tooltip: string;
};

function formatDashboardTime(timeStr: string): DashboardTimeLabel {
  const [datePart, hourPart] = timeStr.split(' ');
  const dateParts = datePart?.split('-') ?? [];
  const compactDate = dateParts.length === 3 ? `${dateParts[1]}/${dateParts[2]}` : datePart;
  if (hourPart) {
    const hour = hourPart.slice(0, 5) || hourPart;
    return {
      primary: compactDate || timeStr,
      secondary: hour,
      tooltip: compactDate ? `${compactDate} ${hour}` : timeStr,
    };
  }
  return {
    primary: compactDate || timeStr,
    tooltip: compactDate || timeStr,
  };
}

function dashboardTooltipLabel(label: string | undefined, payload?: Array<{ payload?: unknown }>) {
  const datum = payload?.[0]?.payload;
  const timeLabel = datum && typeof datum === 'object' && 'timeLabel' in datum
    ? (datum as { timeLabel?: unknown }).timeLabel
    : undefined;
  if (timeLabel && typeof timeLabel === 'object' && 'tooltip' in timeLabel) {
    return String((timeLabel as DashboardTimeLabel).tooltip);
  }
  return label ?? '';
}

function DashboardTimeAxisTick({
  x = 0,
  y = 0,
  payload,
}: {
  x?: number;
  y?: number;
  payload?: { value?: string | number };
}) {
  const label = formatDashboardTime(String(payload?.value ?? ''));
  if (label.secondary) {
    return (
      <g transform={`translate(${x},${y + 8})`}>
        <text fill="var(--ag-text)" fontSize={10} textAnchor="middle">
          <tspan x={0} dy={0}>{label.primary}</tspan>
          <tspan x={0} dy={13}>{label.secondary}</tspan>
        </text>
      </g>
    );
  }
  return (
    <g transform={`translate(${x},${y + 10})`}>
      <text fill="var(--ag-text)" fontSize={11} textAnchor="middle">{label.primary}</text>
    </g>
  );
}

function DashboardCard({
  children,
  extra,
  title,
}: {
  children: ReactNode;
  extra?: ReactNode;
  title?: string;
}) {
  const hasHeader = Boolean(title || extra);

  return (
    <Card className="ag-dashboard-panel">
      {hasHeader ? (
        <div
          className={`flex items-center gap-3 p-3 pb-2 2xl:p-4 2xl:pb-2 ${title ? 'justify-between' : 'justify-end'}`}
        >
          {title ? <h3 className="text-base font-semibold leading-none text-text">{title}</h3> : null}
          {extra ? (
            <div className="shrink-0">{extra}</div>
          ) : null}
        </div>
      ) : null}
      <Card.Content className={hasHeader ? 'px-3 pb-3 2xl:px-4 2xl:pb-4' : 'p-3 2xl:p-4'}>{children}</Card.Content>
    </Card>
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
    <Card className="ag-dashboard-metric min-h-[72px] 2xl:min-h-[78px]">
      <Card.Content className="ag-dashboard-metric-content p-3 2xl:p-3.5">
        <div className="ag-dashboard-metric-copy">
          <div className="truncate text-sm font-semibold tracking-normal text-text-tertiary">{title}</div>
          <div className="mt-1 flex min-w-0 items-baseline gap-2">
            <div className="flex min-w-0 items-baseline font-mono text-xl font-semibold leading-none text-text 2xl:text-2xl">
              {value}
              {valueSuffix ? <span className="ml-1.5 text-xs font-medium text-text-tertiary 2xl:text-sm">{valueSuffix}</span> : null}
            </div>
            <div className={`min-w-0 truncate text-xs font-semibold ${META_TONE_CLASSES[metaTone]}`}>{meta}</div>
          </div>
        </div>
        <span
          className={`hidden h-11 w-11 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1 shadow-sm 2xl:flex ${METRIC_TONE_CLASSES[tone]}`}
          style={METRIC_TONE_STYLES[tone]}
        >
          {icon}
        </span>
      </Card.Content>
    </Card>
  );
}

function StatsSkeleton() {
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4 2xl:gap-4">
      {Array.from({ length: 8 }).map((_, index) => (
        <Card className="ag-dashboard-metric min-h-[72px] 2xl:min-h-[78px]" key={index}>
          <Card.Content className="ag-dashboard-metric-content p-3 2xl:p-3.5">
            <div className="ag-dashboard-metric-copy space-y-2">
              <Skeleton className="h-3 w-24" />
              <div className="flex items-baseline gap-2">
                <Skeleton className="h-6 w-24" />
                <Skeleton className="h-3 w-32" />
              </div>
            </div>
            <Skeleton className="hidden h-11 w-11 shrink-0 rounded-[var(--field-radius)] 2xl:block" />
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
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-4 2xl:gap-4">
      <MetricCard
        icon={<KeyRound className="h-5 w-5" />}
        tone="gray"
        title={t('dashboard.api_keys')}
        value={stats.total_api_keys}
        meta={t('dashboard.api_keys_enabled', { count: stats.enabled_api_keys })}
      />
      <MetricCard
        icon={<ToggleRight className="h-5 w-5" />}
        tone="emerald"
        title={t('dashboard.accounts')}
        value={stats.total_accounts}
        meta={t('dashboard.accounts_status', { enabled: stats.enabled_accounts, errors: stats.error_accounts })}
      />
      <MetricCard
        icon={<Activity className="h-5 w-5" />}
        tone="violet"
        title={t('dashboard.today_requests')}
        value={`${fmtNum(todayTextRequests)}/${fmtNum(todayImageRequests)}`}
        valueSuffix={t('dashboard.image_suffix')}
        meta={t('dashboard.alltime_requests', { count: fmtNum(stats.alltime_requests) } as Record<string, string>)}
      />
      <MetricCard
        icon={<Users className="h-5 w-5" />}
        tone="teal"
        title={t('dashboard.users')}
        value={t('dashboard.new_users', { count: stats.new_users_today })}
        meta={`${t('dashboard.total_count', { count: stats.total_users })}  ${t('dashboard.active_users_label', { count: stats.active_users })}`}
      />
      <MetricCard
        icon={<Astroid className="h-5 w-5" />}
        tone="amber"
        title={t('dashboard.today_tokens')}
        value={fmtNum(stats.today_tokens)}
        meta={<CostPair actual={stats.today_cost} standard={stats.today_standard_cost} />}
      />
      <MetricCard
        icon={<Sigma className="h-5 w-5" />}
        tone="stream"
        title={t('dashboard.total_tokens')}
        value={fmtNum(stats.alltime_tokens)}
        meta={<CostPair actual={stats.alltime_cost} standard={stats.alltime_standard_cost} />}
      />
      <MetricCard
        icon={<Zap className="h-5 w-5" />}
        tone="amber"
        metaTone="accent"
        title={t('dashboard.performance')}
        value={Math.round(stats.rpm ?? 0)}
        valueSuffix={t('dashboard.rpm')}
        meta={`${fmtNum(stats.tpm ?? 0)} ${t('dashboard.tpm')}`}
      />
      <MetricCard
        icon={<Clock className="h-5 w-5" />}
        tone="rose"
        title={t('dashboard.avg_response')}
        value={`${fmtDurationMs(stats.avg_first_token_ms)}/${fmtDurationMs(stats.avg_duration_ms)}`}
        meta={
          (stats.avg_image_duration_ms ?? 0) > 0
            ? t('dashboard.image_response_time', { time: fmtDurationMs(stats.avg_image_duration_ms) })
            : undefined
        }
      />
    </div>
  );
}

function ChartTooltip({
  active,
  label,
  payload,
}: {
  active?: boolean;
  label?: string;
  payload?: Array<{ color?: string; dataKey?: string; name?: string; payload?: Record<string, unknown>; value?: number }>;
}) {
  if (!active || !payload?.length) return null;
  const title = dashboardTooltipLabel(label, payload);
  return (
    <div className="rounded-[var(--radius)] border border-border bg-surface px-3 py-2 text-xs text-text shadow-lg">
      <div className="mb-1 font-medium">{title}</div>
      <div className="space-y-1">
        {payload.map((item) => (
          <div key={`${item.dataKey}-${item.name}`} className="flex items-center gap-2">
            <span className="h-2 w-2 rounded-full" style={{ background: item.color }} />
            <span className="text-text">{item.name ?? item.dataKey}</span>
            <span className="font-mono">{fmtNum(Number(item.value ?? 0))}</span>
          </div>
        ))}
      </div>
    </div>
  );
}

function TokenTrendTooltip({
  active,
  label,
  payload,
}: {
  active?: boolean;
  label?: string;
  payload?: Array<{ color?: string; dataKey?: string; payload?: { actualCost?: number; standardCost?: number; timeLabel?: DashboardTimeLabel }; value?: number }>;
}) {
  const { t } = useTranslation();
  if (!active || !payload?.length) return null;
  const datum = payload[0]?.payload;
  const title = dashboardTooltipLabel(label, payload);
  const labels: Record<string, string> = {
    input: t('dashboard.input'),
    output: t('dashboard.output'),
    cacheCreation: t('dashboard.cache_creation'),
    cacheRead: t('dashboard.cache_read'),
    cacheRatio: t('dashboard.cache_ratio'),
    cacheCumulativeRatio: t('dashboard.cache_cumulative_ratio'),
  };
  const orderedPayload = [...payload].sort((a, b) => {
    const aIndex = TOKEN_TREND_LINE_ORDER.indexOf(String(a.dataKey) as keyof typeof USAGE_TOKEN_COLORS);
    const bIndex = TOKEN_TREND_LINE_ORDER.indexOf(String(b.dataKey) as keyof typeof USAGE_TOKEN_COLORS);
    return (aIndex < 0 ? TOKEN_TREND_LINE_ORDER.length : aIndex) - (bIndex < 0 ? TOKEN_TREND_LINE_ORDER.length : bIndex);
  });

  return (
    <div className="rounded-[var(--radius)] border border-border bg-surface px-3 py-2 text-xs text-text shadow-lg">
      <div className="mb-1 font-medium">{title}</div>
      <div className="space-y-1">
        {orderedPayload.map((item) => (
          <div key={item.dataKey} className="flex items-center gap-2">
            <span className="h-2 w-2 rounded-full" style={{ background: item.color }} />
            <span className="text-text">{labels[item.dataKey ?? ''] ?? item.dataKey}</span>
            <span className="font-mono">
              {TOKEN_TREND_RATIO_KEYS.has(item.dataKey as keyof typeof USAGE_TOKEN_COLORS) ? `${Number(item.value ?? 0).toFixed(1)}%` : fmtNum(Number(item.value ?? 0))}
            </span>
          </div>
        ))}
      </div>
      <div className="mt-2 border-t border-border pt-2 text-text">
        {t('dashboard.actual')}: <CostValue className="font-mono" value={datum?.actualCost} tone="actual" />
        {' / '}
        {t('dashboard.standard')}: <CostValue className="font-mono" value={datum?.standardCost} tone="standard" />
      </div>
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
    <DashboardCard title={activeTitle} extra={distributionTabs}>
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
    </DashboardCard>
  );
}

function TokenTrendCard({ trend }: { trend: DashboardTrendResp }) {
  const { t } = useTranslation();
  const lineLabels: Record<string, string> = {
    input: t('dashboard.input'),
    output: t('dashboard.output'),
    cacheCreation: t('dashboard.cache_creation'),
    cacheRead: t('dashboard.cache_read'),
    cacheRatio: t('dashboard.cache_ratio'),
    cacheCumulativeRatio: t('dashboard.cache_cumulative_ratio'),
  };
  const chartData = useMemo(() => {
    let cumulativeCache = 0;
    let cumulativeTotal = 0;

    return (trend.token_trend ?? []).map((item) => {
      const cacheRead = item.cache_read ?? item.cached_input ?? 0;
      const cacheCreation = item.cache_creation ?? 0;
      const cacheTokens = cacheRead + cacheCreation;
      const totalTokens = item.input_tokens + item.output_tokens + cacheTokens;
      cumulativeCache += cacheTokens;
      cumulativeTotal += totalTokens;
      const cacheRatio = totalTokens > 0
        ? Math.min(100, Math.max(0, (cacheTokens / totalTokens) * 100))
        : 0;
      const cacheCumulativeRatio = cumulativeTotal > 0
        ? Math.min(100, Math.max(0, (cumulativeCache / cumulativeTotal) * 100))
        : 0;

      return {
        actualCost: item.actual_cost,
        cacheCreation,
        cacheCumulativeRatio,
        cacheRatio,
        cacheRead,
        input: item.input_tokens,
        output: item.output_tokens,
        standardCost: item.standard_cost,
        time: item.time,
        timeLabel: formatDashboardTime(item.time),
      };
    });
  }, [trend]);

  return (
    <DashboardCard title={t('dashboard.token_trend')}>
      {chartData.length > 0 ? (
        <div className="flex h-[248px] w-full min-w-0 flex-col 2xl:h-[288px]">
          <div className="min-h-0 flex-1">
            <ResponsiveContainer width="100%" height="100%" debounce={80} initialDimension={DASHBOARD_TOKEN_TREND_INITIAL_DIMENSION}>
              <LineChart data={chartData} margin={{ bottom: 8, left: 0, right: 4, top: 4 }}>
                <CartesianGrid stroke="var(--ag-border-subtle)" vertical={false} />
                <XAxis
                  axisLine={false}
                  dataKey="time"
                  height={DASHBOARD_TIME_AXIS_HEIGHT}
                  minTickGap={18}
                  tick={<DashboardTimeAxisTick />}
                  tickLine={false}
                />
                <YAxis
                  yAxisId="tokens"
                  allowDecimals={false}
                  axisLine={false}
                  domain={[0, 'dataMax']}
                  tick={{ fill: 'var(--ag-text)', fontSize: 11 }}
                  tickFormatter={fmtNum}
                  tickLine={false}
                  width={DASHBOARD_TOKEN_Y_AXIS_WIDTH}
                />
                <YAxis
                  yAxisId="ratio"
                  axisLine={false}
                  domain={[0, 100]}
                  orientation="right"
                  tick={{ fill: 'var(--ag-text)', fontSize: 11 }}
                  tickFormatter={(value: number) => `${Math.round(value)}%`}
                  tickLine={false}
                  width={DASHBOARD_RATIO_Y_AXIS_WIDTH}
                />
                <RechartsTooltip content={<TokenTrendTooltip />} />
                <Line yAxisId="tokens" dataKey="input" dot={false} isAnimationActive={false} name={lineLabels.input} stroke={USAGE_TOKEN_COLORS.input} strokeWidth={2.5} type="monotone" />
                <Line yAxisId="tokens" dataKey="output" dot={false} isAnimationActive={false} name={lineLabels.output} stroke={USAGE_TOKEN_COLORS.output} strokeWidth={2.5} type="monotone" />
                <Line yAxisId="tokens" dataKey="cacheCreation" dot={false} isAnimationActive={false} name={lineLabels.cacheCreation} stroke={USAGE_TOKEN_COLORS.cacheCreation} strokeWidth={2.5} type="monotone" />
                <Line yAxisId="tokens" dataKey="cacheRead" dot={false} isAnimationActive={false} name={lineLabels.cacheRead} stroke={USAGE_TOKEN_COLORS.cacheRead} strokeWidth={2.5} type="monotone" />
                <Line yAxisId="ratio" dataKey="cacheRatio" dot={false} isAnimationActive={false} name={lineLabels.cacheRatio} stroke={USAGE_TOKEN_COLORS.cacheRatio} strokeDasharray="5 5" strokeWidth={2} type="monotone" />
                <Line yAxisId="ratio" dataKey="cacheCumulativeRatio" dot={false} isAnimationActive={false} name={lineLabels.cacheCumulativeRatio} stroke={USAGE_TOKEN_COLORS.cacheCumulativeRatio} strokeDasharray="5 5" strokeWidth={2} type="monotone" />
              </LineChart>
            </ResponsiveContainer>
          </div>
          <TokenTrendLegend lineLabels={lineLabels} />
        </div>
      ) : (
        <div className="flex h-[248px] items-center justify-center text-sm text-text 2xl:h-[288px]">{t('common.no_data')}</div>
      )}
    </DashboardCard>
  );
}

function TokenTrendLegend({ lineLabels }: { lineLabels: Record<string, string> }) {
  return (
    <div className="flex shrink-0 flex-wrap items-center justify-center gap-x-4 gap-y-1 pt-1 text-[11px] text-text">
      {TOKEN_TREND_LINE_ORDER.map((key) => (
        <span key={key} className="inline-flex items-center gap-1.5">
          {TOKEN_TREND_RATIO_KEYS.has(key) ? (
            <span className="h-0 w-4 border-t-2 border-dashed" style={{ borderColor: USAGE_TOKEN_COLORS[key] }} />
          ) : (
            <span className="h-2 w-2 rounded-full" style={{ background: USAGE_TOKEN_COLORS[key] }} />
          )}
          <span>{lineLabels[key]}</span>
        </span>
      ))}
    </div>
  );
}

function topUserSeriesKey(userId: number, index: number) {
  return userId > 0 ? `user_${userId}` : `user_index_${index}`;
}

function TopUsersCard({ trend }: { trend: DashboardTrendResp }) {
  const { t } = useTranslation();
  const topUsers = trend.top_users ?? [];
  const userSeries = useMemo(
    () => topUsers.map((user, index) => ({
      color: getUserTrendColor(index),
      id: user.user_id,
      key: topUserSeriesKey(user.user_id, index),
      label: user.email,
    })),
    [topUsers],
  );
  const chartData = useMemo(() => {
    if (topUsers.length === 0) return [];
    const timeSet = new Set<string>();
    topUsers.forEach((user) => user.trend.forEach((point) => timeSet.add(point.time)));
    const trendByUser = topUsers.map((user) => new Map(user.trend.map((point) => [point.time, point.tokens])));
    return Array.from(timeSet).sort().map((time) => {
      const row: Record<string, DashboardTimeLabel | number | string> = {
        time,
        timeLabel: formatDashboardTime(time),
      };
      userSeries.forEach((series, index) => {
        row[series.key] = trendByUser[index]?.get(time) ?? 0;
      });
      return row;
    });
  }, [topUsers, userSeries]);

  return (
    <DashboardCard title={t('dashboard.top_users')}>
      {topUsers.length > 0 ? (
        <div className="flex h-[268px] w-full min-w-0 flex-col 2xl:h-[320px]">
          <div className="min-h-0 flex-1">
            <ResponsiveContainer width="100%" height="100%" debounce={80} initialDimension={DASHBOARD_TOP_USERS_INITIAL_DIMENSION}>
              <LineChart data={chartData} margin={{ bottom: 8, left: 0, right: 8, top: 4 }}>
                <CartesianGrid stroke="var(--ag-border-subtle)" vertical={false} />
                <XAxis
                  axisLine={false}
                  dataKey="time"
                  height={DASHBOARD_TIME_AXIS_HEIGHT}
                  minTickGap={18}
                  tick={<DashboardTimeAxisTick />}
                  tickLine={false}
                />
                <YAxis
                  allowDecimals={false}
                  axisLine={false}
                  domain={[0, 'dataMax']}
                  tick={{ fill: 'var(--ag-text)', fontSize: 11 }}
                  tickFormatter={fmtNum}
                  tickLine={false}
                  width={DASHBOARD_TOKEN_Y_AXIS_WIDTH}
                />
                <RechartsTooltip content={<ChartTooltip />} />
                {userSeries.map((user) => (
                  <Line key={user.key} dataKey={user.key} dot={false} isAnimationActive={false} name={user.label} stroke={user.color} strokeWidth={2.5} type="monotone" />
                ))}
              </LineChart>
            </ResponsiveContainer>
          </div>
          <TopUsersLegend users={userSeries} />
        </div>
      ) : (
        <div className="flex h-[268px] items-center justify-center text-sm text-text 2xl:h-[320px]">{t('common.no_data')}</div>
      )}
    </DashboardCard>
  );
}

function TopUsersLegend({ users }: { users: Array<{ color: string; id: number; label: string }> }) {
  return (
    <div className="flex shrink-0 flex-wrap items-center justify-center gap-x-4 gap-y-1 pt-1 text-[11px] text-text">
      {users.map((user) => (
        <span key={user.id} className="inline-flex min-w-0 items-center gap-1.5">
          <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: user.color }} />
          <span className="max-w-40 truncate">{user.label}</span>
        </span>
      ))}
    </div>
  );
}

function TrendCharts({ trend }: { trend: DashboardTrendResp }) {
  return (
    <div className="ag-dashboard-trends space-y-4 2xl:space-y-5">
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
        <ModelDistributionCard trend={trend} />
        <TokenTrendCard trend={trend} />
      </div>
      <TopUsersCard trend={trend} />
    </div>
  );
}

export default function DashboardPage() {
  const { t } = useTranslation();
  const [range, setRange] = useState<RangePreset>('today');
  const [granularity, setGranularity] = useState<Granularity>('day');
  const [selectedUserId, setSelectedUserId] = useState<number | undefined>();
  const [selectedUserLabel, setSelectedUserLabel] = useState('');
  const granularityOptions = [
    { id: 'day', label: t('dashboard.granularity_day') },
    { id: 'hour', label: t('dashboard.granularity_hour') },
  ];
  const selectedGranularity = range === 'today' ? 'hour' : granularity;
  const selectedGranularityLabel = granularityOptions.find((item) => item.id === selectedGranularity)?.label ?? '';
  const userFilter = selectedUserId ? { user_id: selectedUserId } : undefined;

  const statsQuery = useQuery({
    queryKey: queryKeys.dashboard(selectedUserId),
    queryFn: () => dashboardApi.stats(userFilter),
  });

  const trendParams = useMemo(() => ({
    range,
    granularity: range === 'today' ? 'hour' as const : granularity,
    ...(selectedUserId ? { user_id: selectedUserId } : {}),
  }), [range, granularity, selectedUserId]);

  const trendQuery = useQuery({
    queryKey: queryKeys.dashboardTrend(trendParams),
    queryFn: () => dashboardApi.trend(trendParams),
    placeholderData: keepPreviousData,
  });

  const refresh = () => {
    statsQuery.refetch();
    trendQuery.refetch();
  };

  return (
    <div className="space-y-5 2xl:space-y-6">
      {statsQuery.error ? (
        <Alert status="danger">
          {t('dashboard.load_failed', { error: statsQuery.error instanceof Error ? statsQuery.error.message : '' })}
        </Alert>
      ) : null}

      {statsQuery.isLoading ? <StatsSkeleton /> : statsQuery.data ? <StatsCards stats={statsQuery.data} /> : null}

      <div className="ag-dashboard-toolbar flex flex-col gap-3 p-4 2xl:p-5 xl:flex-row xl:items-center xl:justify-between">
        <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
          <span className="shrink-0 text-sm font-semibold text-text">{t('dashboard.time_range')}</span>
          <Tabs className="ag-segmented-tabs ag-segmented-tabs-compact" selectedKey={range} onSelectionChange={(key) => setRange(key as RangePreset)}>
            <Tabs.List>
              {RANGE_PRESETS.map((item, index) => (
                <Tabs.Tab id={item} key={item}>
                  {index > 0 ? <Tabs.Separator /> : null}
                  <Tabs.Indicator />
                  <span>{t(`dashboard.range_${item}`)}</span>
                </Tabs.Tab>
              ))}
            </Tabs.List>
          </Tabs>
          <Button isIconOnly aria-label={t('common.refresh', 'Refresh')} size="sm" variant="ghost" onPress={refresh}>
            <RefreshCw className={`h-4 w-4 ${statsQuery.isFetching || trendQuery.isFetching ? 'animate-spin' : ''}`} />
          </Button>
        </div>

        <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-end">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <span className="shrink-0 text-sm font-semibold text-text">{t('dashboard.filter_user')}</span>
            <div className="w-full sm:w-48">
              <UserSearchFilterComboBox
                ariaLabel={t('dashboard.filter_user')}
                emptyPrompt={t('dashboard.filter_user')}
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
          </div>

          <div className="flex flex-col gap-2 sm:flex-row sm:items-center">
            <span className="shrink-0 text-sm font-semibold text-text">{t('dashboard.granularity')}</span>
            <div className="w-full sm:w-48">
              <SimpleSelect
                ariaLabel={t('dashboard.granularity')}
                fullWidth
                isDisabled={range === 'today'}
                items={granularityOptions.map((item) => ({ key: item.id, label: item.label }))}
                selectedKey={selectedGranularity}
                selectedLabel={(
                  <span className="inline-flex min-w-0 items-center gap-2">
                    <CalendarDays className="h-4 w-4 shrink-0 text-text" />
                    <span className="min-w-0 truncate">{selectedGranularityLabel}</span>
                  </span>
                )}
                onSelectionChange={(key) => setGranularity(key as Granularity)}
              />
            </div>
          </div>
        </div>
      </div>

      {trendQuery.isLoading && !trendQuery.data ? (
        <div className="ag-dashboard-trends space-y-4 2xl:space-y-5">
          <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
            {Array.from({ length: 2 }).map((_, index) => (
              <Card className="ag-dashboard-panel" key={index}>
                <Card.Content>
                  <Skeleton className="h-[280px] w-full 2xl:h-[320px]" />
                </Card.Content>
              </Card>
            ))}
          </div>
          <Card className="ag-dashboard-panel">
            <Card.Content>
              <Skeleton className="h-[300px] w-full 2xl:h-[360px]" />
            </Card.Content>
          </Card>
        </div>
      ) : trendQuery.data ? (
        <TrendCharts trend={trendQuery.data} />
      ) : null}
    </div>
  );
}
