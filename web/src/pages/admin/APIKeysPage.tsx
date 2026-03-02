import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { PageHeader } from '../../shared/components/PageHeader';
import { Button } from '../../shared/components/Button';
import { Input } from '../../shared/components/Input';
import { Table, type Column } from '../../shared/components/Table';
import { Modal, ConfirmModal } from '../../shared/components/Modal';
import { StatusBadge } from '../../shared/components/Badge';
import { useToast } from '../../shared/components/Toast';
import { apikeysApi } from '../../shared/api/apikeys';
import { groupsApi } from '../../shared/api/groups';
import type { APIKeyResp, CreateAPIKeyReq, UpdateAPIKeyReq, GroupResp } from '../../shared/types';

const PAGE_SIZE = 20;

export default function APIKeysPage() {
  const { toast } = useToast();
  const queryClient = useQueryClient();

  // 状态
  const [page, setPage] = useState(1);
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [editingKey, setEditingKey] = useState<APIKeyResp | null>(null);
  const [deletingKey, setDeletingKey] = useState<APIKeyResp | null>(null);
  const [createdKey, setCreatedKey] = useState<string | null>(null);

  // 查询密钥列表
  const { data, isLoading } = useQuery({
    queryKey: ['apikeys', page],
    queryFn: () => apikeysApi.list({ page, page_size: PAGE_SIZE }),
  });

  // 查询分组列表（用于创建密钥时选择分组）
  const { data: groupsData } = useQuery({
    queryKey: ['groups-all'],
    queryFn: () => groupsApi.list({ page: 1, page_size: 100 }),
  });

  // 创建密钥
  const createMutation = useMutation({
    mutationFn: (data: CreateAPIKeyReq) => apikeysApi.create(data),
    onSuccess: (resp) => {
      toast('success', 'API 密钥创建成功');
      setShowCreateModal(false);
      // 显示完整密钥
      if (resp.key) {
        setCreatedKey(resp.key);
      }
      queryClient.invalidateQueries({ queryKey: ['apikeys'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 更新密钥
  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: UpdateAPIKeyReq }) =>
      apikeysApi.update(id, data),
    onSuccess: () => {
      toast('success', '密钥更新成功');
      setEditingKey(null);
      queryClient.invalidateQueries({ queryKey: ['apikeys'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 删除密钥
  const deleteMutation = useMutation({
    mutationFn: (id: number) => apikeysApi.delete(id),
    onSuccess: () => {
      toast('success', '密钥已删除');
      setDeletingKey(null);
      queryClient.invalidateQueries({ queryKey: ['apikeys'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 格式化过期时间
  const formatExpiry = (date?: string) => {
    if (!date) return '永不过期';
    const d = new Date(date);
    return d.toLocaleDateString('zh-CN');
  };

  // 表格列定义
  const columns: Column<APIKeyResp>[] = [
    { key: 'id', title: 'ID', width: '60px' },
    { key: 'name', title: '名称' },
    {
      key: 'key_prefix',
      title: '密钥前缀',
      render: (row) => (
        <code className="text-xs bg-gray-100 px-1.5 py-0.5 rounded">
          {row.key_prefix}...
        </code>
      ),
    },
    {
      key: 'group_id',
      title: '分组',
      render: (row) => {
        const group = groupsData?.list?.find(
          (g: GroupResp) => g.id === row.group_id,
        );
        return group ? group.name : `#${row.group_id}`;
      },
    },
    {
      key: 'quota',
      title: '配额/已用',
      render: (row) => (
        <span>
          ${row.used_quota.toFixed(2)} / {row.quota_usd > 0 ? `$${row.quota_usd.toFixed(2)}` : '无限'}
        </span>
      ),
    },
    {
      key: 'expires_at',
      title: '过期时间',
      render: (row) => formatExpiry(row.expires_at),
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
        <div className="flex gap-2">
          <Button size="sm" variant="ghost" onClick={() => setEditingKey(row)}>
            编辑
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="text-red-600"
            onClick={() => setDeletingKey(row)}
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
        title="API 密钥"
        actions={
          <Button onClick={() => setShowCreateModal(true)}>创建密钥</Button>
        }
      />

      {/* 表格 */}
      <Table<APIKeyResp>
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
      <CreateKeyModal
        open={showCreateModal}
        groups={groupsData?.list ?? []}
        onClose={() => setShowCreateModal(false)}
        onSubmit={(data) => createMutation.mutate(data)}
        loading={createMutation.isPending}
      />

      {/* 密钥展示弹窗 */}
      <KeyRevealModal
        open={!!createdKey}
        keyValue={createdKey ?? ''}
        onClose={() => setCreatedKey(null)}
      />

      {/* 编辑弹窗 */}
      {editingKey && (
        <EditKeyModal
          open
          apiKey={editingKey}
          onClose={() => setEditingKey(null)}
          onSubmit={(data) =>
            updateMutation.mutate({ id: editingKey.id, data })
          }
          loading={updateMutation.isPending}
        />
      )}

      {/* 删除确认 */}
      <ConfirmModal
        open={!!deletingKey}
        onClose={() => setDeletingKey(null)}
        onConfirm={() => deletingKey && deleteMutation.mutate(deletingKey.id)}
        title="删除密钥"
        message={`确定要删除密钥「${deletingKey?.name}」吗？此操作不可恢复。`}
        loading={deleteMutation.isPending}
        danger
      />
    </div>
  );
}

// ==================== 创建密钥弹窗 ====================

function CreateKeyModal({
  open,
  groups,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  groups: GroupResp[];
  onClose: () => void;
  onSubmit: (data: CreateAPIKeyReq) => void;
  loading: boolean;
}) {
  const [form, setForm] = useState<CreateAPIKeyReq>({
    name: '',
    group_id: 0,
    quota_usd: 0,
    expires_at: '',
  });

  const handleSubmit = () => {
    if (!form.name || !form.group_id) return;
    onSubmit({
      ...form,
      quota_usd: form.quota_usd || undefined,
      expires_at: form.expires_at || undefined,
    });
  };

  const handleClose = () => {
    setForm({ name: '', group_id: 0, quota_usd: 0, expires_at: '' });
    onClose();
  };

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title="创建 API 密钥"
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
          label="名称"
          required
          value={form.name}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
          placeholder="给密钥起个名字"
        />

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
          label="配额 (USD)"
          type="number"
          step="0.01"
          min="0"
          value={String(form.quota_usd ?? 0)}
          onChange={(e) =>
            setForm({ ...form, quota_usd: Number(e.target.value) })
          }
          hint="设为 0 表示无限制"
        />

        <Input
          label="过期时间"
          type="date"
          value={form.expires_at ? form.expires_at.split('T')[0] : ''}
          onChange={(e) =>
            setForm({
              ...form,
              expires_at: e.target.value
                ? `${e.target.value}T23:59:59Z`
                : '',
            })
          }
          hint="留空表示永不过期"
        />
      </div>
    </Modal>
  );
}

// ==================== 密钥展示弹窗 ====================

function KeyRevealModal({
  open,
  keyValue,
  onClose,
}: {
  open: boolean;
  keyValue: string;
  onClose: () => void;
}) {
  const { toast } = useToast();

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(keyValue);
      toast('success', '已复制到剪贴板');
    } catch {
      toast('error', '复制失败，请手动复制');
    }
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="密钥已创建"
      footer={
        <Button onClick={onClose}>我已保存，关闭</Button>
      }
    >
      <div className="space-y-4">
        <div className="rounded-md bg-yellow-50 border border-yellow-200 p-4">
          <p className="text-sm text-yellow-800 font-medium">
            请立即复制并妥善保存此密钥，关闭后将无法再次查看完整密钥。
          </p>
        </div>
        <div className="flex items-center gap-2">
          <code className="flex-1 bg-gray-100 px-3 py-2 rounded text-sm font-mono break-all">
            {keyValue}
          </code>
          <Button size="sm" variant="secondary" onClick={handleCopy}>
            复制
          </Button>
        </div>
      </div>
    </Modal>
  );
}

// ==================== 编辑密钥弹窗 ====================

function EditKeyModal({
  open,
  apiKey,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  apiKey: APIKeyResp;
  onClose: () => void;
  onSubmit: (data: UpdateAPIKeyReq) => void;
  loading: boolean;
}) {
  const [form, setForm] = useState<UpdateAPIKeyReq>({
    name: apiKey.name,
    quota_usd: apiKey.quota_usd,
    expires_at: apiKey.expires_at,
    status: apiKey.status as 'active' | 'disabled',
  });

  return (
    <Modal
      open={open}
      onClose={onClose}
      title="编辑密钥"
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
          label="名称"
          value={form.name ?? ''}
          onChange={(e) => setForm({ ...form, name: e.target.value })}
        />

        <Input
          label="配额 (USD)"
          type="number"
          step="0.01"
          min="0"
          value={String(form.quota_usd ?? 0)}
          onChange={(e) =>
            setForm({ ...form, quota_usd: Number(e.target.value) })
          }
          hint="设为 0 表示无限制"
        />

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
          hint="留空表示永不过期"
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
