import { useState } from 'react';
import { useQuery } from '@tanstack/react-query';
import { usageApi } from '../../shared/api/usage';
import { PageHeader } from '../../shared/components/PageHeader';
import { Table, type Column } from '../../shared/components/Table';
import { Input } from '../../shared/components/Input';
import { Card, StatCard } from '../../shared/components/Card';
import { Badge } from '../../shared/components/Badge';
import type { UsageLogResp, UsageQuery } from '../../shared/types';

export default function UsagePage() {
  const [page, setPage] = useState(1);
  const [filters, setFilters] = useState<Partial<UsageQuery>>({});
  const [statsGroupBy, setStatsGroupBy] = useState<string>('model');

  // 构建查询参数
  const queryParams: UsageQuery = {
    page,
    page_size: 20,
    ...filters,
  };

  // 使用记录列表
  const { data, isLoading } = useQuery({
    queryKey: ['admin-usage', queryParams],
    queryFn: () => usageApi.adminList(queryParams),
  });

  // 聚合统计
  const { data: stats } = useQuery({
    queryKey: ['admin-usage-stats', statsGroupBy, filters.start_date, filters.end_date],
    queryFn: () =>
      usageApi.stats({
        group_by: statsGroupBy,
        start_date: filters.start_date,
        end_date: filters.end_date,
      }),
  });

  function updateFilter(key: string, value: string) {
    setFilters((prev) => ({ ...prev, [key]: value || undefined }));
    setPage(1);
  }

  const columns: Column<UsageLogResp>[] = [
    {
      key: 'created_at',
      title: '时间',
      render: (row) => new Date(row.created_at).toLocaleString('zh-CN'),
    },
    { key: 'user_id', title: '用户', render: (row) => `#${row.user_id}` },
    { key: 'model', title: '模型' },
    {
      key: 'input_tokens',
      title: '输入Token',
      render: (row) => row.input_tokens.toLocaleString(),
    },
    {
      key: 'output_tokens',
      title: '输出Token',
      render: (row) => row.output_tokens.toLocaleString(),
    },
    {
      key: 'total_cost',
      title: '总费用',
      render: (row) => `$${row.total_cost.toFixed(6)}`,
    },
    {
      key: 'actual_cost',
      title: '实际费用',
      render: (row) => `$${row.actual_cost.toFixed(6)}`,
    },
    {
      key: 'stream',
      title: '流式',
      render: (row) => (
        <Badge variant={row.stream ? 'info' : 'default'}>
          {row.stream ? '是' : '否'}
        </Badge>
      ),
    },
    {
      key: 'duration_ms',
      title: '耗时',
      render: (row) => `${row.duration_ms}ms`,
    },
  ];

  return (
    <div className="p-6">
      <PageHeader title="使用记录" />

      {/* 筛选栏 */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
        <Input
          label="开始日期"
          type="date"
          value={filters.start_date || ''}
          onChange={(e) => updateFilter('start_date', e.target.value)}
        />
        <Input
          label="结束日期"
          type="date"
          value={filters.end_date || ''}
          onChange={(e) => updateFilter('end_date', e.target.value)}
        />
        <Input
          label="平台"
          placeholder="例如：openai"
          value={filters.platform || ''}
          onChange={(e) => updateFilter('platform', e.target.value)}
        />
        <Input
          label="模型"
          placeholder="例如：gpt-4"
          value={filters.model || ''}
          onChange={(e) => updateFilter('model', e.target.value)}
        />
        <Input
          label="用户ID"
          type="number"
          placeholder="用户ID"
          value={filters.user_id ?? ''}
          onChange={(e) => updateFilter('user_id', e.target.value)}
        />
      </div>

      {/* 聚合统计 */}
      {stats && (
        <div className="mb-6">
          <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-4">
            <StatCard
              title="总请求数"
              value={stats.total_requests.toLocaleString()}
            />
            <StatCard
              title="总Token"
              value={stats.total_tokens.toLocaleString()}
            />
            <StatCard
              title="总费用"
              value={`$${stats.total_cost.toFixed(4)}`}
            />
            <StatCard
              title="实际费用"
              value={`$${stats.total_actual_cost.toFixed(4)}`}
            />
          </div>

          {/* 分组统计切换 */}
          <Card>
            <div className="flex items-center gap-2 mb-4">
              <span className="text-sm text-gray-500">分组统计：</span>
              {['model', 'user', 'account', 'group'].map((g) => (
                <button
                  key={g}
                  className={`px-3 py-1 text-sm rounded-md transition-colors ${
                    statsGroupBy === g
                      ? 'bg-indigo-100 text-indigo-700 font-medium'
                      : 'text-gray-600 hover:bg-gray-100'
                  }`}
                  onClick={() => setStatsGroupBy(g)}
                >
                  {{
                    model: '按模型',
                    user: '按用户',
                    account: '按账号',
                    group: '按分组',
                  }[g]}
                </button>
              ))}
            </div>

            {/* 按模型 */}
            {statsGroupBy === 'model' && stats.by_model && (
              <div className="overflow-x-auto">
                <table className="min-w-full text-sm">
                  <thead>
                    <tr className="border-b">
                      <th className="text-left py-2 pr-4 text-gray-500">模型</th>
                      <th className="text-left py-2 pr-4 text-gray-500">请求数</th>
                      <th className="text-left py-2 pr-4 text-gray-500">Token</th>
                      <th className="text-left py-2 text-gray-500">费用</th>
                    </tr>
                  </thead>
                  <tbody>
                    {stats.by_model.map((s) => (
                      <tr key={s.model} className="border-b last:border-0">
                        <td className="py-2 pr-4 font-medium">{s.model}</td>
                        <td className="py-2 pr-4">{s.requests.toLocaleString()}</td>
                        <td className="py-2 pr-4">{s.tokens.toLocaleString()}</td>
                        <td className="py-2">${s.total_cost.toFixed(4)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            {/* 按用户 */}
            {statsGroupBy === 'user' && stats.by_user && (
              <div className="overflow-x-auto">
                <table className="min-w-full text-sm">
                  <thead>
                    <tr className="border-b">
                      <th className="text-left py-2 pr-4 text-gray-500">用户</th>
                      <th className="text-left py-2 pr-4 text-gray-500">请求数</th>
                      <th className="text-left py-2 text-gray-500">费用</th>
                    </tr>
                  </thead>
                  <tbody>
                    {stats.by_user.map((s) => (
                      <tr key={s.user_id} className="border-b last:border-0">
                        <td className="py-2 pr-4 font-medium">{s.email}</td>
                        <td className="py-2 pr-4">{s.requests.toLocaleString()}</td>
                        <td className="py-2">${s.total_cost.toFixed(4)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            {/* 按账号 */}
            {statsGroupBy === 'account' && stats.by_account && (
              <div className="overflow-x-auto">
                <table className="min-w-full text-sm">
                  <thead>
                    <tr className="border-b">
                      <th className="text-left py-2 pr-4 text-gray-500">账号</th>
                      <th className="text-left py-2 pr-4 text-gray-500">请求数</th>
                      <th className="text-left py-2 text-gray-500">费用</th>
                    </tr>
                  </thead>
                  <tbody>
                    {stats.by_account.map((s) => (
                      <tr key={s.account_id} className="border-b last:border-0">
                        <td className="py-2 pr-4 font-medium">{s.name}</td>
                        <td className="py-2 pr-4">{s.requests.toLocaleString()}</td>
                        <td className="py-2">${s.total_cost.toFixed(4)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}

            {/* 按分组 */}
            {statsGroupBy === 'group' && stats.by_group && (
              <div className="overflow-x-auto">
                <table className="min-w-full text-sm">
                  <thead>
                    <tr className="border-b">
                      <th className="text-left py-2 pr-4 text-gray-500">分组</th>
                      <th className="text-left py-2 pr-4 text-gray-500">请求数</th>
                      <th className="text-left py-2 text-gray-500">费用</th>
                    </tr>
                  </thead>
                  <tbody>
                    {stats.by_group.map((s) => (
                      <tr key={s.group_id} className="border-b last:border-0">
                        <td className="py-2 pr-4 font-medium">{s.name}</td>
                        <td className="py-2 pr-4">{s.requests.toLocaleString()}</td>
                        <td className="py-2">${s.total_cost.toFixed(4)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Card>
        </div>
      )}

      {/* 使用记录表格 */}
      <Table
        columns={columns}
        data={(data?.list ?? [])}
        loading={isLoading}
        rowKey={(row) => row.id as number}
        page={page}
        pageSize={20}
        total={data?.total ?? 0}
        onPageChange={setPage}
      />
    </div>
  );
}
