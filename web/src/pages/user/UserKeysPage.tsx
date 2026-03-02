import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { apikeysApi } from '../../shared/api/apikeys';
import { groupsApi } from '../../shared/api/groups';
import { useToast } from '../../shared/components/Toast';
import { PageHeader } from '../../shared/components/PageHeader';
import { Table, type Column } from '../../shared/components/Table';
import { Button } from '../../shared/components/Button';
import { Input } from '../../shared/components/Input';
import { Modal, ConfirmModal } from '../../shared/components/Modal';
import { StatusBadge } from '../../shared/components/Badge';
import type { APIKeyResp, CreateAPIKeyReq, UpdateAPIKeyReq } from '../../shared/types';

interface KeyForm {
  name: string;
  group_id: string;
  quota_usd: string;
  expires_at: string;
}

const emptyForm: KeyForm = {
  name: '',
  group_id: '',
  quota_usd: '',
  expires_at: '',
};

export default function UserKeysPage() {
  const { toast } = useToast();
  const queryClient = useQueryClient();

  const [page, setPage] = useState(1);
  const [modalOpen, setModalOpen] = useState(false);
  const [editingKey, setEditingKey] = useState<APIKeyResp | null>(null);
  const [form, setForm] = useState<KeyForm>(emptyForm);
  const [deleteTarget, setDeleteTarget] = useState<APIKeyResp | null>(null);

  // 显示新创建密钥的弹窗
  const [createdKey, setCreatedKey] = useState<string | null>(null);

  // 密钥列表
  const { data, isLoading } = useQuery({
    queryKey: ['user-keys', page],
    queryFn: () => apikeysApi.list({ page, page_size: 20 }),
  });

  // 分组列表（用于选择）
  const { data: groupsData } = useQuery({
    queryKey: ['groups-for-keys'],
    queryFn: () => groupsApi.list({ page: 1, page_size: 100 }),
  });

  // 创建密钥
  const createMutation = useMutation({
    mutationFn: (data: CreateAPIKeyReq) => apikeysApi.create(data),
    onSuccess: (result) => {
      toast('success', '密钥创建成功');
      queryClient.invalidateQueries({ queryKey: ['user-keys'] });
      closeModal();
      // 显示完整密钥
      if (result.key) {
        setCreatedKey(result.key);
      }
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 更新密钥
  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: UpdateAPIKeyReq }) =>
      apikeysApi.update(id, data),
    onSuccess: () => {
      toast('success', '密钥已更新');
      queryClient.invalidateQueries({ queryKey: ['user-keys'] });
      closeModal();
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 删除密钥
  const deleteMutation = useMutation({
    mutationFn: (id: number) => apikeysApi.delete(id),
    onSuccess: () => {
      toast('success', '密钥已删除');
      queryClient.invalidateQueries({ queryKey: ['user-keys'] });
      setDeleteTarget(null);
    },
    onError: (err: Error) => toast('error', err.message),
  });

  function openCreate() {
    setEditingKey(null);
    setForm(emptyForm);
    setModalOpen(true);
  }

  function openEdit(key: APIKeyResp) {
    setEditingKey(key);
    setForm({
      name: key.name,
      group_id: String(key.group_id),
      quota_usd: key.quota_usd ? String(key.quota_usd) : '',
      expires_at: key.expires_at ? key.expires_at.slice(0, 10) : '',
    });
    setModalOpen(true);
  }

  function closeModal() {
    setModalOpen(false);
    setEditingKey(null);
    setForm(emptyForm);
  }

  function handleSubmit() {
    if (!form.name) {
      toast('error', '请填写密钥名称');
      return;
    }
    if (!editingKey && !form.group_id) {
      toast('error', '请选择分组');
      return;
    }

    if (editingKey) {
      const payload: UpdateAPIKeyReq = {
        name: form.name,
        quota_usd: form.quota_usd ? Number(form.quota_usd) : undefined,
        expires_at: form.expires_at || undefined,
      };
      updateMutation.mutate({ id: editingKey.id, data: payload });
    } else {
      const payload: CreateAPIKeyReq = {
        name: form.name,
        group_id: Number(form.group_id),
        quota_usd: form.quota_usd ? Number(form.quota_usd) : undefined,
        expires_at: form.expires_at || undefined,
      };
      createMutation.mutate(payload);
    }
  }

  // 查找分组名称
  const groupMap = new Map(
    (groupsData?.list ?? []).map((g) => [g.id, g.name]),
  );

  const columns: Column<APIKeyResp>[] = [
    { key: 'name', title: '名称' },
    {
      key: 'key_prefix',
      title: '密钥前缀',
      render: (row) => (
        <code className="text-xs bg-gray-100 px-2 py-0.5 rounded">
          {row.key_prefix}...
        </code>
      ),
    },
    {
      key: 'group_id',
      title: '分组',
      render: (row) => groupMap.get(row.group_id) || `#${row.group_id}`,
    },
    {
      key: 'quota',
      title: '配额/已用',
      render: (row) => (
        <span>
          {row.quota_usd > 0 ? (
            <>
              ${row.used_quota.toFixed(4)} / ${row.quota_usd.toFixed(4)}
            </>
          ) : (
            <span className="text-gray-400">无限制</span>
          )}
        </span>
      ),
    },
    {
      key: 'expires_at',
      title: '过期时间',
      render: (row) =>
        row.expires_at
          ? new Date(row.expires_at).toLocaleDateString('zh-CN')
          : '永不过期',
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
          <Button size="sm" variant="ghost" onClick={() => openEdit(row)}>
            编辑
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="text-red-600"
            onClick={() => setDeleteTarget(row)}
          >
            删除
          </Button>
        </div>
      ),
    },
  ];

  const saving = createMutation.isPending || updateMutation.isPending;

  return (
    <div className="p-6">
      <PageHeader
        title="我的密钥"
        actions={<Button onClick={openCreate}>创建密钥</Button>}
      />

      <Table
        columns={columns}
        data={(data?.list ?? [])}
        loading={isLoading}
        rowKey={(row) => row.id as number}
        page={page}
        pageSize={20}
        total={data?.total ?? 0}
        onPageChange={setPage}
      />

      {/* 创建/编辑弹窗 */}
      <Modal
        open={modalOpen}
        onClose={closeModal}
        title={editingKey ? '编辑密钥' : '创建密钥'}
        footer={
          <>
            <Button variant="secondary" onClick={closeModal}>
              取消
            </Button>
            <Button onClick={handleSubmit} loading={saving}>
              {editingKey ? '保存' : '创建'}
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
            placeholder="例如：生产环境密钥"
          />
          {!editingKey && (
            <div className="space-y-1">
              <label className="block text-sm font-medium text-gray-700">
                分组 <span className="text-red-500 ml-1">*</span>
              </label>
              <select
                className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
                value={form.group_id}
                onChange={(e) => setForm({ ...form, group_id: e.target.value })}
              >
                <option value="">请选择分组</option>
                {(groupsData?.list ?? []).map((g) => (
                  <option key={g.id} value={g.id}>
                    {g.name} ({g.platform})
                  </option>
                ))}
              </select>
            </div>
          )}
          <Input
            label="配额 (USD)"
            type="number"
            value={form.quota_usd}
            onChange={(e) => setForm({ ...form, quota_usd: e.target.value })}
            placeholder="留空为无限制"
            hint="设为 0 或留空表示不限配额"
          />
          <Input
            label="过期时间"
            type="date"
            value={form.expires_at}
            onChange={(e) => setForm({ ...form, expires_at: e.target.value })}
            hint="留空表示永不过期"
          />
        </div>
      </Modal>

      {/* 新建密钥后显示完整密钥 */}
      <Modal
        open={!!createdKey}
        onClose={() => setCreatedKey(null)}
        title="密钥创建成功"
        footer={
          <Button onClick={() => setCreatedKey(null)}>我已保存，关闭</Button>
        }
      >
        <div className="space-y-3">
          <p className="text-sm text-red-600 font-medium">
            请立即复制并保存此密钥，关闭后将无法再次查看完整密钥！
          </p>
          <div className="bg-gray-50 rounded-md p-3 break-all font-mono text-sm">
            {createdKey}
          </div>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => {
              navigator.clipboard.writeText(createdKey || '');
              toast('success', '已复制到剪贴板');
            }}
          >
            复制密钥
          </Button>
        </div>
      </Modal>

      {/* 删除确认 */}
      <ConfirmModal
        open={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        onConfirm={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
        title="删除密钥"
        message={`确定要删除密钥「${deleteTarget?.name}」吗？删除后使用此密钥的请求将立即失效。`}
        loading={deleteMutation.isPending}
        danger
      />
    </div>
  );
}
