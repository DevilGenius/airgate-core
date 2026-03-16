import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Plus,
  Pencil,
  Layers,
  ArrowUpDown,
  Lock,
} from 'lucide-react';
import { PageHeader } from '../../shared/components/PageHeader';
import { Button } from '../../shared/components/Button';
import { Input, Select } from '../../shared/components/Input';
import { Table, type Column } from '../../shared/components/Table';
import { Modal } from '../../shared/components/Modal';
import { Badge } from '../../shared/components/Badge';
import { useToast } from '../../shared/components/Toast';
import { PlatformIcon } from '../../shared/components/PlatformIcon';
import { groupsApi } from '../../shared/api/groups';
import { usePlatforms } from '../../shared/hooks/usePlatforms';
import type { GroupResp, CreateGroupReq, UpdateGroupReq } from '../../shared/types';

const PAGE_SIZE = 20;

export default function GroupsPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const queryClient = useQueryClient();
  const { platforms, platformName } = usePlatforms();

  const PLATFORM_OPTIONS = [
    { value: '', label: t('groups.all_platforms') },
    ...platforms.map((p) => ({ value: p, label: platformName(p) })),
  ];

  // 筛选状态
  const [page, setPage] = useState(1);
  const [platformFilter, setPlatformFilter] = useState('');

  // 弹窗状态
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [editingGroup, setEditingGroup] = useState<GroupResp | null>(null);

  // 查询分组列表
  const { data, isLoading } = useQuery({
    queryKey: ['groups', page, platformFilter],
    queryFn: () =>
      groupsApi.list({
        page,
        page_size: PAGE_SIZE,
        platform: platformFilter || undefined,
      }),
  });

  // 创建分组
  const createMutation = useMutation({
    mutationFn: (data: CreateGroupReq) => groupsApi.create(data),
    onSuccess: () => {
      toast('success', t('groups.create_success'));
      setShowCreateModal(false);
      queryClient.invalidateQueries({ queryKey: ['groups'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 更新分组
  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: UpdateGroupReq }) =>
      groupsApi.update(id, data),
    onSuccess: () => {
      toast('success', t('groups.update_success'));
      setEditingGroup(null);
      queryClient.invalidateQueries({ queryKey: ['groups'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 表格列定义
  const columns: Column<GroupResp>[] = [
    {
      key: 'id',
      title: t('common.id'),
      width: '60px',
      render: (row) => (
        <span className="font-mono">
          {row.id}
        </span>
      ),
    },
    {
      key: 'name',
      title: t('common.name'),
      render: (row) => (
        <span className="inline-flex items-center gap-1.5">
          <Layers className="w-3.5 h-3.5" style={{ color: 'var(--ag-text-tertiary)' }} />
          <span style={{ color: 'var(--ag-text)' }} className="font-medium">
            {row.name}
          </span>
        </span>
      ),
    },
    {
      key: 'platform',
      title: t('groups.platform'),
      render: (row) => (
        <span className="inline-flex items-center gap-1.5">
          <PlatformIcon platform={row.platform} className="w-3.5 h-3.5" />
          {platformName(row.platform)}
        </span>
      ),
    },
    {
      key: 'subscription_type',
      title: t('groups.subscription_type'),
      render: (row) => (
        <Badge variant={row.subscription_type === 'subscription' ? 'info' : 'default'}>
          {row.subscription_type === 'subscription' ? t('groups.type_subscription') : t('groups.type_standard')}
        </Badge>
      ),
    },
    {
      key: 'rate_multiplier',
      title: t('groups.rate_multiplier'),
      width: '80px',
      render: (row) => (
        <span className="font-mono" style={{ color: 'var(--ag-primary)' }}>
          {row.rate_multiplier}x
        </span>
      ),
    },
    {
      key: 'is_exclusive',
      title: t('groups.exclusive'),
      width: '80px',
      render: (row) =>
        row.is_exclusive ? (
          <span className="inline-flex items-center gap-1" style={{ color: 'var(--ag-warning)' }}>
            <Lock className="w-3.5 h-3.5" />
            {t('common.yes')}
          </span>
        ) : (
          <span style={{ color: 'var(--ag-text-tertiary)' }}>{t('common.no')}</span>
        ),
    },
    {
      key: 'sort_weight',
      title: t('groups.sort_weight'),
      width: '100px',
      render: (row) => (
        <span className="inline-flex items-center gap-1 font-mono">
          <ArrowUpDown className="w-3 h-3" style={{ color: 'var(--ag-text-tertiary)' }} />
          {row.sort_weight}
        </span>
      ),
    },
    {
      key: 'actions',
      title: t('common.actions'),
      render: (row) => (
        <Button
          size="sm"
          variant="ghost"
          icon={<Pencil className="w-3.5 h-3.5" />}
          onClick={() => setEditingGroup(row)}
        >
          {t('common.edit')}
        </Button>
      ),
    },
  ];

  return (
    <div>
      <PageHeader
        title={t('groups.title')}
        description={t('groups.description')}
        actions={
          <Button icon={<Plus className="w-4 h-4" />} onClick={() => setShowCreateModal(true)}>
            {t('groups.create')}
          </Button>
        }
      />

      {/* 筛选 */}
      <div className="flex items-center gap-3 mb-5">
        <Select
          value={platformFilter}
          onChange={(e) => {
            setPlatformFilter(e.target.value);
            setPage(1);
          }}
          options={PLATFORM_OPTIONS}
          label={t('groups.platform')}
        />
      </div>

      {/* 表格 */}
      <Table<GroupResp>
        columns={columns}
        data={data?.list ?? []}
        loading={isLoading}
        rowKey={(row) => row.id}
        page={page}
        pageSize={PAGE_SIZE}
        total={data?.total ?? 0}
        onPageChange={setPage}
      />

      {/* 创建弹窗 */}
      <GroupFormModal
        open={showCreateModal}
        title={t('groups.create')}
        onClose={() => setShowCreateModal(false)}
        onSubmit={(data) => createMutation.mutate(data as CreateGroupReq)}
        loading={createMutation.isPending}
        platforms={platforms}
      />

      {/* 编辑弹窗 */}
      {editingGroup && (
        <GroupFormModal
          open
          title={t('groups.edit')}
          group={editingGroup}
          onClose={() => setEditingGroup(null)}
          onSubmit={(data) =>
            updateMutation.mutate({ id: editingGroup.id, data })
          }
          loading={updateMutation.isPending}
          platforms={platforms}
        />
      )}
    </div>
  );
}

// ==================== 分组表单弹窗 ====================

// 从 quotas 对象解析为结构化值
function parseQuotas(quotas?: Record<string, unknown>): { daily: string; weekly: string; monthly: string } {
  return {
    daily: quotas?.daily ? String(quotas.daily) : '',
    weekly: quotas?.weekly ? String(quotas.weekly) : '',
    monthly: quotas?.monthly ? String(quotas.monthly) : '',
  };
}

// 从结构化值组装回 quotas 对象
function buildQuotas(q: { daily: string; weekly: string; monthly: string }): Record<string, unknown> | undefined {
  const result: Record<string, number> = {};
  if (q.daily && Number(q.daily) > 0) result.daily = Number(q.daily);
  if (q.weekly && Number(q.weekly) > 0) result.weekly = Number(q.weekly);
  if (q.monthly && Number(q.monthly) > 0) result.monthly = Number(q.monthly);
  return Object.keys(result).length > 0 ? result : undefined;
}

function GroupFormModal({
  open,
  title,
  group,
  onClose,
  onSubmit,
  loading,
  platforms,
}: {
  open: boolean;
  title: string;
  group?: GroupResp;
  onClose: () => void;
  onSubmit: (data: CreateGroupReq | UpdateGroupReq) => void;
  loading: boolean;
  platforms: string[];
}) {
  const { t } = useTranslation();
  const isEdit = !!group;

  const [form, setForm] = useState({
    name: group?.name ?? '',
    platform: group?.platform ?? '',
    rate_multiplier: group?.rate_multiplier ?? 1,
    is_exclusive: group?.is_exclusive ?? false,
    subscription_type: group?.subscription_type ?? 'standard' as const,
    sort_weight: group?.sort_weight ?? 0,
  });

  const [quotas, setQuotas] = useState(
    parseQuotas(group?.quotas as Record<string, unknown> | undefined),
  );

  const handleSubmit = () => {
    if (!isEdit && (!form.name || !form.platform)) return;

    onSubmit({
      ...form,
      subscription_type: form.subscription_type as 'standard' | 'subscription',
      quotas: form.subscription_type === 'subscription' ? buildQuotas(quotas) : undefined,
    });
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={title}
      width="560px"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={handleSubmit} loading={loading}>
            {isEdit ? t('common.save') : t('common.create')}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Input
          label={t('common.name')}
          required
          value={form.name}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
          icon={<Layers className="w-4 h-4" />}
        />

        {isEdit ? (
          <Input label={t('groups.platform')} value={form.platform} disabled />
        ) : (
          <Select
            label={t('groups.platform')}
            required
            value={form.platform}
            onChange={(e) => setForm({ ...form, platform: e.target.value })}
            options={[
              { value: '', label: t('groups.select_platform') },
              ...platforms.map((p) => ({ value: p, label: p })),
            ]}
          />
        )}

        <Input
          label={t('groups.rate_multiplier')}
          type="number"
          step="0.1"
          value={String(form.rate_multiplier)}
          onChange={(e) =>
            setForm({ ...form, rate_multiplier: Number(e.target.value) })
          }
        />

        <div className="flex items-center justify-between">
          <span className="text-sm" style={{ color: 'var(--ag-text-secondary)' }}>
            {t('groups.exclusive_hint')}
          </span>
          <button
            type="button"
            role="switch"
            aria-checked={form.is_exclusive}
            onClick={() => setForm({ ...form, is_exclusive: !form.is_exclusive })}
            className="relative inline-flex h-5 w-9 items-center rounded-full transition-colors"
            style={{
              backgroundColor: form.is_exclusive ? 'var(--ag-primary)' : 'var(--ag-glass-border)',
            }}
          >
            <span
              className="inline-block h-3.5 w-3.5 rounded-full bg-white transition-transform"
              style={{
                transform: form.is_exclusive ? 'translateX(18px)' : 'translateX(3px)',
              }}
            />
          </button>
        </div>

        <Select
          label={t('groups.subscription_type')}
          value={form.subscription_type}
          onChange={(e) =>
            setForm({
              ...form,
              subscription_type: e.target.value as 'standard' | 'subscription',
            })
          }
          options={[
            { value: 'standard', label: t('groups.type_standard') },
            { value: 'subscription', label: t('groups.type_subscription') },
          ]}
        />

        <Input
          label={t('groups.sort_weight')}
          type="number"
          value={String(form.sort_weight)}
          onChange={(e) =>
            setForm({ ...form, sort_weight: Number(e.target.value) })
          }
          hint={t('groups.sort_weight_hint')}
          icon={<ArrowUpDown className="w-4 h-4" />}
        />

        {/* 配额限制 —— 仅订阅制显示 */}
        {form.subscription_type === 'subscription' && (
          <div>
            <label className="block text-xs font-medium text-text-secondary uppercase tracking-wider mb-1.5">
              {t('groups.quotas')}
            </label>
            <p className="text-[11px] mb-2" style={{ color: 'var(--ag-text-tertiary)' }}>
              {t('groups.quota_hint')}
            </p>
            <div className="grid grid-cols-3 gap-3">
              <Input
                label={t('groups.quota_daily')}
                type="number"
                min="0"
                value={quotas.daily}
                onChange={(e) => setQuotas({ ...quotas, daily: e.target.value })}
              />
              <Input
                label={t('groups.quota_weekly')}
                type="number"
                min="0"
                value={quotas.weekly}
                onChange={(e) => setQuotas({ ...quotas, weekly: e.target.value })}
              />
              <Input
                label={t('groups.quota_monthly')}
                type="number"
                min="0"
                value={quotas.monthly}
                onChange={(e) => setQuotas({ ...quotas, monthly: e.target.value })}
              />
            </div>
          </div>
        )}
      </div>
    </Modal>
  );
}
