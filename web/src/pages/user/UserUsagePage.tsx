import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { usageApi } from '../../shared/api/usage';
import { Table, type Column } from '../../shared/components/Table';
import { Input } from '../../shared/components/Input';
import { DatePicker } from '../../shared/components/DatePicker';
import { StatCard } from '../../shared/components/Card';
import { Badge } from '../../shared/components/Badge';
import { Activity, Hash, DollarSign, Coins, Search } from 'lucide-react';
import type { UsageLogResp, UsageQuery } from '../../shared/types';

export default function UserUsagePage() {
  const { t } = useTranslation();
  const [page, setPage] = useState(1);
  const [filters, setFilters] = useState<Partial<UsageQuery>>({});

  const queryParams: UsageQuery = {
    page,
    page_size: 20,
    ...filters,
  };

  const { data, isLoading } = useQuery({
    queryKey: ['user-usage', queryParams],
    queryFn: () => usageApi.list(queryParams),
  });

  function updateFilter(key: string, value: string) {
    setFilters((prev) => ({ ...prev, [key]: value || undefined }));
    setPage(1);
  }

  // 从返回数据中汇总统计
  const list = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalTokens = list.reduce((sum, r) => sum + r.input_tokens + r.output_tokens, 0);
  const totalCost = list.reduce((sum, r) => sum + r.total_cost, 0);
  const actualCost = list.reduce((sum, r) => sum + r.actual_cost, 0);

  const columns: Column<UsageLogResp>[] = [
    {
      key: 'created_at',
      title: t('usage.time'),
      render: (row) => (
        <span className="text-text-secondary">
          {new Date(row.created_at).toLocaleString('zh-CN')}
        </span>
      ),
    },
    {
      key: 'model',
      title: t('usage.model'),
      render: (row) => <span className="text-text">{row.model}</span>,
    },
    {
      key: 'input_tokens',
      title: t('usage.input_tokens'),
      render: (row) => (
        <span className="font-mono">{row.input_tokens.toLocaleString()}</span>
      ),
    },
    {
      key: 'output_tokens',
      title: t('usage.output_tokens'),
      render: (row) => (
        <span className="font-mono">{row.output_tokens.toLocaleString()}</span>
      ),
    },
    {
      key: 'total_cost',
      title: t('usage.total_cost'),
      render: (row) => (
        <span className="font-mono">${row.total_cost.toFixed(6)}</span>
      ),
    },
    {
      key: 'actual_cost',
      title: t('usage.actual_cost'),
      render: (row) => (
        <span className="font-mono">${row.actual_cost.toFixed(6)}</span>
      ),
    },
    {
      key: 'stream',
      title: t('usage.stream'),
      render: (row) => (
        <Badge variant={row.stream ? 'info' : 'default'}>
          {row.stream ? t('common.yes') : t('common.no')}
        </Badge>
      ),
    },
    {
      key: 'duration_ms',
      title: t('usage.duration'),
      render: (row) => (
        <span className="font-mono">{row.duration_ms}ms</span>
      ),
    },
  ];

  return (
    <div>
      {/* 筛选栏 */}
      <div className="flex items-end gap-3 mb-5 flex-wrap">
        <div className="w-44">
          <DatePicker
            label={t('usage.start_date')}
            value={filters.start_date || ''}
            onChange={(v) => updateFilter('start_date', v)}
          />
        </div>
        <div className="w-44">
          <DatePicker
            label={t('usage.end_date')}
            value={filters.end_date || ''}
            onChange={(v) => updateFilter('end_date', v)}
          />
        </div>
        <div className="w-40">
          <Input
            label={t('usage.platform')}
            placeholder={t('usage.platform_placeholder')}
            value={filters.platform || ''}
            onChange={(e) => updateFilter('platform', e.target.value)}
            icon={<Search className="w-4 h-4" />}
          />
        </div>
        <div className="w-40">
          <Input
            label={t('usage.model')}
            placeholder={t('usage.model_placeholder')}
            value={filters.model || ''}
            onChange={(e) => updateFilter('model', e.target.value)}
            icon={<Search className="w-4 h-4" />}
          />
        </div>
      </div>

      {/* 概览统计 */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4 mb-6">
        <StatCard
          title={t('usage.total_requests')}
          value={total.toLocaleString()}
          icon={<Activity className="w-5 h-5" />}
          accentColor="var(--ag-primary)"
        />
        <StatCard
          title={t('usage.total_tokens')}
          value={totalTokens.toLocaleString()}
          icon={<Hash className="w-5 h-5" />}
          accentColor="var(--ag-info)"
        />
        <StatCard
          title={t('usage.total_cost')}
          value={`$${totalCost.toFixed(4)}`}
          icon={<DollarSign className="w-5 h-5" />}
          accentColor="var(--ag-warning)"
        />
        <StatCard
          title={t('usage.actual_cost')}
          value={`$${actualCost.toFixed(4)}`}
          icon={<Coins className="w-5 h-5" />}
          accentColor="var(--ag-success)"
        />
      </div>

      {/* 使用记录表格 */}
      <Table
        columns={columns}
        data={list}
        loading={isLoading}
        rowKey={(row) => row.id as number}
        page={page}
        pageSize={20}
        total={total}
        onPageChange={setPage}
      />
    </div>
  );
}
