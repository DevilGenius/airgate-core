import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  Plus,
  Pencil,
  Trash2,
  Zap,
  Server,
  Hash,
  Gauge,
  Layers,
  Shield,
} from 'lucide-react';
import { PageHeader } from '../../shared/components/PageHeader';
import { Button } from '../../shared/components/Button';
import { Input, Textarea, Select } from '../../shared/components/Input';
import { Table, type Column } from '../../shared/components/Table';
import { Modal, ConfirmModal } from '../../shared/components/Modal';
import { StatusBadge } from '../../shared/components/Badge';
import { useToast } from '../../shared/components/Toast';
import { accountsApi } from '../../shared/api/accounts';
import { usePlatforms } from '../../shared/hooks/usePlatforms';
import type {
  AccountResp,
  CreateAccountReq,
  UpdateAccountReq,
  CredentialField,
} from '../../shared/types';

const PAGE_SIZE = 20;

export default function AccountsPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const queryClient = useQueryClient();
  const { platforms } = usePlatforms();

  const PLATFORM_OPTIONS = [
    { value: '', label: t('accounts.all_platforms') },
    ...platforms.map((p) => ({ value: p, label: p })),
  ];

  const STATUS_OPTIONS = [
    { value: '', label: t('users.all_status') },
    { value: 'active', label: t('status.active') },
    { value: 'error', label: t('status.error') },
    { value: 'disabled', label: t('status.disabled') },
  ];

  // 筛选状态
  const [page, setPage] = useState(1);
  const [platformFilter, setPlatformFilter] = useState('');
  const [statusFilter, setStatusFilter] = useState('');

  // 弹窗状态
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [editingAccount, setEditingAccount] = useState<AccountResp | null>(null);
  const [deletingAccount, setDeletingAccount] = useState<AccountResp | null>(null);
  const [testingId, setTestingId] = useState<number | null>(null);

  // 查询账号列表
  const { data, isLoading } = useQuery({
    queryKey: ['accounts', page, platformFilter, statusFilter],
    queryFn: () =>
      accountsApi.list({
        page,
        page_size: PAGE_SIZE,
        platform: platformFilter || undefined,
        status: statusFilter || undefined,
      }),
  });

  // 创建账号
  const createMutation = useMutation({
    mutationFn: (data: CreateAccountReq) => accountsApi.create(data),
    onSuccess: () => {
      toast('success', t('accounts.create_success'));
      setShowCreateModal(false);
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 更新账号
  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: UpdateAccountReq }) =>
      accountsApi.update(id, data),
    onSuccess: () => {
      toast('success', t('accounts.update_success'));
      setEditingAccount(null);
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 删除账号
  const deleteMutation = useMutation({
    mutationFn: (id: number) => accountsApi.delete(id),
    onSuccess: () => {
      toast('success', t('accounts.delete_success'));
      setDeletingAccount(null);
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 测试连通性
  const testMutation = useMutation({
    mutationFn: (id: number) => accountsApi.test(id),
    onSuccess: (result) => {
      if (result.success) {
        toast('success', t('accounts.test_success'));
      } else {
        toast('error', t('accounts.test_failed', { error: result.error_msg ?? '' }));
      }
      setTestingId(null);
    },
    onError: (err: Error) => {
      toast('error', err.message);
      setTestingId(null);
    },
  });

  // 表格列定义
  const columns: Column<AccountResp>[] = [
    {
      key: 'id',
      title: t('common.id'),
      width: '60px',
      render: (row) => (
        <span style={{ fontFamily: 'var(--ag-font-mono)' }}>
          {row.id}
        </span>
      ),
    },
    {
      key: 'name',
      title: t('common.name'),
      render: (row) => (
        <span style={{ color: 'var(--ag-text)' }} className="font-medium">
          {row.name}
        </span>
      ),
    },
    {
      key: 'platform',
      title: t('accounts.platform'),
      render: (row) => (
        <span className="inline-flex items-center gap-1.5">
          <Server className="w-3.5 h-3.5" style={{ color: 'var(--ag-text-tertiary)' }} />
          {row.platform}
        </span>
      ),
    },
    {
      key: 'status',
      title: t('common.status'),
      render: (row) => <StatusBadge status={row.status} />,
    },
    {
      key: 'priority',
      title: t('accounts.priority'),
      width: '80px',
      render: (row) => (
        <span style={{ fontFamily: 'var(--ag-font-mono)' }}>
          {row.priority}
        </span>
      ),
    },
    {
      key: 'max_concurrency',
      title: t('accounts.concurrency'),
      width: '80px',
      render: (row) => (
        <span style={{ fontFamily: 'var(--ag-font-mono)' }}>
          {row.max_concurrency}
        </span>
      ),
    },
    {
      key: 'rate_multiplier',
      title: t('accounts.rate_multiplier'),
      width: '80px',
      render: (row) => (
        <span style={{ fontFamily: 'var(--ag-font-mono)', color: 'var(--ag-primary)' }}>
          {row.rate_multiplier}x
        </span>
      ),
    },
    {
      key: 'proxy_id',
      title: t('accounts.proxy'),
      width: '80px',
      render: (row) =>
        row.proxy_id ? (
          <span className="inline-flex items-center gap-1" style={{ fontFamily: 'var(--ag-font-mono)' }}>
            <Shield className="w-3 h-3" style={{ color: 'var(--ag-text-tertiary)' }} />
            #{row.proxy_id}
          </span>
        ) : (
          <span style={{ color: 'var(--ag-text-tertiary)' }}>-</span>
        ),
    },
    {
      key: 'actions',
      title: t('common.actions'),
      render: (row) => (
        <div className="flex gap-1">
          <Button
            size="sm"
            variant="ghost"
            icon={<Pencil className="w-3.5 h-3.5" />}
            onClick={() => setEditingAccount(row)}
          >
            {t('common.edit')}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            icon={<Zap className="w-3.5 h-3.5" />}
            loading={testingId === row.id && testMutation.isPending}
            onClick={() => {
              setTestingId(row.id);
              testMutation.mutate(row.id);
            }}
          >
            {t('common.test')}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            icon={<Trash2 className="w-3.5 h-3.5" />}
            style={{ color: 'var(--ag-danger)' }}
            onClick={() => setDeletingAccount(row)}
          >
            {t('common.delete')}
          </Button>
        </div>
      ),
    },
  ];

  return (
    <div>
      <PageHeader
        title={t('accounts.title')}
        description={t('accounts.description')}
        actions={
          <Button icon={<Plus className="w-4 h-4" />} onClick={() => setShowCreateModal(true)}>
            {t('accounts.create')}
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
          label={t('accounts.platform')}
        />
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
      <Table<AccountResp>
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
      <CreateAccountModal
        open={showCreateModal}
        onClose={() => setShowCreateModal(false)}
        onSubmit={(data) => createMutation.mutate(data)}
        loading={createMutation.isPending}
        platforms={platforms}
      />

      {/* 编辑弹窗 */}
      {editingAccount && (
        <EditAccountModal
          open
          account={editingAccount}
          onClose={() => setEditingAccount(null)}
          onSubmit={(data) =>
            updateMutation.mutate({ id: editingAccount.id, data })
          }
          loading={updateMutation.isPending}
        />
      )}

      {/* 删除确认 */}
      <ConfirmModal
        open={!!deletingAccount}
        onClose={() => setDeletingAccount(null)}
        onConfirm={() => deletingAccount && deleteMutation.mutate(deletingAccount.id)}
        title={t('accounts.delete_title')}
        message={t('accounts.delete_confirm', { name: deletingAccount?.name })}
        loading={deleteMutation.isPending}
        danger
      />
    </div>
  );
}

// ==================== 创建账号弹窗 ====================

function CreateAccountModal({
  open,
  onClose,
  onSubmit,
  loading,
  platforms,
}: {
  open: boolean;
  onClose: () => void;
  onSubmit: (data: CreateAccountReq) => void;
  loading: boolean;
  platforms: string[];
}) {
  const { t } = useTranslation();
  const [platform, setPlatform] = useState('');
  const [form, setForm] = useState<Omit<CreateAccountReq, 'platform' | 'credentials'>>({
    name: '',
    priority: 0,
    max_concurrency: 5,
    rate_multiplier: 1,
  });
  const [credentials, setCredentials] = useState<Record<string, string>>({});

  // 根据平台获取凭证字段定义
  const { data: schema } = useQuery({
    queryKey: ['credentials-schema', platform],
    queryFn: () => accountsApi.credentialsSchema(platform),
    enabled: !!platform,
  });

  // 平台变化时重置凭证
  const handlePlatformChange = (newPlatform: string) => {
    setPlatform(newPlatform);
    setCredentials({});
  };

  const handleSubmit = () => {
    if (!platform || !form.name) return;
    onSubmit({
      ...form,
      platform,
      credentials,
    });
  };

  const handleClose = () => {
    setPlatform('');
    setForm({ name: '', priority: 0, max_concurrency: 5, rate_multiplier: 1 });
    setCredentials({});
    onClose();
  };

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title={t('accounts.create')}
      width="560px"
      footer={
        <>
          <Button variant="secondary" onClick={handleClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={handleSubmit} loading={loading} disabled={!platform}>
            {t('common.create')}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Select
          label={t('accounts.platform')}
          required
          value={platform}
          onChange={(e) => handlePlatformChange(e.target.value)}
          options={[
            { value: '', label: t('accounts.select_platform') },
            ...platforms.map((p) => ({ value: p, label: p })),
          ]}
        />

        <Input
          label={t('common.name')}
          required
          value={form.name}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
          icon={<Layers className="w-4 h-4" />}
        />

        {/* 动态凭证字段 */}
        {schema?.fields && schema.fields.length > 0 && (
          <div
            className="space-y-4 pt-4"
            style={{ borderTop: '1px solid var(--ag-border)' }}
          >
            <p
              className="text-xs font-medium uppercase tracking-wider"
              style={{ color: 'var(--ag-text-secondary)' }}
            >
              {t('accounts.credentials')}
            </p>
            {schema.fields.map((field) => (
              <CredentialFieldInput
                key={field.key}
                field={field}
                value={credentials[field.key] ?? ''}
                onChange={(val) =>
                  setCredentials({ ...credentials, [field.key]: val })
                }
              />
            ))}
          </div>
        )}

        <Input
          label={t('accounts.priority')}
          type="number"
          value={String(form.priority ?? 0)}
          onChange={(e) =>
            setForm({ ...form, priority: Number(e.target.value) })
          }
          icon={<Hash className="w-4 h-4" />}
        />
        <Input
          label={t('accounts.concurrency')}
          type="number"
          value={String(form.max_concurrency ?? 5)}
          onChange={(e) =>
            setForm({ ...form, max_concurrency: Number(e.target.value) })
          }
          icon={<Gauge className="w-4 h-4" />}
        />
        <Input
          label={t('accounts.rate_multiplier')}
          type="number"
          step="0.1"
          value={String(form.rate_multiplier ?? 1)}
          onChange={(e) =>
            setForm({ ...form, rate_multiplier: Number(e.target.value) })
          }
        />
      </div>
    </Modal>
  );
}

// ==================== 凭证字段渲染 ====================

function CredentialFieldInput({
  field,
  value,
  onChange,
}: {
  field: CredentialField;
  value: string;
  onChange: (val: string) => void;
}) {
  if (field.type === 'textarea') {
    return (
      <Textarea
        label={field.label}
        required={field.required}
        placeholder={field.placeholder}
        value={value}
        rows={3}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  }

  // text 和 password 都使用 Input
  return (
    <Input
      label={field.label}
      type={field.type === 'password' ? 'password' : 'text'}
      required={field.required}
      placeholder={field.placeholder}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    />
  );
}

// ==================== 编辑账号弹窗 ====================

function EditAccountModal({
  open,
  account,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  account: AccountResp;
  onClose: () => void;
  onSubmit: (data: UpdateAccountReq) => void;
  loading: boolean;
}) {
  const { t } = useTranslation();
  const [form, setForm] = useState<UpdateAccountReq>({
    name: account.name,
    status: account.status === 'error' ? 'active' : (account.status as 'active' | 'disabled'),
    priority: account.priority,
    max_concurrency: account.max_concurrency,
    rate_multiplier: account.rate_multiplier,
    proxy_id: account.proxy_id,
  });

  // 获取凭证字段定义，用于编辑凭证
  const { data: schema } = useQuery({
    queryKey: ['credentials-schema', account.platform],
    queryFn: () => accountsApi.credentialsSchema(account.platform),
  });

  const [credentials, setCredentials] = useState<Record<string, string>>(
    account.credentials,
  );

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={t('accounts.edit')}
      width="560px"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button
            onClick={() => onSubmit({ ...form, credentials })}
            loading={loading}
          >
            {t('common.save')}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Input label={t('accounts.platform')} value={account.platform} disabled />
        <Input
          label={t('common.name')}
          value={form.name ?? ''}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
          icon={<Layers className="w-4 h-4" />}
        />

        {/* 凭证编辑 */}
        {schema?.fields && schema.fields.length > 0 && (
          <div
            className="space-y-4 pt-4"
            style={{ borderTop: '1px solid var(--ag-border)' }}
          >
            <p
              className="text-xs font-medium uppercase tracking-wider"
              style={{ color: 'var(--ag-text-secondary)' }}
            >
              {t('accounts.credentials')}
            </p>
            {schema.fields.map((field) => (
              <CredentialFieldInput
                key={field.key}
                field={field}
                value={credentials[field.key] ?? ''}
                onChange={(val) =>
                  setCredentials({ ...credentials, [field.key]: val })
                }
              />
            ))}
          </div>
        )}

        <Select
          label={t('common.status')}
          value={form.status ?? 'active'}
          onChange={(e) =>
            setForm({
              ...form,
              status: e.target.value as 'active' | 'disabled',
            })
          }
          options={[
            { value: 'active', label: t('status.active') },
            { value: 'disabled', label: t('status.disabled') },
          ]}
        />
        <Input
          label={t('accounts.priority')}
          type="number"
          value={String(form.priority ?? 0)}
          onChange={(e) =>
            setForm({ ...form, priority: Number(e.target.value) })
          }
          icon={<Hash className="w-4 h-4" />}
        />
        <Input
          label={t('accounts.concurrency')}
          type="number"
          value={String(form.max_concurrency ?? 5)}
          onChange={(e) =>
            setForm({ ...form, max_concurrency: Number(e.target.value) })
          }
          icon={<Gauge className="w-4 h-4" />}
        />
        <Input
          label={t('accounts.rate_multiplier')}
          type="number"
          step="0.1"
          value={String(form.rate_multiplier ?? 1)}
          onChange={(e) =>
            setForm({ ...form, rate_multiplier: Number(e.target.value) })
          }
        />
        <Input
          label={t('accounts.proxy_id')}
          type="number"
          value={String(form.proxy_id ?? '')}
          onChange={(e) =>
            setForm({
              ...form,
              proxy_id: e.target.value ? Number(e.target.value) : undefined,
            })
          }
          hint={t('accounts.proxy_hint')}
          icon={<Shield className="w-4 h-4" />}
        />
      </div>
    </Modal>
  );
}
