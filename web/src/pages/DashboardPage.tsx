import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { PageHeader } from '../shared/components/PageHeader';
import { StatCard } from '../shared/components/Card';
import { dashboardApi } from '../shared/api/dashboard';
import {
  Users,
  KeyRound,
  FolderTree,
  Key,
  Activity,
  Coins,
  DollarSign,
  Puzzle,
} from 'lucide-react';

/** 格式化数字 */
function formatNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

/** 格式化金额 */
function formatCurrency(n: number): string {
  return `$${n.toFixed(2)}`;
}

// 统计卡片配置，titleKey 对应 i18n key
const statConfigs = [
  { key: 'total_users', titleKey: 'dashboard.total_users', icon: Users, color: 'var(--ag-primary)' },
  { key: 'total_accounts', titleKey: 'dashboard.total_accounts', icon: KeyRound, color: 'var(--ag-info)' },
  { key: 'total_groups', titleKey: 'dashboard.total_groups', icon: FolderTree, color: 'var(--ag-success)' },
  { key: 'total_api_keys', titleKey: 'dashboard.total_api_keys', icon: Key, color: 'var(--ag-warning)' },
  { key: 'total_requests', titleKey: 'dashboard.today_requests', icon: Activity, color: 'var(--ag-primary)' },
  { key: 'total_tokens', titleKey: 'dashboard.today_tokens', icon: Coins, color: 'var(--ag-info)' },
  { key: 'total_revenue', titleKey: 'dashboard.today_revenue', icon: DollarSign, color: 'var(--ag-success)', isCurrency: true },
  { key: 'active_plugins', titleKey: 'dashboard.active_plugins', icon: Puzzle, color: 'var(--ag-warning)' },
] as const;

export default function DashboardPage() {
  const { t } = useTranslation();

  const { data, isLoading, error } = useQuery({
    queryKey: ['dashboard', 'stats'],
    queryFn: () => dashboardApi.stats(),
  });

  return (
    <div>
      <PageHeader title={t('dashboard.title')} description={t('dashboard.description')} />

      {/* 加载骨架 */}
      {isLoading && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <div
              key={i}
              className="rounded-lg border border-glass-border bg-bg-elevated p-5"
              style={{ animationDelay: `${i * 60}ms` }}
            >
              <div className="space-y-3">
                <div className="h-3 w-16 ag-shimmer rounded" />
                <div className="h-7 w-20 ag-shimmer rounded" />
              </div>
            </div>
          ))}
        </div>
      )}

      {/* 错误 */}
      {error && (
        <div className="rounded-md bg-danger-subtle border border-danger border-opacity-20 px-4 py-3 text-sm text-danger">
          {t('dashboard.load_failed', { error: error instanceof Error ? error.message : t('common.unknown_error') })}
        </div>
      )}

      {/* 数据 */}
      {data && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          {statConfigs.map((config, i) => {
            const Icon = config.icon;
            const rawValue = data[config.key as keyof typeof data] as number;
            const value = 'isCurrency' in config && config.isCurrency
              ? formatCurrency(rawValue)
              : formatNumber(rawValue);

            return (
              <div key={config.key} style={{ animation: `ag-slide-up 0.4s ease-out ${i * 50}ms both` }}>
                <StatCard
                  title={t(config.titleKey)}
                  value={value}
                  icon={<Icon className="w-5 h-5" />}
                  accentColor={config.color}
                />
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
