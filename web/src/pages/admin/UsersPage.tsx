import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { PageHeader } from '../../shared/components/PageHeader';
import { Button } from '../../shared/components/Button';
import { Input } from '../../shared/components/Input';
import { Table, type Column } from '../../shared/components/Table';
import { Modal } from '../../shared/components/Modal';
import { Badge, StatusBadge } from '../../shared/components/Badge';
import { useToast } from '../../shared/components/Toast';
import { usersApi } from '../../shared/api/users';
import type { UserResp, CreateUserReq, UpdateUserReq, AdjustBalanceReq } from '../../shared/types';

// 默认分页大小
const PAGE_SIZE = 20;

export default function UsersPage() {
  const { toast } = useToast();
  const queryClient = useQueryClient();

  // 搜索和筛选状态
  const [page, setPage] = useState(1);
  const [keyword, setKeyword] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [roleFilter, setRoleFilter] = useState('');

  // 弹窗状态
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [editingUser, setEditingUser] = useState<UserResp | null>(null);
  const [balanceUser, setBalanceUser] = useState<UserResp | null>(null);

  // 查询用户列表
  const { data, isLoading } = useQuery({
    queryKey: ['users', page, keyword, statusFilter, roleFilter],
    queryFn: () =>
      usersApi.list({
        page,
        page_size: PAGE_SIZE,
        keyword: keyword || undefined,
        status: statusFilter || undefined,
        role: roleFilter || undefined,
      }),
  });

  // 创建用户
  const createMutation = useMutation({
    mutationFn: (data: CreateUserReq) => usersApi.create(data),
    onSuccess: () => {
      toast('success', '用户创建成功');
      setShowCreateModal(false);
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 更新用户
  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: UpdateUserReq }) =>
      usersApi.update(id, data),
    onSuccess: () => {
      toast('success', '用户更新成功');
      setEditingUser(null);
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 调整余额
  const balanceMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: AdjustBalanceReq }) =>
      usersApi.adjustBalance(id, data),
    onSuccess: () => {
      toast('success', '余额调整成功');
      setBalanceUser(null);
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 表格列定义
  const columns: Column<UserResp>[] = [
    { key: 'id', title: 'ID', width: '60px' },
    { key: 'email', title: '邮箱' },
    { key: 'username', title: '用户名' },
    {
      key: 'role',
      title: '角色',
      render: (row) => (
        <Badge variant={row.role === 'admin' ? 'info' : 'default'}>
          {row.role === 'admin' ? '管理员' : '用户'}
        </Badge>
      ),
    },
    {
      key: 'balance',
      title: '余额',
      render: (row) => `$${row.balance.toFixed(2)}`,
    },
    { key: 'max_concurrency', title: '并发数' },
    {
      key: 'status',
      title: '状态',
      render: (row) => <StatusBadge status={row.status} />,
    },
    {
      key: 'actions',
      title: '操作',
      render: (row) => (
        <div className="flex gap-2">
          <Button size="sm" variant="ghost" onClick={() => setEditingUser(row)}>
            编辑
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setBalanceUser(row)}>
            余额
          </Button>
        </div>
      ),
    },
  ];

  return (
    <div>
      <PageHeader
        title="用户管理"
        actions={
          <Button onClick={() => setShowCreateModal(true)}>创建用户</Button>
        }
      />

      {/* 搜索和筛选 */}
      <div className="flex items-center gap-4 mb-4">
        <div className="w-64">
          <Input
            placeholder="搜索邮箱或用户名..."
            value={keyword}
            onChange={(e) => {
              setKeyword(e.target.value);
              setPage(1);
            }}
          />
        </div>
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
          <option value="disabled">已禁用</option>
        </select>
        <select
          className="rounded-md border border-gray-300 px-3 py-2 text-sm"
          value={roleFilter}
          onChange={(e) => {
            setRoleFilter(e.target.value);
            setPage(1);
          }}
        >
          <option value="">全部角色</option>
          <option value="admin">管理员</option>
          <option value="user">用户</option>
        </select>
      </div>

      {/* 表格 */}
      <Table<UserResp>
        columns={columns}
        data={data?.list ?? []}
        loading={isLoading}
        rowKey={(row) => row.id}
        page={page}
        pageSize={PAGE_SIZE}
        total={data?.total ?? 0}
        onPageChange={setPage}
      />

      {/* 创建用户弹窗 */}
      <CreateUserModal
        open={showCreateModal}
        onClose={() => setShowCreateModal(false)}
        onSubmit={(data) => createMutation.mutate(data)}
        loading={createMutation.isPending}
      />

      {/* 编辑用户弹窗 */}
      {editingUser && (
        <EditUserModal
          open
          user={editingUser}
          onClose={() => setEditingUser(null)}
          onSubmit={(data) =>
            updateMutation.mutate({ id: editingUser.id, data })
          }
          loading={updateMutation.isPending}
        />
      )}

      {/* 余额调整弹窗 */}
      {balanceUser && (
        <BalanceModal
          open
          user={balanceUser}
          onClose={() => setBalanceUser(null)}
          onSubmit={(data) =>
            balanceMutation.mutate({ id: balanceUser.id, data })
          }
          loading={balanceMutation.isPending}
        />
      )}
    </div>
  );
}

