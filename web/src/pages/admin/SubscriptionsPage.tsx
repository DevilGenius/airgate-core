import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { PageHeader } from '../../shared/components/PageHeader';
import { Button } from '../../shared/components/Button';
import { Input } from '../../shared/components/Input';
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
  const { toast } = useToast();
  const queryClient = useQueryClient();

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
      toast('success', '订阅分配成功');
      setShowAssignModal(false);
      queryClient.invalidateQueries({ queryKey: ['subscriptions'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 批量分配
  const bulkMutation = useMutation({
    mutationFn: (data: BulkAssignReq) => subscriptionsApi.bulkAssign(data),
    onSuccess: () => {
      toast('success', '批量分配成功');
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
      toast('success', '订阅调整成功');
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
    return user ? user.email : `用户 #${userId}`;
  };

  // 表格列定义
  const columns: Column<SubscriptionResp>[] = [
    { key: 'id', title: 'ID', width: '60px' },
    {
      key: 'user_id',
      title: '用户',
      render: (row) => getUserEmail(row.user_id),
    },
    { key: 'group_name', title: '分组' },
    {
      key: 'effective_at',
      title: '生效时间',
      render: (row) => formatDate(row.effective_at),
    },
    {
      key: 'expires_at',
      title: '过期时间',
      render: (row) => formatDate(row.expires_at),
    },
    {
      key: 'status',
      title: '状态',
      render: (row) => <StatusBadge status={row.status} />,
    },
    {
      key: 'actions',
      title: '操作',
      render: (row) => (
        <Button
          size="sm"
          variant="ghost"
          onClick={() => setAdjustingSub(row)}
        >
          调整
        </Button>
      ),
    },
  ];

  return (
    <div>
      <PageHeader
        title="订阅管理"
        actions={
          <div className="flex gap-2">
            <Button variant="secondary" onClick={() => setShowBulkModal(true)}>
              批量分配
            </Button>
            <Button onClick={() => setShowAssignModal(true)}>分配订阅</Button>
          </div>
        }
      />

      {/* 筛选 */}
      <div className="flex items-center gap-4 mb-4">
        <select
          className="rounded-md border border-gray-300 px-3 py-2 text-sm"
          value={statusFilter}
          onChange={(e) => {
            setStatusFilter(e.target.value);
            setPage(1);
          }}
        >
          <option value="">全部状态</option>
          <option value="active">活跃</option>
          <option value="expired">已过期</option>
          <option value="suspended">已暂停</option>
        </select>
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

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title="分配订阅"
      footer={
        <>
          <Button variant="secondary" onClick={handleClose}>
            取消
          </Button>
          <Button onClick={handleSubmit} loading={loading}>
            分配
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <div className="space-y-1">
          <label className="block text-sm font-medium text-gray-700">
            用户 <span className="text-red-500">*</span>
          </label>
          <select
            className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
            value={form.user_id}
            onChange={(e) =>
              setForm({ ...form, user_id: Number(e.target.value) })
            }
          >
            <option value={0}>请选择用户</option>
            {users.map((u) => (
              <option key={u.id} value={u.id}>
                {u.email} ({u.username || '未设置'})
              </option>
            ))}
          </select>
        </div>

        <div className="space-y-1">
          <label className="block text-sm font-medium text-gray-700">
            分组 <span className="text-red-500">*</span>
          </label>
          <select
            className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
            value={form.group_id}
            onChange={(e) =>
              setForm({ ...form, group_id: Number(e.target.value) })
            }
          >
            <option value={0}>请选择分组</option>
            {groups.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name} ({g.platform})
              </option>
            ))}
          </select>
        </div>

        <Input
          label="过期时间"
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

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title="批量分配订阅"
      width="560px"
      footer={
        <>
          <Button variant="secondary" onClick={handleClose}>
            取消
          </Button>
          <Button onClick={handleSubmit} loading={loading}>
            批量分配 ({selectedUserIds.length} 人)
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        {/* 用户多选 */}
        <div className="space-y-1">
          <label className="block text-sm font-medium text-gray-700">
            选择用户 <span className="text-red-500">*</span>
          </label>
          <div className="border border-gray-300 rounded-md max-h-48 overflow-y-auto p-2 space-y-1">
            {users.map((u) => (
              <label
                key={u.id}
                className="flex items-center gap-2 px-2 py-1 rounded hover:bg-gray-50 cursor-pointer"
              >
                <input
                  type="checkbox"
                  checked={selectedUserIds.includes(u.id)}
                  onChange={() => toggleUser(u.id)}
                  className="rounded border-gray-300 text-indigo-600"
                />
                <span className="text-sm text-gray-700">
                  {u.email} ({u.username || '未设置'})
                </span>
              </label>
            ))}
          </div>
          <p className="text-xs text-gray-500">
            已选 {selectedUserIds.length} 个用户
          </p>
        </div>

        <div className="space-y-1">
          <label className="block text-sm font-medium text-gray-700">
            分组 <span className="text-red-500">*</span>
          </label>
          <select
            className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
            value={groupId}
            onChange={(e) => setGroupId(Number(e.target.value))}
          >
            <option value={0}>请选择分组</option>
            {groups.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name} ({g.platform})
              </option>
            ))}
          </select>
        </div>

        <Input
          label="过期时间"
          type="date"
          required
          value={expiresAt ? expiresAt.split('T')[0] : ''}
          onChange={(e) =>
            setExpiresAt(
              e.target.value ? `${e.target.value}T23:59:59Z` : '',
            )
          }
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
  const [form, setForm] = useState<AdjustSubscriptionReq>({
    expires_at: subscription.expires_at,
    status: subscription.status as 'active' | 'suspended',
  });

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`调整订阅 - ${subscription.group_name}`}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button onClick={() => onSubmit(form)} loading={loading}>
            保存
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Input
          label="过期时间"
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
        />

        <div className="space-y-1">
          <label className="block text-sm font-medium text-gray-700">状态</label>
          <select
            className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
            value={form.status}
            onChange={(e) =>
              setForm({
                ...form,
                status: e.target.value as 'active' | 'suspended',
              })
            }
          >
            <option value="active">活跃</option>
            <option value="suspended">暂停</option>
          </select>
        </div>
      </div>
    </Modal>
  );
}
