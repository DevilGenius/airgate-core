import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import {
  PieChart, Pie, Cell, ResponsiveContainer, Tooltip as RechartsTooltip,
} from 'recharts';
import {
  Wallet, Zap,
  Activity, Coins, Clock, TrendingUp,
} from 'lucide-react';
import { useAuth } from '../../app/providers/AuthProvider';
import { usageApi } from '../../shared/api/usage';
import { queryKeys } from '../../shared/queryKeys';
import { Card, StatCard } from '../../shared/components/Card';

import { decorativePalette } from '@airgate/theme';

const PIE_COLORS = decorativePalette.slice(0, 10);

function fmtNum(n: number | undefined | null): string {
  if (n == null) return '0';
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(2)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(2)}K`;
  return n.toLocaleString();
}

function fmtCost(n: number): string {
  if (n >= 1000) return `$${(n / 1000).toFixed(2)}K`;
  return `$${n.toFixed(4)}`;
}

function todayStr(): string {
  const d = new Date();
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`;
}

export default function UserOverviewPage() {
  const { t } = useTranslation();
  const { user } = useAuth();
  const today = useMemo(todayStr, []);

  // 全部统计
  const { data: allStats } = useQuery({
    queryKey: queryKeys.userUsageStats({}),
    queryFn: () => usageApi.userStats({}),
  });

  // 今日统计
  const { data: todayStats } = useQuery({
    queryKey: queryKeys.userUsageStats({ start_date: today, end_date: today }),
    queryFn: () => usageApi.userStats({ start_date: today, end_date: today }),
  });

  const models = allStats?.by_model ?? [];

  return (
    <div>
      {/* 账户信息 */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
        <StatCard
          title={t('user_overview.balance')}
          value={`$${(user?.balance ?? 0).toFixed(2)}`}
          icon={<Wallet className="w-5 h-5" />}
          accentColor="var(--ag-primary)"
        />
        <StatCard
          title={t('user_overview.max_concurrency')}
          value={String(user?.max_concurrency ?? 0)}
          icon={<Zap className="w-5 h-5" />}
          accentColor="var(--ag-info)"
        />
        <StatCard
          title={t('usage.total_requests')}
          value={(allStats?.total_requests ?? 0).toLocaleString()}
          icon={<Activity className="w-5 h-5" />}
          accentColor="var(--ag-warning)"
        />
        <StatCard
          title={t('usage.actual_cost')}
          value={fmtCost(allStats?.total_actual_cost ?? 0)}
          icon={<Coins className="w-5 h-5" />}
          accentColor="var(--ag-success)"
        />
      </div>

      {/* 今日数据 + 累计数据 */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-4 mb-4">
        <Card
          title={t('user_overview.today')}
          extra={<Clock className="w-4 h-4 text-text-tertiary" />}
        >
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <MiniStat label={t('dashboard.requests')} value={(todayStats?.total_requests ?? 0).toLocaleString()} />
            <MiniStat label="Token" value={fmtNum(todayStats?.total_tokens)} />
            <MiniStat label={t('usage.total_cost')} value={fmtCost(todayStats?.total_cost ?? 0)} color="var(--ag-warning)" />
            <MiniStat label={t('usage.actual_cost')} value={fmtCost(todayStats?.total_actual_cost ?? 0)} color="var(--ag-primary)" />
          </div>
        </Card>
        <Card
          title={t('user_overview.cumulative')}
          extra={<TrendingUp className="w-4 h-4 text-text-tertiary" />}
        >
          <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
            <MiniStat label={t('dashboard.requests')} value={(allStats?.total_requests ?? 0).toLocaleString()} />
            <MiniStat label="Token" value={fmtNum(allStats?.total_tokens)} />
            <MiniStat label={t('usage.total_cost')} value={fmtCost(allStats?.total_cost ?? 0)} color="var(--ag-warning)" />
            <MiniStat label={t('usage.actual_cost')} value={fmtCost(allStats?.total_actual_cost ?? 0)} color="var(--ag-primary)" />
          </div>
        </Card>
      </div>

      {/* 模型分布饼图 */}
      {models.length > 0 && <ModelPieCard models={models} />}
    </div>
  );
}

/* ==================== Mini Stat ====================  */

function MiniStat({ label, value, color }: { label: string; value: string; color?: string }) {
  return (
    <div className="text-center py-2">
      <p className="text-[11px] text-text-tertiary mb-1">{label}</p>
      <p className="text-lg font-bold font-mono" style={color ? { color } : undefined}>{value}</p>
    </div>
  );
}

/* ==================== 模型分布饼图 ==================== */

function ModelPieCard({ models }: { models: Array<{ model: string; requests: number; tokens: number; total_cost: number; actual_cost: number }> }) {
  const { t } = useTranslation();

  const pieData = useMemo(
    () => models.map((m) => ({ name: m.model, value: m.tokens })),
    [models],
  );

  return (
    <Card title={t('dashboard.model_distribution')}>
      <div className="flex flex-col sm:flex-row gap-4">
        <div className="w-44 h-44 flex-shrink-0 mx-auto sm:mx-0">
          <ResponsiveContainer width="100%" height="100%">
            <PieChart>
              <Pie data={pieData} cx="50%" cy="50%" innerRadius={35} outerRadius={65} paddingAngle={2} dataKey="value">
                {pieData.map((_, i) => <Cell key={i} fill={PIE_COLORS[i % PIE_COLORS.length]} />)}
              </Pie>
              <RechartsTooltip
                contentStyle={{ background: 'var(--ag-bg-elevated)', border: '1px solid var(--ag-border)', borderRadius: 8, fontSize: 12 }}
                formatter={(value) => [fmtNum(Number(value)), 'Token']}
              />
            </PieChart>
          </ResponsiveContainer>
        </div>
        <div className="flex-1 overflow-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-text-tertiary">
                <th className="text-left font-medium pb-2">{t('usage.model')}</th>
                <th className="text-right font-medium pb-2">{t('dashboard.requests')}</th>
                <th className="text-right font-medium pb-2">TOKEN</th>
                <th className="text-right font-medium pb-2">{t('usage.cost')}</th>
              </tr>
            </thead>
            <tbody>
              {models.map((m, i) => (
                <tr key={m.model} className="border-t border-border-subtle">
                  <td className="py-1.5 flex items-center gap-1.5">
                    <span className="w-2 h-2 rounded-full flex-shrink-0" style={{ backgroundColor: PIE_COLORS[i % PIE_COLORS.length] }} />
                    <span className="text-text truncate max-w-[120px]">{m.model}</span>
                  </td>
                  <td className="text-right text-text-secondary py-1.5">{m.requests}</td>
                  <td className="text-right text-text-secondary py-1.5 font-mono">{fmtNum(m.tokens)}</td>
                  <td className="text-right py-1.5 font-mono" style={{ color: 'var(--ag-primary)' }}>{fmtCost(m.actual_cost)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>
    </Card>
  );
}
