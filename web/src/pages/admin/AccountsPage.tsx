import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { PageHeader } from '../../shared/components/PageHeader';
import { Button } from '../../shared/components/Button';
import { Input, Textarea } from '../../shared/components/Input';
import { Table, type Column } from '../../shared/components/Table';
import { Modal, ConfirmModal } from '../../shared/components/Modal';
import { StatusBadge } from '../../shared/components/Badge';
import { useToast } from '../../shared/components/Toast';
import { accountsApi } from '../../shared/api/accounts';
import type {
  AccountResp,
  CreateAccountReq,
  UpdateAccountReq,
  CredentialField,
} from '../../shared/types';

const PAGE_SIZE = 20;

// 已知平台列表（用于筛选和创建）
const PLATFORMS = ['openai', 'claude', 'gemini', 'sora'];

export default function AccountsPage() {
  const { toast } = useToast();
  const queryClient = useQueryClient();

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
      toast('success', '账号创建成功');
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
      toast('success', '账号更新成功');
      setEditingAccount(null);
      queryClient.invalidateQueries({ queryKey: ['accounts'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 删除账号
  const deleteMutation = useMutation({
    mutationFn: (id: number) => accountsApi.delete(id),
    onSuccess: () => {
      toast('success', '账号已删除');
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
        toast('success', '连通性测试通过');
      } else {
        toast('error', `连通性测试失败：${result.error_msg ?? '未知错误'}`);
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
    { key: 'id', title: 'ID', width: '60px' },
    { key: 'name', title: '名称' },
    { key: 'platform', title: '平台' },
    {
      key: 'status',
      title: '状态',
      render: (row) => <StatusBadge status={row.status} />,
    },
    { key: 'priority', title: '优先级', width: '80px' },
    { key: 'max_concurrency', title: '并发数', width: '80px' },
    {
      key: 'rate_multiplier',
      title: '倍率',
      width: '80px',
      render: (row) => `${row.rate_multiplier}x`,
    },
    {
      key: 'proxy_id',
      title: '代理',
      width: '80px',
      render: (row) => (row.proxy_id ? `#${row.proxy_id}` : '-'),
    },
    {
      key: 'actions',
      title: '操作',
      render: (row) => (
        <div className="flex gap-2">
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setEditingAccount(row)}
          >
            编辑
          </Button>
          <Button
            size="sm"
            variant="ghost"
            loading={testingId === row.id && testMutation.isPending}
            onClick={() => {
              setTestingId(row.id);
              testMutation.mutate(row.id);
            }}
          >
            测试
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="text-red-600"
            onClick={() => setDeletingAccount(row)}
          >
            删除
          </Button>
        </div>
      ),
    },
  ];

  return (
    <div>
      <PageHeader
        title="账号管理"
        actions={
          <Button onClick={() => setShowCreateModal(true)}>添加账号</Button>
        }
      />

      {/* 筛选 */}
      <div className="flex items-center gap-4 mb-4">
        <select
          className="rounded-md border border-gray-300 px-3 py-2 text-sm"
          value={platformFilter}
          onChange={(e) => {
            setPlatformFilter(e.target.value);
            setPage(1);
          }}
        >
          <option value="">全部平台</option>
          {PLATFORMS.map((p) => (
            <option key={p} value={p}>
              {p}
            </option>
          ))}
        </select>
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
          <option value="error">错误</option>
          <option value="disabled">已禁用</option>
        </select>
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
        title="删除账号"
        message={`确定要删除账号「${deletingAccount?.name}」吗？此操作不可恢复。`}
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
}: {
  open: boolean;
  onClose: () => void;
  onSubmit: (data: CreateAccountReq) => void;
  loading: boolean;
}) {
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
      title="添加账号"
      width="560px"
      footer={
        <>
          <Button variant="secondary" onClick={handleClose}>
            取消
          </Button>
          <Button onClick={handleSubmit} loading={loading} disabled={!platform}>
            创建
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <div className="space-y-1">
          <label className="block text-sm font-medium text-gray-700">
            平台 <span className="text-red-500">*</span>
          </label>
          <select
            className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
            value={platform}
            onChange={(e) => handlePlatformChange(e.target.value)}
          >
            <option value="">请选择平台</option>
            {PLATFORMS.map((p) => (
              <option key={p} value={p}>
                {p}
              </option>
            ))}
          </select>
        </div>

        <Input
          label="名称"
          required
          value={form.name}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
        />

        {/* 动态凭证字段 */}
        {schema?.fields && schema.fields.length > 0 && (
          <div className="space-y-4 border-t border-gray-200 pt-4">
            <p className="text-sm font-medium text-gray-700">凭证信息</p>
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
          label="优先级"
          type="number"
          value={String(form.priority ?? 0)}
          onChange={(e) =>
            setForm({ ...form, priority: Number(e.target.value) })
          }
        />
        <Input
          label="最大并发数"
          type="number"
          value={String(form.max_concurrency ?? 5)}
          onChange={(e) =>
            setForm({ ...form, max_concurrency: Number(e.target.value) })
          }
        />
        <Input
          label="费率倍率"
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
      title="编辑账号"
      width="560px"
      footer={
        <>
          <Button variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button
            onClick={() => onSubmit({ ...form, credentials })}
            loading={loading}
          >
            保存
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Input label="平台" value={account.platform} disabled />
        <Input
          label="名称"
          value={form.name ?? ''}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
        />

        {/* 凭证编辑 */}
        {schema?.fields && schema.fields.length > 0 && (
          <div className="space-y-4 border-t border-gray-200 pt-4">
            <p className="text-sm font-medium text-gray-700">凭证信息</p>
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
        <Input
          label="优先级"
          type="number"
          value={String(form.priority ?? 0)}
          onChange={(e) =>
            setForm({ ...form, priority: Number(e.target.value) })
          }
        />
        <Input
          label="最大并发数"
          type="number"
          value={String(form.max_concurrency ?? 5)}
          onChange={(e) =>
            setForm({ ...form, max_concurrency: Number(e.target.value) })
          }
        />
        <Input
          label="费率倍率"
          type="number"
          step="0.1"
          value={String(form.rate_multiplier ?? 1)}
          onChange={(e) =>
            setForm({ ...form, rate_multiplier: Number(e.target.value) })
          }
        />
        <Input
          label="代理 ID"
          type="number"
          value={String(form.proxy_id ?? '')}
          onChange={(e) =>
            setForm({
              ...form,
              proxy_id: e.target.value ? Number(e.target.value) : undefined,
            })
          }
          hint="留空表示不使用代理"
        />
      </div>
    </Modal>
  );
}