// ==================== 创建用户弹窗 ====================

function CreateUserModal({
  open,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  onClose: () => void;
  onSubmit: (data: CreateUserReq) => void;
  loading: boolean;
}) {
  const [form, setForm] = useState<CreateUserReq>({
    email: '',
    password: '',
    username: '',
    role: 'user',
    max_concurrency: 5,
  });

  const handleSubmit = () => {
    if (!form.email || !form.password) return;
    onSubmit(form);
  };

  // 重置表单
  const handleClose = () => {
    setForm({ email: '', password: '', username: '', role: 'user', max_concurrency: 5 });
    onClose();
  };

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title="创建用户"
      footer={
        <>
          <Button variant="secondary" onClick={handleClose}>
            取消
          </Button>
          <Button onClick={handleSubmit} loading={loading}>
            创建
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Input
          label="邮箱"
          type="email"
          required
          value={form.email}
          onChange={(e) => setForm({ ...form, email: e.target.value })}
        />
        <Input
          label="密码"
          type="password"
          required
          value={form.password}
          onChange={(e) => setForm({ ...form, password: e.target.value })}
        />
        <Input
          label="用户名"
          value={form.username}
          onChange={(e) => setForm({ ...form, username: e.target.value })}
        />
        <div className="space-y-1">
          <label className="block text-sm font-medium text-gray-700">角色</label>
          <select
            className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
            value={form.role}
            onChange={(e) =>
              setForm({ ...form, role: e.target.value as 'admin' | 'user' })
            }
          >
            <option value="user">用户</option>
            <option value="admin">管理员</option>
          </select>
        </div>
        <Input
          label="最大并发数"
          type="number"
          value={String(form.max_concurrency ?? 5)}
          onChange={(e) =>
            setForm({ ...form, max_concurrency: Number(e.target.value) })
          }
        />
      </div>
    </Modal>
  );
}

// ==================== 编辑用户弹窗 ====================

function EditUserModal({
  open,
  user,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  user: UserResp;
  onClose: () => void;
  onSubmit: (data: UpdateUserReq) => void;
  loading: boolean;
}) {
  const [form, setForm] = useState<UpdateUserReq>({
    username: user.username,
    role: user.role,
    max_concurrency: user.max_concurrency,
    status: user.status as 'active' | 'disabled',
  });

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="编辑用户"
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
        <Input label="邮箱" value={user.email} disabled />
        <Input
          label="用户名"
          value={form.username ?? ''}
          onChange={(e) => setForm({ ...form, username: e.target.value })}
        />
        <div className="space-y-1">
          <label className="block text-sm font-medium text-gray-700">角色</label>
          <select
            className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
            value={form.role}
            onChange={(e) =>
              setForm({ ...form, role: e.target.value as 'admin' | 'user' })
            }
          >
            <option value="user">用户</option>
            <option value="admin">管理员</option>
          </select>
        </div>
        <Input
          label="最大并发数"
          type="number"
          value={String(form.max_concurrency ?? 5)}
          onChange={(e) =>
            setForm({ ...form, max_concurrency: Number(e.target.value) })
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
                status: e.target.value as 'active' | 'disabled',
              })
            }
          >
            <option value="active">活跃</option>
            <option value="disabled">已禁用</option>
          </select>
        </div>
      </div>
    </Modal>
  );
}

// ==================== 余额调整弹窗 ====================

function BalanceModal({
  open,
  user,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  user: UserResp;
  onClose: () => void;
  onSubmit: (data: AdjustBalanceReq) => void;
  loading: boolean;
}) {
  const [form, setForm] = useState<AdjustBalanceReq>({
    action: 'add',
    amount: 0,
  });

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`余额调整 - ${user.email}`}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button onClick={() => onSubmit(form)} loading={loading}>
            确认
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <p className="text-sm text-gray-600">
          当前余额：<span className="font-semibold">${user.balance.toFixed(2)}</span>
        </p>
        <div className="space-y-1">
          <label className="block text-sm font-medium text-gray-700">操作类型</label>
          <select
            className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
            value={form.action}
            onChange={(e) =>
              setForm({
                ...form,
                action: e.target.value as 'set' | 'add' | 'subtract',
              })
            }
          >
            <option value="add">增加</option>
            <option value="subtract">减少</option>
            <option value="set">设为</option>
          </select>
        </div>
        <Input
          label="金额"
          type="number"
          required
          min="0"
          step="0.01"
          value={String(form.amount)}
          onChange={(e) =>
            setForm({ ...form, amount: Number(e.target.value) })
          }
        />
      </div>
    </Modal>
  );
}
