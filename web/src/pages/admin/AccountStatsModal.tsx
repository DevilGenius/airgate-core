import { useState, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import { Chip, Tabs, useOverlayState } from '@heroui/react';
import {
  DollarSign, Activity, TrendingUp, Clock, Calendar,
  Cpu, Zap,
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
import { PlatformIcon } from '../../shared/ui';
import {
  accountsApi,
  type AccountModelSuccessRate,
  type AccountModelSuccessRateBucket,
  type AccountModelSuccessRateWindow,
  type AccountStatsResp,
} from '../../shared/api/accounts';
import { CommonDatePicker } from '../../shared/components/CommonDatePicker';
import { CompactDataTable } from '../../shared/components/CompactDataTable';
import { CommonModal } from '../../shared/components/CommonModal';
import { DISTRIBUTION_COLORS } from '../../shared/constants';
import { useMediaQuery } from '../../shared/hooks/useMediaQuery';

const DISTRIBUTION_DOT_COLORS = DISTRIBUTION_COLORS;
const MODEL_RATE_REFRESH_INTERVAL_MS = 30_000;

// 预设时间范围；rate 是近 24 小时模型成功率视图，排在最前且默认选中
type RangePreset = 'rate' | '7d' | '30d' | '90d' | 'custom';
const RANGE_PRESETS = ['rate', '7d', '30d', '90d', 'custom'] as const;

// 按浏览器本地时区拼出 YYYY-MM-DD（不要用 toISOString，那是 UTC，会跨日）。
function localDateStr(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

function getPresetDates(preset: RangePreset): { start_date?: string; end_date?: string } {
  if (preset === 'custom') return {};
  const now = new Date();
  const end = localDateStr(now);
  // rate 视图只展示调度器统计，账号头部不需要历史费用数据，按当天查询即可降低刷新成本。
  const days = preset === 'rate' ? 1 : preset === '7d' ? 7 : preset === '90d' ? 90 : 30;
  const start = new Date(now);
  start.setDate(start.getDate() - (days - 1));
  return { start_date: localDateStr(start), end_date: end };
}

// 格式化数字
function fmtNum(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(2)}K`;
  return n.toLocaleString();
}

// 格式化费用
function fmtCost(n: number, decimals = 4): string {
  return `$${n.toFixed(decimals)}`;
}

// 格式化日期为 MM/DD
function fmtDate(dateStr: string): string {
  const parts = dateStr.split('-');
  return `${parts[1]}/${parts[2]}`;
}

export function AccountStatsModal({
  accountId,
  lifetimeImageCount,
  onClose,
}: {
  accountId: number;
  /** 累计生图数（全部历史，不受时间范围限制）。由列表页直接透传，避免 stats endpoint 多查一遍。仅 OpenAI 平台账号有值。 */
  lifetimeImageCount?: number;
  onClose: () => void;
}) {
  const { t } = useTranslation();

  // 时间范围状态，默认"模型成功率"
  const [preset, setPreset] = useState<RangePreset>('rate');
  const [customStart, setCustomStart] = useState('');
  const [customEnd, setCustomEnd] = useState('');

  const queryParams = useMemo(() => {
    if (preset === 'custom' && customStart) {
      return { start_date: customStart, end_date: customEnd || undefined };
    }
    return getPresetDates(preset);
  }, [preset, customStart, customEnd]);
  const queryKey = useMemo(
    () => ['account-stats', accountId, preset, queryParams.start_date ?? '', queryParams.end_date ?? ''] as const,
    [accountId, preset, queryParams.end_date, queryParams.start_date],
  );

  const { data, isFetching, isLoading } = useQuery({
    queryKey,
    queryFn: () => accountsApi.stats(accountId, queryParams),
    placeholderData: keepPreviousData,
    refetchInterval: preset === 'rate' ? MODEL_RATE_REFRESH_INTERVAL_MS : false,
    refetchOnWindowFocus: false,
  });
  const initialLoading = isLoading && !data;
  const isRefreshing = isFetching && !initialLoading;
  const modalState = useOverlayState({
    isOpen: true,
    onOpenChange: (open) => {
      if (!open) onClose();
    },
  });

  return (
    <CommonModal
      className="ag-account-page-modal ag-account-stats-modal"
      dialogStyle={{ maxWidth: '880px', width: 'min(100%, calc(100vw - 2rem))' }}
      icon={<Activity className="size-5" />}
      size="lg"
      state={modalState}
      title={t('accounts.view_stats')}
    >
      <div className="space-y-4" aria-busy={isFetching}>
        <AccountStatsRangeControls
          customEnd={customEnd}
          customStart={customStart}
          isRefreshing={isRefreshing}
          preset={preset}
          onCustomEndChange={setCustomEnd}
          onCustomStartChange={setCustomStart}
          onPresetChange={setPreset}
        />

        {/* 固定高度 + 内部滚动，保证切换 tab 时 modal 尺寸不变 */}
        <div className="h-[min(72vh,760px)] overflow-y-auto">
          {data ? (
            <div className="space-y-5">
              {/* 账号卡片始终显示；成功率统计视图的模型请求表紧随其后 */}
              <AccountHeaderCard data={data} rateView={preset === 'rate'} />
              {preset === 'rate' ? (
                <ModelRequestStats
                  rates={data.model_success_rates ?? []}
                  window={data.model_success_rate_window}
                />
              ) : (
                <StatsContent data={data} lifetimeImageCount={lifetimeImageCount} />
              )}
            </div>
          ) : (
            <AccountStatsSkeleton />
          )}
        </div>
      </div>
    </CommonModal>
  );
}

function AccountStatsRangeControls({
  customEnd,
  customStart,
  isRefreshing,
  onCustomEndChange,
  onCustomStartChange,
  onPresetChange,
  preset,
}: {
  customEnd: string;
  customStart: string;
  isRefreshing: boolean;
  onCustomEndChange: (value: string) => void;
  onCustomStartChange: (value: string) => void;
  onPresetChange: (value: RangePreset) => void;
  preset: RangePreset;
}) {
  const { t } = useTranslation();

  return (
    <div className="flex min-h-[2rem] flex-wrap items-center gap-2">
      <Tabs
        className="ag-segmented-tabs ag-segmented-tabs-compact ag-segmented-tabs-auto"
        selectedKey={preset}
        onSelectionChange={(key) => onPresetChange(key as RangePreset)}
      >
        <Tabs.List>
          {RANGE_PRESETS.map((item, index) => (
            <Tabs.Tab key={item} id={item} className="whitespace-nowrap">
              {index > 0 ? <Tabs.Separator /> : null}
              <Tabs.Indicator />
              <span>{t(`accounts.stats_range_${item}`)}</span>
            </Tabs.Tab>
          ))}
        </Tabs.List>
      </Tabs>

      {preset === 'custom' && (
        <div className="ag-account-stats-date-range grid w-full grid-cols-1 gap-2 sm:ml-2 sm:w-auto sm:grid-cols-[minmax(13.5rem,1fr)_auto_minmax(13.5rem,1fr)] sm:items-end">
          <CommonDatePicker
            className="w-full sm:w-56"
            hideLabel
            label={t('accounts.stats_start_date')}
            value={customStart}
            onChange={onCustomStartChange}
          />
          <span className="hidden h-10 items-center text-xs text-text-tertiary sm:inline-flex">—</span>
          <CommonDatePicker
            className="w-full sm:w-56"
            hideLabel
            label={t('accounts.stats_end_date')}
            value={customEnd}
            onChange={onCustomEndChange}
          />
        </div>
      )}

      <span className="sr-only" aria-live="polite">
        {isRefreshing ? t('common.loading') : ''}
      </span>
    </div>
  );
}

function SkeletonBlock({ className }: { className: string }) {
  return <div className={`ag-shimmer ${className}`} />;
}

function AccountStatsSkeleton() {
  return (
    <div className="space-y-5" aria-hidden="true">
      <div className="rounded-lg border border-border-subtle p-4">
        <div className="flex items-center gap-3">
          <SkeletonBlock className="h-10 w-10 rounded-lg" />
          <div className="min-w-0 flex-1 space-y-2">
            <SkeletonBlock className="h-4 w-40 rounded" />
            <SkeletonBlock className="h-3 w-64 max-w-full rounded" />
          </div>
          <SkeletonBlock className="h-6 w-16 rounded-[var(--radius)]" />
        </div>
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {Array.from({ length: 4 }, (_, index) => (
          <div className="rounded-lg border border-border-subtle p-3.5" key={index}>
            <div className="mb-3 flex items-start justify-between">
              <SkeletonBlock className="h-3 w-20 rounded" />
              <SkeletonBlock className="h-7 w-7 rounded-md" />
            </div>
            <SkeletonBlock className="h-6 w-24 rounded" />
            <SkeletonBlock className="mt-2 h-3 w-28 rounded" />
          </div>
        ))}
      </div>

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        {Array.from({ length: 6 }, (_, index) => (
          <div className="space-y-3 rounded-lg border border-border-subtle p-3.5" key={index}>
            <SkeletonBlock className="h-4 w-24 rounded" />
            <SkeletonBlock className="h-3 w-full rounded" />
            <SkeletonBlock className="h-3 w-5/6 rounded" />
            <SkeletonBlock className="h-3 w-2/3 rounded" />
          </div>
        ))}
      </div>

      <div className="rounded-lg border border-border-subtle p-4">
        <SkeletonBlock className="mb-3 h-4 w-32 rounded" />
        <SkeletonBlock className="h-[260px] w-full rounded" />
      </div>
    </div>
  );
}

function AccountHeaderCard({ data, rateView }: { data: AccountStatsResp; rateView: boolean }) {
  const { t } = useTranslation();
  const activeDays = data.active_days || 1;
  const rangeLabel = rateView
    ? t('accounts.stats_rate_range_summary')
    : `${data.start_date} ~ ${data.end_date} · ${t('accounts.stats_range_summary', { days: data.total_days, active: activeDays })}`;

  return (
    <div className="flex flex-wrap items-center gap-3 p-4 rounded-lg bg-gradient-to-r from-primary-subtle/50 to-transparent border border-border-subtle">
      <div className="flex items-center justify-center w-10 h-10 rounded-lg bg-primary-subtle">
        <PlatformIcon platform={data.platform} className="w-5 h-5" />
      </div>
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="font-semibold text-sm text-text truncate">{data.name}</span>
        </div>
        <span className="text-xs text-text-tertiary">
          {rangeLabel}
        </span>
      </div>
      <Chip color={data.state === 'disabled' ? 'default' : 'success'} size="sm" variant="soft">
        {data.state === 'disabled' ? t('status.disabled') : t('status.active')}
      </Chip>
    </div>
  );
}

function StatsContent({ data, lifetimeImageCount }: { data: AccountStatsResp; lifetimeImageCount?: number }) {
  const { t } = useTranslation();
  const range = data.range;

  // 计算活跃天数和日均
  // 注意：所有"上游计费"相关数字都用 account_cost（base × account_rate），
  // 而不是 total_cost（base 原价）。这样 reseller 配置 account_rate 才能真正反映"我用这个上游账号的实际花费"。
  const activeDays = data.active_days || 1;
  const dailyAvgCost = range.account_cost / activeDays;
  const dailyAvgRequests = range.count / activeDays;

  // Token 总量
  const totalTokens = range.input_tokens + range.output_tokens;
  const dailyAvgTokens = totalTokens / activeDays;

  return (
    <div className="space-y-5">
      {/* 顶部 4 个统计卡片 */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
        <MiniStatCard
          label={t('accounts.stats_range_cost')}
          value={fmtCost(range.account_cost, 2)}
          sub={`${t('accounts.stats_actual')}: ${fmtCost(range.actual_cost, 2)}`}
          icon={<DollarSign className="w-4 h-4" />}
          color="var(--ag-warning)"
        />
        <MiniStatCard
          label={t('accounts.stats_range_requests')}
          value={fmtNum(range.count)}
          sub={t('accounts.stats_total_calls')}
          icon={<Activity className="w-4 h-4" />}
          color="var(--ag-info)"
        />
        <MiniStatCard
          label={t('accounts.stats_daily_cost')}
          value={fmtCost(dailyAvgCost, 2)}
          sub={t('accounts.stats_based_on_days', { days: activeDays })}
          icon={<TrendingUp className="w-4 h-4" />}
          color="var(--ag-success)"
        />
        <MiniStatCard
          label={t('accounts.stats_daily_requests')}
          value={fmtNum(Math.round(dailyAvgRequests))}
          sub={t('accounts.stats_avg_daily')}
          icon={<Zap className="w-4 h-4" />}
          color="var(--ag-danger)"
        />
      </div>

      {/* 中间 3 个信息卡片 */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        {/* 今日概览 */}
        <InfoCard title={t('accounts.stats_today')} icon={<Clock className="w-4 h-4" />} color="var(--ag-info)">
          <InfoRow label={t('accounts.stats_cost')} value={fmtCost(data.today.account_cost)} />
          <InfoRow label={t('accounts.stats_actual_cost')} value={fmtCost(data.today.actual_cost)} />
          <InfoRow label={t('accounts.stats_requests')} value={data.today.count.toLocaleString()} />
          <InfoRow label="Token" value={fmtNum(data.today.input_tokens + data.today.output_tokens)} />
          {data.today.image_count > 0 && (
            <InfoRow
              label={t('accounts.stats_today_images', '今日生图')}
              value={`${fmtNum(data.today.image_count)} · ${fmtCost(data.today.image_cost)}`}
            />
          )}
        </InfoCard>

        {/* 最高费用日 */}
        <InfoCard title={t('accounts.stats_peak_cost_day')} icon={<DollarSign className="w-4 h-4" />} color="var(--ag-warning)">
          <InfoRow label={t('accounts.stats_date')} value={data.peak_cost_day.date ? fmtDate(data.peak_cost_day.date) : '-'} />
          <InfoRow label={t('accounts.stats_cost')} value={fmtCost(data.peak_cost_day.account_cost)} highlight />
          <InfoRow label={t('accounts.stats_actual_cost')} value={fmtCost(data.peak_cost_day.actual_cost)} />
          <InfoRow label={t('accounts.stats_requests')} value={fmtNum(data.peak_cost_day.count)} />
        </InfoCard>

        {/* 最高请求日 */}
        <InfoCard title={t('accounts.stats_peak_request_day')} icon={<Activity className="w-4 h-4" />} color="var(--ag-success)">
          <InfoRow label={t('accounts.stats_date')} value={data.peak_request_day.date ? fmtDate(data.peak_request_day.date) : '-'} />
          <InfoRow label={t('accounts.stats_requests')} value={fmtNum(data.peak_request_day.count)} highlight />
          <InfoRow label={t('accounts.stats_cost')} value={fmtCost(data.peak_request_day.account_cost)} />
          <InfoRow label={t('accounts.stats_actual_cost')} value={fmtCost(data.peak_request_day.actual_cost)} />
        </InfoCard>
      </div>

      {/* 下方 3 个信息卡片 */}
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        {/* 累计 Token */}
        <InfoCard title={t('accounts.stats_total_tokens')} icon={<Cpu className="w-4 h-4" />} color="var(--ag-primary)">
          <InfoRow label={t('accounts.stats_range_total')} value={fmtNum(totalTokens)} />
          <InfoRow label={t('accounts.stats_daily_avg_token')} value={fmtNum(Math.round(dailyAvgTokens))} />
        </InfoCard>

        {/* 性能 */}
        <InfoCard title={t('accounts.stats_performance')} icon={<Zap className="w-4 h-4" />} color="var(--ag-warning)">
          <InfoRow label={t('accounts.stats_avg_response')} value={`${(data.avg_duration_ms / 1000).toFixed(2)}s`} />
          <InfoRow label={t('accounts.stats_active_days')} value={`${data.active_days} / ${data.total_days}`} />
        </InfoCard>

        {/* 最近统计 */}
        <InfoCard title={t('accounts.stats_recent')} icon={<Calendar className="w-4 h-4" />} color="var(--ag-info)">
          <InfoRow label={t('accounts.stats_today_requests')} value={data.today.count.toLocaleString()} />
          <InfoRow label={t('accounts.stats_today_tokens')} value={fmtNum(data.today.input_tokens + data.today.output_tokens)} />
          <InfoRow label={t('accounts.stats_today_cost')} value={fmtCost(data.today.account_cost)} />
          {range.image_count > 0 && (
            <InfoRow
              label={t('accounts.stats_range_images', '区间生图')}
              value={`${fmtNum(range.image_count)} · ${fmtCost(range.image_cost)}`}
            />
          )}
          {/* 累计生图来自列表页透传，跨整段历史；仅 OpenAI 平台 + lifetime 列表查询有值。 */}
          {lifetimeImageCount !== undefined && lifetimeImageCount > 0 && (
            <InfoRow
              label={t('accounts.stats_lifetime_images', '累计生图')}
              value={fmtNum(lifetimeImageCount)}
            />
          )}
        </InfoCard>
      </div>

      {/* 费用与请求趋势 */}
      <TrendChart data={data} />

      {/* 模型分布 */}
      {data.models && data.models.length > 0 && <ModelDistribution data={data} />}
    </div>
  );
}

// ==================== 迷你统计卡片 ====================

function MiniStatCard({
  label, value, sub, icon, color,
}: {
  label: string; value: string; sub: string; icon: React.ReactNode; color: string;
}) {
  return (
    <div className="relative overflow-hidden rounded-lg border border-border-subtle p-3.5 transition-colors hover:border-border">
      <div className="absolute top-0 left-0 right-0 h-px opacity-40" style={{ background: `linear-gradient(90deg, transparent, ${color}, transparent)` }} />
      <div className="flex items-start justify-between mb-2">
        <span className="text-[11px] text-text-tertiary font-medium">{label}</span>
        <div className="flex items-center justify-center w-7 h-7 rounded-md" style={{ background: `color-mix(in srgb, ${color} 12%, transparent)`, color }}>
          {icon}
        </div>
      </div>
      <div className="text-xl font-bold text-text font-mono">{value}</div>
      <div className="text-[10px] text-text-tertiary mt-1">{sub}</div>
    </div>
  );
}

// ==================== 信息卡片 ====================

function InfoCard({
  title, icon, color, children,
}: {
  title: string; icon: React.ReactNode; color: string; children: React.ReactNode;
}) {
  return (
    <div className="rounded-lg border border-border-subtle p-3.5 space-y-2">
      <div className="flex items-center gap-1.5">
        <div className="flex items-center justify-center w-5 h-5 rounded" style={{ color }}>{icon}</div>
        <span className="text-xs font-semibold text-text">{title}</span>
      </div>
      <div className="space-y-1.5">{children}</div>
    </div>
  );
}

function InfoRow({ label, value, highlight }: { label: string; value: string; highlight?: boolean }) {
  return (
    <div className="flex items-center justify-between text-xs">
      <span className="text-text-tertiary">{label}</span>
      <span className={`font-mono ${highlight ? 'text-warning font-semibold' : 'text-text-secondary'}`}>{value}</span>
    </div>
  );
}

// ==================== 趋势图 ====================

function TrendChart({ data }: { data: AccountStatsResp }) {
  const { t } = useTranslation();
  const isMobile = useMediaQuery('(max-width: 767px)');

  const chartData = useMemo(() =>
    (data.daily_trend ?? []).map((d) => ({
      date: fmtDate(d.date),
      // 趋势图的"上游计费"线读 account_cost（含 account_rate），匹配卡片数字
      totalCost: Number(d.account_cost.toFixed(4)),
      actualCost: Number(d.actual_cost.toFixed(4)),
      count: d.count,
    })),
    [data.daily_trend],
  );

  if (chartData.length === 0) return null;

  return (
    <div className="rounded-lg border border-border-subtle p-4">
      <h4 className="text-xs font-semibold text-text mb-3">{t('accounts.stats_trend_title')}</h4>
      <ResponsiveContainer width="100%" height={isMobile ? 200 : 260} debounce={80}>
        <LineChart data={chartData} margin={isMobile ? { top: 5, right: 4, left: 0, bottom: 5 } : { top: 5, right: 20, left: 10, bottom: 5 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="var(--ag-border-subtle)" />
          <XAxis
            dataKey="date"
            tick={{ fontSize: 10, fill: 'var(--ag-text-tertiary)' }}
            axisLine={{ stroke: 'var(--ag-border)' }}
            tickLine={false}
          />
          <YAxis
            yAxisId="cost"
            tick={{ fontSize: 10, fill: 'var(--ag-text-tertiary)' }}
            axisLine={false}
            tickLine={false}
            tickFormatter={(v: number) => `$${v}`}
          />
          <YAxis
            yAxisId="count"
            orientation="right"
            tick={{ fontSize: 10, fill: 'var(--ag-text-tertiary)' }}
            axisLine={false}
            tickLine={false}
            tickFormatter={(v: number) => fmtNum(v)}
          />
          <RechartsTooltip
            contentStyle={{
              background: 'var(--ag-bg-elevated)',
              border: '1px solid var(--ag-border)',
              borderRadius: 8,
              fontSize: 12,
              padding: '8px 12px',
            }}
            labelStyle={{ color: 'var(--ag-text)', fontWeight: 600, marginBottom: 4 }}
            itemStyle={{ padding: '2px 0' }}
            formatter={(value, name) => {
              const v = Number(value);
              if (name === 'count') return [fmtNum(v), t('accounts.stats_requests')];
              return [`$${v.toFixed(4)}`, name === 'totalCost' ? t('accounts.stats_total_cost_label') : t('accounts.stats_actual_cost_label')];
            }}
          />
          <Line yAxisId="cost" type="monotone" dataKey="totalCost" stroke="#3b82f6" strokeWidth={2} dot={false} isAnimationActive={false} name="totalCost" />
          <Line yAxisId="cost" type="monotone" dataKey="actualCost" stroke="#10b981" strokeWidth={2} dot={false} isAnimationActive={false} name="actualCost" />
          <Line yAxisId="count" type="monotone" dataKey="count" stroke="#f59e0b" strokeWidth={2} dot={false} isAnimationActive={false} name="count" />
        </LineChart>
      </ResponsiveContainer>
      <div className="flex flex-wrap items-center justify-center gap-x-4 gap-y-1 mt-2">
        <LegendDot color="#3b82f6" label={`${t('accounts.stats_total_cost_label')} (USD)`} />
        <LegendDot color="#10b981" label={`${t('accounts.stats_actual_cost_label')} (USD)`} />
        <LegendDot color="#f59e0b" label={t('accounts.stats_requests')} />
      </div>
    </div>
  );
}

function LegendDot({ color, label }: { color: string; label: string }) {
  return (
    <div className="flex items-center gap-1.5 text-[11px] text-text-tertiary">
      <div className="w-2.5 h-2.5 rounded-full" style={{ background: color }} />
      {label}
    </div>
  );
}

// ==================== 模型请求（24h 固定时间桶） ====================

function fmtPercent(ratio: number): string {
  return `${(ratio * 100).toFixed(1)}%`;
}

function fmtBucketTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '-';
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false });
}

function fmtBucketDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return '-';
  return d.toLocaleDateString(undefined, { month: '2-digit', day: '2-digit' });
}

function fmtBucketRange(startISO: string, endISO: string): string {
  const start = new Date(startISO);
  const end = new Date(endISO);
  if (Number.isNaN(start.getTime()) || Number.isNaN(end.getTime())) return '-';
  const startDate = fmtBucketDate(startISO);
  const endDate = fmtBucketDate(endISO);
  const startTime = fmtBucketTime(startISO);
  const endTime = fmtBucketTime(endISO);
  return startDate === endDate
    ? `${startDate} ${startTime} – ${endTime}`
    : `${startDate} ${startTime} – ${endDate} ${endTime}`;
}

function successRateColor(rate: number): string {
  if (rate >= 0.95) return 'var(--ag-success)';
  if (rate >= 0.8) return 'var(--ag-warning)';
  return 'var(--ag-danger)';
}

type AccountModelSuccessRateBucketView = AccountModelSuccessRateBucket & {
  window_start: string;
  window_end: string;
};

function bucketTone(bucket: AccountModelSuccessRateBucket): 'empty' | 'neutral' | 'good' | 'warning' | 'bad' {
  if (bucket.requests === 0) return 'empty';
  if (bucket.valid_requests === 0) return 'neutral';
  if (bucket.success_rate >= 0.95) return 'good';
  if (bucket.success_rate >= 0.8) return 'warning';
  return 'bad';
}

function aggregateRateBuckets(
  rates: AccountModelSuccessRate[],
  rateWindow?: AccountModelSuccessRateWindow | null,
): AccountModelSuccessRateBucketView[] {
  const fallbackStart = rates[0]?.window_start;
  const fallbackEnd = rates[0]?.window_end;
  const windowStart = rateWindow?.window_start ?? fallbackStart;
  const bucketCount = rateWindow?.bucket_count || 48;
  const bucketSeconds = rateWindow?.bucket_seconds || (
    windowStart && fallbackEnd
      ? Math.max(1, (new Date(fallbackEnd).getTime() - new Date(windowStart).getTime()) / bucketCount / 1000)
      : 1800
  );
  if (!windowStart || Number.isNaN(new Date(windowStart).getTime())) return [];

  const buckets = Array.from({ length: bucketCount }, (_, index) => {
    const start = new Date(new Date(windowStart).getTime() + index * bucketSeconds * 1000);
    const end = new Date(start.getTime() + bucketSeconds * 1000);
    return {
      index,
      window_start: start.toISOString(),
      window_end: end.toISOString(),
      requests: 0,
      valid_requests: 0,
      invalid_requests: 0,
      successes: 0,
      failures: 0,
      success_rate: 0,
    } satisfies AccountModelSuccessRateBucketView;
  });

  rates.forEach((rate) => {
    (rate.buckets ?? []).forEach((bucket) => {
      const current = buckets[bucket.index];
      if (!current) return;
      current.requests += bucket.requests;
      current.valid_requests += bucket.valid_requests;
      current.invalid_requests += bucket.invalid_requests;
      current.successes += bucket.successes;
      current.failures += bucket.failures;
      current.success_rate = current.valid_requests > 0 ? current.successes / current.valid_requests : 0;
    });
  });
  return buckets;
}

function ModelRequestStats({
  rates,
  window,
}: {
  rates: AccountModelSuccessRate[];
  window?: AccountModelSuccessRateWindow | null;
}) {
  const { t } = useTranslation();

  const [selectedBucketIndex, setSelectedBucketIndex] = useState<number | null>(null);
  const buckets = useMemo(() => aggregateRateBuckets(rates, window), [rates, window]);
  const selectedBucket = useMemo(() => {
    const selected = selectedBucketIndex !== null
      ? buckets.find((bucket) => bucket.index === selectedBucketIndex)
      : undefined;
    const latestActive = [...buckets].reverse().find((bucket) => bucket.requests > 0);
    return selected ?? latestActive ?? buckets[buckets.length - 1];
  }, [buckets, selectedBucketIndex]);

  // 只列出所选固定桶内确有请求的模型，按请求量降序。
  const rows = useMemo<AccountModelSuccessRate[]>(
    () => {
      if (!selectedBucket) return [];
      return rates
        .flatMap((rate) => {
          const bucket = (rate.buckets ?? []).find(
            (item) => item.index === selectedBucket.index,
          );
          return bucket && bucket.requests > 0 ? [{ ...rate, ...bucket }] : [];
        })
        .sort((a, b) => b.requests - a.requests);
    },
    [rates, selectedBucket],
  );

  return (
    <>
      <div className="ag-account-rate-panel rounded-lg border border-border-subtle p-4">
        <div className="mb-3 flex flex-wrap items-baseline gap-x-2">
          <h4 className="text-xs font-semibold text-text">{t('accounts.stats_model_requests')}</h4>
          <span className="text-[10px] text-text-tertiary">{t('accounts.stats_model_requests_window')}</span>
        </div>

        {selectedBucket ? (
          <>
            <div className="ag-account-rate-selected-range">
              <div className="ag-account-rate-selected-range__label">
                <Clock className="size-3.5" />
                <span>{t('accounts.stats_selected_bucket')}</span>
              </div>
              <strong>{fmtBucketRange(selectedBucket.window_start, selectedBucket.window_end)}</strong>
              <span className="ag-account-rate-selected-range__summary">
                {t('accounts.stats_bucket_summary', {
                  requests: selectedBucket.requests.toLocaleString(),
                  invalid: selectedBucket.invalid_requests.toLocaleString(),
                  successes: selectedBucket.successes.toLocaleString(),
                  valid: selectedBucket.valid_requests.toLocaleString(),
                  rate: selectedBucket.valid_requests > 0 ? fmtPercent(selectedBucket.success_rate) : '-',
                })}
              </span>
            </div>

            <div className="ag-account-rate-buckets" role="group" aria-label={t('accounts.stats_bucket_selector')}>
              {buckets.map((bucket) => {
                const selected = bucket.index === selectedBucket.index;
                const range = fmtBucketRange(bucket.window_start, bucket.window_end);
                const rate = bucket.valid_requests > 0 ? fmtPercent(bucket.success_rate) : '-';
                return (
                  <button
                    aria-label={t('accounts.stats_bucket_aria', {
                      range,
                      requests: bucket.requests.toLocaleString(),
                      rate,
                    })}
                    aria-pressed={selected}
                    className={`ag-account-rate-bucket ag-account-rate-bucket--${bucketTone(bucket)}`}
                    data-selected={selected}
                    key={bucket.index}
                    title={range}
                    type="button"
                    onClick={() => setSelectedBucketIndex(bucket.index)}
                  >
                    <span className="ag-account-rate-bucket__time">{fmtBucketTime(bucket.window_start)}</span>
                    <span className="ag-account-rate-bucket__count">
                      {bucket.requests > 0 ? bucket.requests.toLocaleString() : '–'}
                    </span>
                  </button>
                );
              })}
            </div>
          </>
        ) : null}
      </div>

      {/* 模型统计表格独立成卡片 */}
      <div className="rounded-lg border border-border-subtle p-4">
        <div className="ag-distribution-table-scroll">
          <CompactDataTable
          ariaLabel={t('accounts.stats_model_requests')}
          className="ag-compact-data-table--dense ag-account-stats-model-table"
          emptyText={t('common.no_data')}
          minWidth={650}
          rowKey={(row) => row.model}
          rows={rows}
          columns={[
            {
              key: 'model',
              title: t('accounts.stats_model'),
              width: '28%',
              render: (row) => (
                <span className="min-w-0 truncate font-medium text-text" title={row.model}>{row.model}</span>
              ),
            },
            {
              align: 'end',
              key: 'requests',
              title: t('accounts.stats_requests'),
              width: '12%',
              render: (row) => <span className="truncate font-mono text-text-secondary">{row.requests.toLocaleString()}</span>,
            },
            {
              align: 'end',
              key: 'valid_requests',
              title: t('accounts.stats_valid_requests'),
              width: '12%',
              render: (row) => <span className="truncate font-mono text-text-secondary">{row.valid_requests.toLocaleString()}</span>,
            },
            {
              align: 'end',
              key: 'successes',
              title: t('accounts.stats_success'),
              width: '12%',
              render: (row) => <span className="truncate font-mono text-success">{row.successes.toLocaleString()}</span>,
            },
            {
              align: 'end',
              key: 'failures',
              title: t('accounts.stats_failure'),
              width: '12%',
              render: (row) => <span className="truncate font-mono text-danger">{row.failures.toLocaleString()}</span>,
            },
            {
              align: 'end',
              key: 'invalid',
              title: t('accounts.stats_invalid_requests'),
              width: '12%',
              render: (row) => <span className="truncate font-mono text-text-tertiary">{row.invalid_requests.toLocaleString()}</span>,
            },
            {
              align: 'end',
              key: 'success_rate',
              title: t('accounts.stats_success_rate'),
              width: '12%',
              render: (row) => (
                <span
                  className="truncate font-mono font-semibold"
                  style={{ color: row.valid_requests > 0 ? successRateColor(row.success_rate) : 'var(--ag-text-tertiary)' }}
                >
                  {row.valid_requests > 0 ? fmtPercent(row.success_rate) : '-'}
                </span>
              ),
            },
          ]}
          />
        </div>
      </div>
    </>
  );
}

// ==================== 模型分布 ====================

function ModelDistribution({ data }: { data: AccountStatsResp }) {
  const { t } = useTranslation();
  const models = data.models ?? [];

  return (
    <div className="rounded-lg border border-border-subtle p-4">
      <h4 className="text-xs font-semibold text-text mb-3">{t('accounts.stats_model_distribution')}</h4>
      <div className="ag-distribution-table-scroll">
        <CompactDataTable
          ariaLabel={t('accounts.stats_model')}
          className="ag-compact-data-table--dense ag-account-stats-model-table"
          emptyText={t('common.no_data')}
          minWidth={440}
          rowKey={(row) => row.model}
          rows={models}
          columns={[
            {
              key: 'model',
              title: t('accounts.stats_model'),
              width: '34%',
              render: (row, index) => (
                <>
                  <span className="shrink-0 font-mono text-[11px] font-semibold text-text-tertiary">#{index + 1}</span>
                  <span className="h-2 w-2 shrink-0 rounded-full" style={{ background: DISTRIBUTION_DOT_COLORS[index % DISTRIBUTION_DOT_COLORS.length] }} />
                  <span className="min-w-0 truncate font-medium text-text" title={row.model}>{row.model}</span>
                </>
              ),
            },
            {
              align: 'end',
              key: 'requests',
              title: t('accounts.stats_requests'),
              width: '15%',
              render: (row) => <span className="truncate font-mono text-text-secondary">{row.count.toLocaleString()}</span>,
            },
            {
              align: 'end',
              key: 'tokens',
              title: 'Token',
              width: '17%',
              render: (row) => <span className="truncate font-mono text-text-secondary">{fmtNum(row.input_tokens + row.output_tokens)}</span>,
            },
            {
              align: 'end',
              key: 'actual',
              title: t('accounts.stats_actual'),
              width: '17%',
              render: (row) => <span className="truncate font-mono text-warning">{fmtCost(row.actual_cost, 2)}</span>,
            },
            {
              align: 'end',
              key: 'standard',
              title: t('accounts.stats_standard'),
              width: '17%',
              render: (row) => <span className="truncate font-mono text-text-secondary">{fmtCost(row.total_cost, 2)}</span>,
            },
          ]}
        />
      </div>
    </div>
  );
}
