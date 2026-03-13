import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { PageHeader } from '../../shared/components/PageHeader';
import { Button } from '../../shared/components/Button';
import { Input } from '../../shared/components/Input';
import { Select } from '../../shared/components/Input';
import { Table, type Column } from '../../shared/components/Table';
import { Modal } from '../../shared/components/Modal';
import { Badge, StatusBadge } from '../../shared/components/Badge';
import { useToast } from '../../shared/components/Toast';
import { usersApi } from '../../shared/api/users';
import type { UserResp, CreateUserReq, UpdateUserReq, AdjustBalanceReq } from '../../shared/types';
import { Plus, Search, Pencil, Wallet } from 'lucide-react';

const PAGE_SIZE = 20;

export default function UsersPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const queryClient = useQueryClient();

  const [page, setPage] = useState(1);
  const [keyword, setKeyword] = useState('');
  const [statusFilter, setStatusFilter] = useState('');
  const [roleFilter, setRoleFilter] = useState('');

  const [showCreateModal, setShowCreateModal] = useState(false);
  const [editingUser, setEditingUser] = useState<UserResp | null>(null);
  const [balanceUser, setBalanceUser] = useState<UserResp | null>(null);

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

  const createMutation = useMutation({
    mutationFn: (data: CreateUserReq) => usersApi.create(data),
    onSuccess: () => {
      toast('success', t('users.create_success'));
      setShowCreateModal(false);
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: UpdateUserReq }) => usersApi.update(id, data),
    onSuccess: () => {
      toast('success', t('users.update_success'));
      setEditingUser(null);
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  const balanceMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: AdjustBalanceReq }) => usersApi.adjustBalance(id, data),
    onSuccess: () => {
      toast('success', t('users.balance_success'));
      setBalanceUser(null);
      queryClient.invalidateQueries({ queryKey: ['users'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  const columns: Column<UserResp>[] = [
    {
      key: 'id',
      title: t('common.id'),
      width: '60px',
      render: (row) => (
        <span className="text-text-tertiary font-mono">
          {row.id}
        </span>
      ),
    },
    {
      key: 'email',
      title: t('users.email'),
      render: (row) => <span className="text-text">{row.email}</span>,
    },
    { key: 'username', title: t('users.username') },
    {
      key: 'role',
      title: t('users.role'),
      render: (row) => (
        <Badge variant={row.role === 'admin' ? 'info' : 'default'}>
          {row.role === 'admin' ? t('users.role_admin') : t('users.role_user')}
        </Badge>
      ),
    },
    {
      key: 'balance',
      title: t('users.balance'),
      render: (row) => (
        <span className="font-mono">
          ${row.balance.toFixed(2)}
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
        <div className="flex gap-1">
          <Button size="sm" variant="ghost" icon={<Pencil className="w-3.5 h-3.5" />} onClick={() => setEditingUser(row)}>
            {t('common.edit')}
          </Button>
          <Button size="sm" variant="ghost" icon={<Wallet className="w-3.5 h-3.5" />} onClick={() => setBalanceUser(row)}>
            {t('users.balance')}
          </Button>
        </div>
      ),
    },
  ];

  return (
    <div>
      <PageHeader
        title={t('users.title')}
        actions={
          <Button icon={<Plus className="w-4 h-4" />} onClick={() => setShowCreateModal(true)}>
            {t('users.create')}
          </Button>
        }
      />

      {/* 筛选栏 */}
      <div className="flex items-end gap-3 mb-5">
        <div className="w-64">
          <Input
            placeholder={t('users.search_placeholder')}
            value={keyword}
            onChange={(e) => { setKeyword(e.target.value); setPage(1); }}
            icon={<Search className="w-4 h-4" />}
          />
        </div>
        <div className="w-36">
          <Select
            value={statusFilter}
            onChange={(e) => { setStatusFilter(e.target.value); setPage(1); }}
            options={[
              { value: '', label: t('users.all_status') },
              { value: 'active', label: t('status.active') },
              { value: 'disabled', label: t('status.disabled') },
            ]}
          />
        </div>
        <div className="w-36">
          <Select
            value={roleFilter}
            onChange={(e) => { setRoleFilter(e.target.value); setPage(1); }}
            options={[
              { value: '', label: t('users.all_roles') },
              { value: 'admin', label: t('users.role_admin') },
              { value: 'user', label: t('users.role_user') },
            ]}
          />
        </div>
      </div>

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

      <CreateUserModal
        open={showCreateModal}
        onClose={() => setShowCreateModal(false)}
        onSubmit={(data) => createMutation.mutate(data)}
        loading={createMutation.isPending}
      />

      {editingUser && (
        <EditUserModal
          open
          user={editingUser}
          onClose={() => setEditingUser(null)}
          onSubmit={(data) => updateMutation.mutate({ id: editingUser.id, data })}
          loading={updateMutation.isPending}
        />
      )}

      {balanceUser && (
        <BalanceModal
          open
          user={balanceUser}
          onClose={() => setBalanceUser(null)}
          onSubmit={(data) => balanceMutation.mutate({ id: balanceUser.id, data })}
          loading={balanceMutation.isPending}
        />
      )}
    </div>
  );
}

/* ==================== 创建用户弹窗 ==================== */

function CreateUserModal({
  open, onClose, onSubmit, loading,
}: {
  open: boolean; onClose: () => void; onSubmit: (data: CreateUserReq) => void; loading: boolean;
}) {
  const { t } = useTranslation();
  const [form, setForm] = useState<CreateUserReq>({
    email: '', password: '', username: '', role: 'user', max_concurrency: 5,
  });

  const handleSubmit = () => {
    if (!form.email || !form.password) return;
    onSubmit(form);
  };

  const handleClose = () => {
    setForm({ email: '', password: '', username: '', role: 'user', max_concurrency: 5 });
    onClose();
  };

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title={t('users.create')}
      footer={
        <>
          <Button variant="secondary" onClick={handleClose}>{t('common.cancel')}</Button>
          <Button onClick={handleSubmit} loading={loading}>{t('common.create')}</Button>
        </>
      }
    >
      <div className="space-y-4">
        <Input label={t('users.email')} type="email" required value={form.email} onChange={(e) => setForm({ ...form, email: e.target.value })} />
        <Input label={t('users.password')} type="password" required value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
        <Input label={t('users.username')} value={form.username} onChange={(e) => setForm({ ...form, username: e.target.value })} />
        <Select
          label={t('users.role')}
          value={form.role}
          onChange={(e) => setForm({ ...form, role: e.target.value as 'admin' | 'user' })}
          options={[{ value: 'user', label: t('users.role_user') }, { value: 'admin', label: t('users.role_admin') }]}
        />
        <Input
          label={t('users.max_concurrency')}
          type="number"
          value={String(form.max_concurrency ?? 5)}
          onChange={(e) => setForm({ ...form, max_concurrency: Number(e.target.value) })}
        />
      </div>
    </Modal>
  );
}

/* ==================== 编辑用户弹窗 ==================== */

function EditUserModal({
  open, user, onClose, onSubmit, loading,
}: {
  open: boolean; user: UserResp; onClose: () => void; onSubmit: (data: UpdateUserReq) => void; loading: boolean;
}) {
  const { t } = useTranslation();
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
      title={t('users.edit')}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>{t('common.cancel')}</Button>
          <Button onClick={() => onSubmit(form)} loading={loading}>{t('common.save')}</Button>
        </>
      }
    >
      <div className="space-y-4">
        <Input label={t('users.email')} value={user.email} disabled />
        <Input label={t('users.username')} value={form.username ?? ''} onChange={(e) => setForm({ ...form, username: e.target.value })} />
        <Select
          label={t('users.role')}
          value={form.role ?? 'user'}
          onChange={(e) => setForm({ ...form, role: e.target.value as 'admin' | 'user' })}
          options={[{ value: 'user', label: t('users.role_user') }, { value: 'admin', label: t('users.role_admin') }]}
        />
        <Input
          label={t('users.max_concurrency')}
          type="number"
          value={String(form.max_concurrency ?? 5)}
          onChange={(e) => setForm({ ...form, max_concurrency: Number(e.target.value) })}
        />
        <Select
          label={t('common.status')}
          value={form.status ?? 'active'}
          onChange={(e) => setForm({ ...form, status: e.target.value as 'active' | 'disabled' })}
          options={[{ value: 'active', label: t('status.active') }, { value: 'disabled', label: t('status.disabled') }]}
        />
      </div>
    </Modal>
  );
}

