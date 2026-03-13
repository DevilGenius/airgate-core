import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Plus,
  Users,
  Settings2,
  Layers,
  CalendarDays,
  User,
} from 'lucide-react';
import { PageHeader } from '../../shared/components/PageHeader';
import { Button } from '../../shared/components/Button';
import { Input, Select } from '../../shared/components/Input';
import { Table, type Column } from '../../shared/components/Table';
import { Modal } from '../../shared/components/Modal';
import { StatusBadge } from '../../shared/components/Badge';
import { useToast } from '../../shared/components/Toast';
import { subscriptionsApi } from '../../shared/api/subscriptions';
import { groupsApi } from '../../shared/api/groups';
import { usersApi } from '../../shared/api/users';
import type {
  SubscriptionResp,
  AssignSubscriptionReq,
  BulkAssignReq,
  AdjustSubscriptionReq,
  GroupResp,
  UserResp,
} from '../../shared/types';

const PAGE_SIZE = 20;

export default function SubscriptionsPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const queryClient = useQueryClient();

  const STATUS_OPTIONS = [
    { value: '', label: t('subscriptions.all_status') },
    { value: 'active', label: t('status.active') },
    { value: 'expired', label: t('status.expired') },
    { value: 'suspended', label: t('status.suspended') },
  ];

  // 筛选状态
  const [page, setPage] = useState(1);
  const [statusFilter, setStatusFilter] = useState('');

  // 弹窗状态
  const [showAssignModal, setShowAssignModal] = useState(false);
  const [showBulkModal, setShowBulkModal] = useState(false);
  const [adjustingSub, setAdjustingSub] = useState<SubscriptionResp | null>(null);

  // 查询订阅列表
  const { data, isLoading } = useQuery({
    queryKey: ['subscriptions', page, statusFilter],
    queryFn: () =>
      subscriptionsApi.adminList({
        page,
        page_size: PAGE_SIZE,
        status: statusFilter || undefined,
      }),
  });

  // 查询分组列表
  const { data: groupsData } = useQuery({
    queryKey: ['groups-all'],
    queryFn: () => groupsApi.list({ page: 1, page_size: 100 }),
  });

  // 查询用户列表（用于选择用户）
  const { data: usersData } = useQuery({
    queryKey: ['users-all'],
    queryFn: () => usersApi.list({ page: 1, page_size: 100 }),
  });

  // 分配订阅
  const assignMutation = useMutation({
    mutationFn: (data: AssignSubscriptionReq) => subscriptionsApi.assign(data),
    onSuccess: () => {
      toast('success', t('subscriptions.assign_success'));
      setShowAssignModal(false);
      queryClient.invalidateQueries({ queryKey: ['subscriptions'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 批量分配
  const bulkMutation = useMutation({
    mutationFn: (data: BulkAssignReq) => subscriptionsApi.bulkAssign(data),
    onSuccess: () => {
      toast('success', t('subscriptions.bulk_success'));
      setShowBulkModal(false);
      queryClient.invalidateQueries({ queryKey: ['subscriptions'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 调整订阅
  const adjustMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: AdjustSubscriptionReq }) =>
      subscriptionsApi.adjust(id, data),
    onSuccess: () => {
      toast('success', t('subscriptions.adjust_success'));
      setAdjustingSub(null);
      queryClient.invalidateQueries({ queryKey: ['subscriptions'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 格式化日期
  const formatDate = (date: string) => {
    return new Date(date).toLocaleDateString('zh-CN', {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    });
  };

  // 查找用户邮箱
  const getUserEmail = (userId: number) => {
    const user = usersData?.list?.find((u: UserResp) => u.id === userId);
    return user ? user.email : `${t('subscriptions.user')} #${userId}`;
  };

  // 表格列定义
  const columns: Column<SubscriptionResp>[] = [
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
      key: 'user_id',
      title: t('subscriptions.user'),
      render: (row) => (
        <span className="inline-flex items-center gap-1.5">
          <User className="w-3.5 h-3.5" style={{ color: 'var(--ag-text-tertiary)' }} />
          {getUserEmail(row.user_id)}
        </span>
      ),
    },
    {
      key: 'group_name',
      title: t('subscriptions.group'),
      render: (row) => (
        <span className="inline-flex items-center gap-1.5">
          <Layers className="w-3.5 h-3.5" style={{ color: 'var(--ag-text-tertiary)' }} />
          <span style={{ color: 'var(--ag-text)' }} className="font-medium">
            {row.group_name}
          </span>
        </span>
      ),
    },
    {
      key: 'effective_at',
      title: t('subscriptions.effective_time'),
      render: (row) => (
        <span className="font-mono">
          {formatDate(row.effective_at)}
        </span>
      ),
    },
    {
      key: 'expires_at',
      title: t('subscriptions.expire_time'),
      render: (row) => (
        <span className="font-mono">
          {formatDate(row.expires_at)}
        </span>
      ),
    },
    {
      key: 'status',
      title: t('common.status'),
      render: (row) => <StatusBadge status={row.status} />,
    },
    {
      key: 'actions',
      title: t('common.actions'),
      render: (row) => (
        <Button
          size="sm"
          variant="ghost"
          icon={<Settings2 className="w-3.5 h-3.5" />}
          onClick={() => setAdjustingSub(row)}
        >
          {t('subscriptions.adjust')}
        </Button>
      ),
    },
  ];

  return (
    <div>
      <PageHeader
        title={t('subscriptions.title')}
        description={t('subscriptions.description')}
        actions={
          <div className="flex gap-2">
            <Button
              variant="secondary"
              icon={<Users className="w-4 h-4" />}
              onClick={() => setShowBulkModal(true)}
            >
              {t('subscriptions.bulk_assign')}
            </Button>
            <Button
              icon={<Plus className="w-4 h-4" />}
              onClick={() => setShowAssignModal(true)}
            >
              {t('subscriptions.assign')}
            </Button>
          </div>
        }
      />

      {/* 筛选 */}
      <div className="flex items-center gap-3 mb-5">
        <Select
          value={statusFilter}
          onChange={(e) => {
            setStatusFilter(e.target.value);
            setPage(1);
          }}
          options={STATUS_OPTIONS}
          label={t('common.status')}
        />
      </div>

      {/* 表格 */}
      <Table<SubscriptionResp>
        columns={columns}
        data={data?.list ?? []}
        loading={isLoading}
        rowKey={(row) => row.id}
        page={page}
        pageSize={PAGE_SIZE}
        total={data?.total ?? 0}
        onPageChange={setPage}
      />

      {/* 分配订阅弹窗 */}
      <AssignModal
        open={showAssignModal}
        groups={groupsData?.list ?? []}
        users={usersData?.list ?? []}
        onClose={() => setShowAssignModal(false)}
        onSubmit={(data) => assignMutation.mutate(data)}
        loading={assignMutation.isPending}
      />

      {/* 批量分配弹窗 */}
      <BulkAssignModal
        open={showBulkModal}
        groups={groupsData?.list ?? []}
        users={usersData?.list ?? []}
        onClose={() => setShowBulkModal(false)}
        onSubmit={(data) => bulkMutation.mutate(data)}
        loading={bulkMutation.isPending}
      />

      {/* 调整弹窗 */}
      {adjustingSub && (
        <AdjustModal
          open
          subscription={adjustingSub}
          onClose={() => setAdjustingSub(null)}
          onSubmit={(data) =>
            adjustMutation.mutate({ id: adjustingSub.id, data })
          }
          loading={adjustMutation.isPending}
        />
      )}
    </div>
  );
}

// ==================== 分配订阅弹窗 ====================

function AssignModal({
  open,
  groups,
  users,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  groups: GroupResp[];
  users: UserResp[];
  onClose: () => void;
  onSubmit: (data: AssignSubscriptionReq) => void;
  loading: boolean;
}) {
  const { t } = useTranslation();
  const [form, setForm] = useState<AssignSubscriptionReq>({
    user_id: 0,
    group_id: 0,
    expires_at: '',
  });

  const handleSubmit = () => {
    if (!form.user_id || !form.group_id || !form.expires_at) return;
    onSubmit(form);
  };

  const handleClose = () => {
    setForm({ user_id: 0, group_id: 0, expires_at: '' });
    onClose();
  };

  const userOptions = [
    { value: '0', label: t('subscriptions.select_user') },
    ...users.map((u) => ({
      value: String(u.id),
      label: `${u.email} (${u.username || '-'})`,
    })),
  ];

  const groupOptions = [
    { value: '0', label: t('subscriptions.select_group') },
    ...groups.map((g) => ({
      value: String(g.id),
      label: `${g.name} (${g.platform})`,
    })),
  ];

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title={t('subscriptions.assign')}
      footer={
        <>
          <Button variant="secondary" onClick={handleClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={handleSubmit} loading={loading}>
            {t('subscriptions.assign')}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Select
          label={t('subscriptions.user')}
          required
          value={String(form.user_id)}
          onChange={(e) =>
            setForm({ ...form, user_id: Number(e.target.value) })
          }
          options={userOptions}
        />

        <Select
          label={t('subscriptions.group')}
          required
          value={String(form.group_id)}
          onChange={(e) =>
            setForm({ ...form, group_id: Number(e.target.value) })
          }
          options={groupOptions}
        />

        <Input
          label={t('subscriptions.expire_time')}
          type="date"
          required
          value={form.expires_at ? form.expires_at.split('T')[0] : ''}
          onChange={(e) =>
            setForm({
              ...form,
              expires_at: e.target.value
                ? `${e.target.value}T23:59:59Z`
                : '',
            })
          }
          icon={<CalendarDays className="w-4 h-4" />}
        />
      </div>
    </Modal>
  );
}

// ==================== 批量分配弹窗 ====================

function BulkAssignModal({
  open,
  groups,
  users,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  groups: GroupResp[];
  users: UserResp[];
  onClose: () => void;
  onSubmit: (data: BulkAssignReq) => void;
  loading: boolean;
}) {
  const { t } = useTranslation();
  const [selectedUserIds, setSelectedUserIds] = useState<number[]>([]);
  const [groupId, setGroupId] = useState(0);
  const [expiresAt, setExpiresAt] = useState('');

  const toggleUser = (userId: number) => {
    setSelectedUserIds((prev) =>
      prev.includes(userId)
        ? prev.filter((id) => id !== userId)
        : [...prev, userId],
    );
  };

  const handleSubmit = () => {
    if (selectedUserIds.length === 0 || !groupId || !expiresAt) return;
    onSubmit({
      user_ids: selectedUserIds,
      group_id: groupId,
      expires_at: expiresAt,
    });
  };

  const handleClose = () => {
    setSelectedUserIds([]);
    setGroupId(0);
    setExpiresAt('');
    onClose();
  };

  const groupOptions = [
    { value: '0', label: t('subscriptions.select_group') },
    ...groups.map((g) => ({
      value: String(g.id),
      label: `${g.name} (${g.platform})`,
    })),
  ];

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title={t('subscriptions.bulk_assign')}
      width="560px"
      footer={
        <>
          <Button variant="secondary" onClick={handleClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={handleSubmit} loading={loading}>
            {t('subscriptions.bulk_assign_count', { count: selectedUserIds.length })}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        {/* 用户多选 */}
        <div className="space-y-1.5">
          <label
            className="block text-xs font-medium uppercase tracking-wider"
            style={{ color: 'var(--ag-text-secondary)' }}
          >
            {t('subscriptions.select_users')} <span style={{ color: 'var(--ag-danger)' }}>*</span>
          </label>
          <div
            className="rounded-md max-h-48 overflow-y-auto p-2 space-y-0.5"
            style={{
              border: '1px solid var(--ag-glass-border)',
              background: 'var(--ag-bg-surface)',
            }}
          >
            {users.map((u) => (
              <label
                key={u.id}
                className="flex items-center gap-2.5 px-2 py-1.5 rounded-sm cursor-pointer transition-colors"
                style={{ color: 'var(--ag-text-secondary)' }}
                onMouseEnter={(e) => {
                  (e.currentTarget as HTMLElement).style.background = 'var(--ag-bg-hover)';
                }}
                onMouseLeave={(e) => {
                  (e.currentTarget as HTMLElement).style.background = 'transparent';
                }}
              >
                <input
                  type="checkbox"
                  checked={selectedUserIds.includes(u.id)}
                  onChange={() => toggleUser(u.id)}
                  className="rounded"
                  style={{
                    borderColor: 'var(--ag-glass-border)',
                    accentColor: 'var(--ag-primary)',
                  }}
                />
                <User className="w-3.5 h-3.5" style={{ color: 'var(--ag-text-tertiary)' }} />
                <span className="text-sm">
                  {u.email} ({u.username || '-'})
                </span>
              </label>
            ))}
          </div>
          <p className="text-xs font-mono" style={{ color: 'var(--ag-text-tertiary)' }}>
            {t('subscriptions.selected_count', { count: selectedUserIds.length })}
          </p>
        </div>

        <Select
          label={t('subscriptions.group')}
          required
          value={String(groupId)}
          onChange={(e) => setGroupId(Number(e.target.value))}
          options={groupOptions}
        />

        <Input
          label={t('subscriptions.expire_time')}
          type="date"
          required
          value={expiresAt ? expiresAt.split('T')[0] : ''}
          onChange={(e) =>
            setExpiresAt(
              e.target.value ? `${e.target.value}T23:59:59Z` : '',
            )
          }
          icon={<CalendarDays className="w-4 h-4" />}
        />
      </div>
    </Modal>
  );
}

// ==================== 调整订阅弹窗 ====================

function AdjustModal({
  open,
  subscription,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  subscription: SubscriptionResp;
  onClose: () => void;
  onSubmit: (data: AdjustSubscriptionReq) => void;
  loading: boolean;
}) {
  const { t } = useTranslation();
  const [form, setForm] = useState<AdjustSubscriptionReq>({
    expires_at: subscription.expires_at,
    status: subscription.status as 'active' | 'suspended',
  });

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t('subscriptions.adjust_title', { name: subscription.group_name })}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={() => onSubmit(form)} loading={loading}>
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Input
          label={t('subscriptions.expire_time')}
          type="date"
          value={
            form.expires_at ? form.expires_at.split('T')[0] : ''
          }
          onChange={(e) =>
            setForm({
              ...form,
              expires_at: e.target.value
                ? `${e.target.value}T23:59:59Z`
                : undefined,
            })
          }
          icon={<CalendarDays className="w-4 h-4" />}
        />

        <Select
          label={t('common.status')}
          value={form.status ?? 'active'}
          onChange={(e) =>
            setForm({
              ...form,
              status: e.target.value as 'active' | 'suspended',
            })
          }
          options={[
            { value: 'active', label: t('subscriptions.status_active') },
            { value: 'suspended', label: t('subscriptions.status_suspended') },
          ]}
        />
      </div>
    </Modal>
  );
}
