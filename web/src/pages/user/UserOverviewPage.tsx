import { Panel } from '../../shared/components/Panel';
import { TokenTrendChart } from '../../shared/charts/TokenTrendChart';
import { useState, useMemo, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Card } from '../../shared/components/Card';
import { Tabs } from '@heroui/react';
import {
  Wallet, Zap, Activity, Coins,
} from 'lucide-react';
import { useAuth } from '../../app/providers/AuthProvider';
import { usageApi } from '../../shared/api/usage';
import { queryKeys } from '../../shared/queryKeys';
import { CompactDataTable } from '../../shared/components/CompactDataTable';
import { CostValue } from '../../shared/components/CostValue';
import { DISTRIBUTION_COLORS } from '../../shared/constants';

const DISTRIBUTION_DOT_COLORS = DISTRIBUTION_COLORS;
const TOKEN_TREND_LINE_ORDER = ['input', 'output', 'cacheRead'] as const;

type RangePreset = 'today' | '7d' | '30d' | '90d';
type MetricTone = 'blue' | 'emerald' | 'amber' | 'indigo';

const RANGE_PRESETS = ['today', '7d', '30d', '90d'] as const;
const METRIC_TONE_CLASSES: Record<MetricTone, string> = {
  amber: 'bg-amber-100 text-amber-600 ring-amber-200 dark:bg-amber-400/15 dark:text-amber-300 dark:ring-amber-400/25',
  blue: 'bg-blue-100 text-blue-600 ring-blue-200 dark:bg-blue-400/15 dark:text-blue-300 dark:ring-blue-400/25',
  emerald: 'bg-success-subtle text-success ring-success/25',
  indigo: 'bg-indigo-100 text-indigo-600 ring-indigo-200 dark:bg-indigo-400/15 dark:text-indigo-300 dark:ring-indigo-400/25',
};

function StatCard({
  icon,
  tone,
  title,
  value,
}: {
  icon: ReactNode;
  tone: MetricTone;
  title: string;
  value: ReactNode;
}) {
  return (
    <Card density="compact" className="ag-dashboard-metric ag-overview-metric-card min-h-[72px]">
      <Card.Content className="ag-dashboard-metric-content ag-overview-metric-content p-3">
        <div className="ag-dashboard-metric-copy">
          <div className="truncate text-sm font-semibold tracking-normal text-text-tertiary">{title}</div>
          <div className="mt-1 flex min-w-0 items-baseline gap-2">
            <div className="ag-overview-metric-value flex min-w-0 items-baseline font-mono text-[22px] font-semibold leading-none text-text">
              {value}
            </div>
          </div>
        </div>
        <span className={`ag-overview-metric-badge ag-overview-metric-badge--always h-10 w-10 shrink-0 items-center justify-center rounded-[var(--field-radius)] ring-1 shadow-sm ${METRIC_TONE_CLASSES[tone]}`}>
          {icon}
        </span>
      </Card.Content>
    </Card>
  );
}

function fmtNum(n: number | undefined | null): string {
  if (n == null) return '0';
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(2)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(2)}K`;
  return n.toLocaleString();
}

function rangeToDate(range: RangePreset): { start_date: string; end_date: string } {
  const now = new Date();
  const end = `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}-${String(now.getDate()).padStart(2, '0')}`;
  const d = new Date();
  switch (range) {
    case 'today': break;
    case '7d': d.setDate(d.getDate() - 6); break;
    case '30d': d.setDate(d.getDate() - 29); break;
    case '90d': d.setDate(d.getDate() - 89); break;
  }
  const start = `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
  return { start_date: start, end_date: end };
}

