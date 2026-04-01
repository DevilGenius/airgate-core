import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { usageApi } from '../../shared/api/usage';
import { usePagination } from '../../shared/hooks/usePagination';
import { Table, type Column } from '../../shared/components/Table';
import { Input, Select } from '../../shared/components/Input';
import { DatePicker } from '../../shared/components/DatePicker';
import { StatCard } from '../../shared/components/Card';
import { Badge } from '../../shared/components/Badge';
import { usePlatforms } from '../../shared/hooks/usePlatforms';
import { Activity, Hash, DollarSign, Coins, Search } from 'lucide-react';
import type { UsageLogResp, UsageQuery } from '../../shared/types';

/** 大数字友好显示：33518599 → "33.52M"，1234 → "1,234" */
function formatLargeNumber(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(2)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 10_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

export default function UserUsagePage() {
  const { t } = useTranslation();
  const { page, setPage, pageSize, setPageSize } = usePagination(20);
  const [filters, setFilters] = useState<Partial<UsageQuery>>({});

  const queryParams: UsageQuery = {
    page,
    page_size: pageSize,
    ...filters,
  };

  const { platforms, platformName } = usePlatforms();

  const { data, isLoading } = useQuery({
    queryKey: ['user-usage', queryParams],
    queryFn: () => usageApi.list(queryParams),
  });

  // 聚合统计（跟随筛选条件，独立于分页）
  const { data: stats } = useQuery({
    queryKey: ['user-usage-stats', filters],
    queryFn: () => usageApi.userStats(filters),
  });

  function updateFilter(key: string, value: string) {
    setFilters((prev) => ({ ...prev, [key]: value || undefined }));
    setPage(1);
  }

  const list = data?.list ?? [];
  const total = data?.total ?? 0;

  const columns: Column<UsageLogResp>[] = [
    {
      key: 'created_at',
      title: t('usage.time'),
      width: '168px',
      render: (row) => (
        <span className="text-text-secondary">
          {new Date(row.created_at).toLocaleString('zh-CN')}
        </span>
      ),
    },
    {
      key: 'model',
      title: t('usage.model'),
      width: '220px',
      render: (row) => (
        <span className="block max-w-full truncate text-text" title={row.model}>
          {row.model}
        </span>
      ),
    },
    {
      key: 'input_tokens',
      title: t('usage.input_tokens'),
      width: '120px',
      render: (row) => (
        <span className="font-mono">{row.input_tokens.toLocaleString()}</span>
      ),
    },
    {
      key: 'output_tokens',
      title: t('usage.output_tokens'),
      width: '120px',
      render: (row) => (
        <span className="font-mono">{row.output_tokens.toLocaleString()}</span>
      ),
    },
    {
      key: 'cached_input_tokens',
      title: t('usage.cached_input_tokens'),
      width: '132px',
      render: (row) => (
        <span className="font-mono text-text-secondary">
          {row.cached_input_tokens > 0 ? row.cached_input_tokens.toLocaleString() : '-'}
        </span>
      ),
    },
    {
      key: 'total_cost',
      title: t('usage.total_cost'),
      width: '124px',
      render: (row) => (
        <span className="font-mono">${row.total_cost.toFixed(6)}</span>
      ),
    },
    {
      key: 'actual_cost',
      title: t('usage.actual_cost'),
      width: '124px',
      render: (row) => (
        <span className="font-mono">${row.actual_cost.toFixed(6)}</span>
      ),
    },
    {
      key: 'stream',
      title: t('usage.stream'),
      width: '84px',
      render: (row) => (
        <Badge variant={row.stream ? 'info' : 'default'}>
          {row.stream ? t('common.yes') : t('common.no')}
        </Badge>
      ),
    },
    {
      key: 'duration_ms',
      title: t('usage.duration'),
      width: '96px',
      render: (row) => (
        <span className="font-mono">{row.duration_ms}ms</span>
      ),
    },
  ];

  return (
    <div>
      {/* 筛选栏 */}
      <div className="flex items-center gap-3 mb-5 flex-wrap">
        <div className="w-44">
          <DatePicker
            placeholder={t('usage.start_date')}
            value={filters.start_date || ''}
            onChange={(v) => updateFilter('start_date', v)}
          />
        </div>
        <div className="w-44">
          <DatePicker
            placeholder={t('usage.end_date')}
            value={filters.end_date || ''}
            onChange={(v) => updateFilter('end_date', v)}
          />
        </div>
        <div className="w-40">
          <Select
            placeholder={t('common.all')}
            value={filters.platform || ''}
            onChange={(e) => updateFilter('platform', e.target.value)}
            options={[
              { label: t('common.all'), value: '' },
              ...platforms.map((p) => ({ label: platformName(p), value: p })),
            ]}
          />
        </div>
        <div className="w-40">
          <Input
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
          value={(stats?.total_requests ?? 0).toLocaleString()}
          icon={<Activity className="w-5 h-5" />}
          accentColor="var(--ag-primary)"
        />
        <StatCard
          title={t('usage.total_tokens')}
          value={formatLargeNumber(stats?.total_tokens ?? 0)}
          icon={<Hash className="w-5 h-5" />}
          accentColor="var(--ag-info)"
        />
        <StatCard
          title={t('usage.total_cost')}
          value={`$${(stats?.total_cost ?? 0).toFixed(4)}`}
          icon={<DollarSign className="w-5 h-5" />}
          accentColor="var(--ag-warning)"
        />
        <StatCard
          title={t('usage.actual_cost')}
          value={`$${(stats?.total_actual_cost ?? 0).toFixed(4)}`}
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
        pageSize={pageSize}
        total={total}
        onPageChange={setPage}
        onPageSizeChange={setPageSize}
        separateHeader
      />
    </div>
  );
}
