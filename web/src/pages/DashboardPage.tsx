import { useQuery } from '@tanstack/react-query';
import { PageHeader } from '../shared/components/PageHeader';
import { StatCard } from '../shared/components/Card';
import { dashboardApi } from '../shared/api/dashboard';

/** 格式化数字：超过 1000 用 k 表示，超过 100 万用 M 表示 */
function formatNumber(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`;
  return String(n);
}

/** 格式化金额 */
function formatCurrency(n: number): string {
  return `$${n.toFixed(2)}`;
}

export default function DashboardPage() {
  const { data, isLoading, error } = useQuery({
    queryKey: ['dashboard', 'stats'],
    queryFn: () => dashboardApi.stats(),
  });

  return (
    <div className="p-6">
      <PageHeader title="仪表盘" description="系统运行概览" />

      {/* 加载态 */}
      {isLoading && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          {Array.from({ length: 8 }).map((_, i) => (
            <div
              key={i}
              className="bg-white rounded-lg border border-gray-200 shadow-sm p-6 animate-pulse"
            >
              <div className="h-4 bg-gray-200 rounded w-20 mb-3" />
              <div className="h-8 bg-gray-200 rounded w-16" />
            </div>
          ))}
        </div>
      )}

      {/* 错误态 */}
      {error && (
        <div className="rounded-md bg-red-50 border border-red-200 p-4 text-sm text-red-700">
          加载仪表盘数据失败：{error instanceof Error ? error.message : '未知错误'}
        </div>
      )}

      {/* 数据展示 */}
      {data && (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-4">
          <StatCard title="总用户数" value={formatNumber(data.total_users)} />
          <StatCard title="总账号数" value={formatNumber(data.total_accounts)} />
          <StatCard title="总分组数" value={formatNumber(data.total_groups)} />
          <StatCard title="API 密钥数" value={formatNumber(data.total_api_keys)} />
          <StatCard title="今日请求数" value={formatNumber(data.total_requests)} />
          <StatCard title="今日 Token 数" value={formatNumber(data.total_tokens)} />
          <StatCard title="今日收入" value={formatCurrency(data.total_revenue)} />
          <StatCard title="活跃插件数" value={data.active_plugins} />
        </div>
      )}
    </div>
  );
}