/* ==================== 余额调整弹窗 ==================== */

function BalanceModal({
  open, user, onClose, onSubmit, loading,
}: {
  open: boolean; user: UserResp; onClose: () => void; onSubmit: (data: AdjustBalanceReq) => void; loading: boolean;
}) {
  const { t } = useTranslation();
  const [form, setForm] = useState<AdjustBalanceReq>({ action: 'add', amount: 0 });

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t('users.adjust_balance', { email: user.email })}
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>{t('common.cancel')}</Button>
          <Button onClick={() => onSubmit(form)} loading={loading}>{t('common.confirm')}</Button>
        </>
      }
    >
      <div className="space-y-4">
        <div className="rounded-md bg-surface border border-glass-border px-4 py-3">
          <p className="text-xs text-text-tertiary uppercase tracking-wider">{t('users.current_balance')}</p>
          <p className="text-lg font-bold mt-1 font-mono">
            ${user.balance.toFixed(2)}
          </p>
        </div>
        <Select
          label={t('users.action_type')}
          value={form.action}
          onChange={(e) => setForm({ ...form, action: e.target.value as 'set' | 'add' | 'subtract' })}
          options={[
            { value: 'add', label: t('users.action_add') },
            { value: 'subtract', label: t('users.action_subtract') },
            { value: 'set', label: t('users.action_set') },
          ]}
        />
        <Input
          label={t('users.amount')}
          type="number"
          required
          min="0"
          step="0.01"
          value={String(form.amount)}
          onChange={(e) => setForm({ ...form, amount: Number(e.target.value) })}
        />
      </div>
    </Modal>
  );
}