export default function UserOverviewPage() {
  const { t } = useTranslation();
  const { user } = useAuth();
  const [range, setRange] = useState<RangePreset>('today');

  const dateRange = useMemo(() => rangeToDate(range), [range]);
  const granularity = range === 'today' ? 'hour' : 'day';

  // 统计数据（按时间范围）
  const { data: stats } = useQuery({
    queryKey: queryKeys.userUsageStats(dateRange),
    queryFn: () => usageApi.userStats(dateRange),
  });

  // 趋势数据
  const { data: trend } = useQuery({
    queryKey: ['user-trend', dateRange, granularity],
    queryFn: () => usageApi.userTrend({ granularity, ...dateRange }),
  });

  const models = stats?.by_model ?? [];

  const trendData = useMemo(
    () => (trend ?? []).map((b) => ({
      time: b.time,
      input: b.input_tokens,
      output: b.output_tokens,
      cacheRead: b.cache_read,
    })),
    [trend],
  );

  return (
    <div className="ag-overview-page">
      {/* 账户信息 */}
      <div className="ag-overview-metrics-grid grid gap-3">
        <StatCard
          title={t('user_overview.balance')}
          value={`$${(user?.balance ?? 0).toFixed(2)}`}
          icon={<Wallet className="w-5 h-5" />}
          tone="blue"
        />
        <StatCard
          title={t('user_overview.max_concurrency')}
          value={String(user?.max_concurrency ?? 0)}
          icon={<Zap className="w-5 h-5" />}
          tone="indigo"
        />
        <StatCard
          title={t('usage.total_requests')}
          value={(stats?.total_requests ?? 0).toLocaleString()}
          icon={<Activity className="w-5 h-5" />}
          tone="emerald"
        />
        <StatCard
          title={t('usage.actual_cost')}
          value={<CostValue value={stats?.total_actual_cost ?? 0} decimals={4} tone="actual" />}
          icon={<Coins className="w-5 h-5" />}
          tone="amber"
        />
      </div>

      {/* 时间范围选择 */}
      <div className="ag-dashboard-toolbar flex flex-col gap-3 p-4 sm:flex-row sm:items-center">
        <span className="shrink-0 text-sm font-semibold text-text">{t('dashboard.time_range')}</span>
        <Tabs
          className="ag-segmented-tabs ag-segmented-tabs-compact"
          selectedKey={range}
          onSelectionChange={(key) => setRange(key as RangePreset)}
        >
          <Tabs.List>
            {RANGE_PRESETS.map((r, index) => (
              <Tabs.Tab key={r} id={r}>
                {index > 0 ? <Tabs.Separator /> : null}
                <Tabs.Indicator />
                <span>{t(`dashboard.range_${r}`)}</span>
              </Tabs.Tab>
            ))}
          </Tabs.List>
        </Tabs>
      </div>

      {/* 模型分布 + Token 趋势 */}
      <div className="ag-overview-detail-grid grid grid-cols-1 gap-4">
        {/* 模型分布 */}
        <Panel title={t('dashboard.model_distribution')}>
          <div className="ag-distribution-table-scroll">
            <CompactDataTable
              ariaLabel={t('dashboard.model_distribution')}
              className="ag-compact-data-table--dense"
              emptyText={t('common.no_data')}
              minWidth={480}
              rowKey={(row) => row.model}
              rows={models}
              columns={[
                {
                  key: 'model',
                  title: t('usage.model'),
                  width: '32%',
                  render: (row, index) => (
                    <>
                      <span className="shrink-0 font-mono text-[11px] font-semibold text-text">#{index + 1}</span>
                      <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: DISTRIBUTION_DOT_COLORS[index % DISTRIBUTION_DOT_COLORS.length] }} />
                      <span className="min-w-0 truncate font-medium text-text" title={row.model}>{row.model}</span>
                    </>
                  ),
                },
                {
                  align: 'end',
                  key: 'requests',
                  title: t('dashboard.requests'),
                  width: '20%',
                  render: (row) => <span className="truncate font-mono text-text">{row.requests.toLocaleString()}</span>,
                },
                {
                  align: 'end',
                  key: 'tokens',
                  title: t('dashboard.tokens'),
                  width: '24%',
                  render: (row) => <span className="truncate font-mono text-text">{fmtNum(row.tokens)}</span>,
                },
                {
                  align: 'end',
                  key: 'cost',
                  title: t('usage.cost'),
                  width: '24%',
                  render: (row) => <CostValue className="truncate font-mono" value={row.actual_cost} decimals={4} tone="actual" />,
                },
              ]}
            />
          </div>
        </Panel>

        {/* Token 趋势 */}
        <Panel title={t('dashboard.token_trend')}>
          {trendData.length > 0 ? (
            <div className="ag-overview-chart flex h-[248px] w-full min-w-0 flex-col">
              <TokenTrendChart data={trendData} metrics={TOKEN_TREND_LINE_ORDER} />
            </div>
          ) : (
            <div className="ag-overview-chart flex h-[248px] items-center justify-center text-sm text-text">{t('common.no_data')}</div>
          )}
        </Panel>
      </div>
    </div>
  );
}
