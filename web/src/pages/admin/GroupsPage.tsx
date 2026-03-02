import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { PageHeader } from '../../shared/components/PageHeader';
import { Button } from '../../shared/components/Button';
import { Input, Textarea } from '../../shared/components/Input';
import { Table, type Column } from '../../shared/components/Table';
import { Modal } from '../../shared/components/Modal';
import { Badge } from '../../shared/components/Badge';
import { useToast } from '../../shared/components/Toast';
import { groupsApi } from '../../shared/api/groups';
import type { GroupResp, CreateGroupReq, UpdateGroupReq } from '../../shared/types';

const PAGE_SIZE = 20;
const PLATFORMS = ['openai', 'claude', 'gemini', 'sora'];

export default function GroupsPage() {
  const { toast } = useToast();
  const queryClient = useQueryClient();

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
      toast('success', '分组创建成功');
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
      toast('success', '分组更新成功');
      setEditingGroup(null);
      queryClient.invalidateQueries({ queryKey: ['groups'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 表格列定义
  const columns: Column<GroupResp>[] = [
    { key: 'id', title: 'ID', width: '60px' },
    { key: 'name', title: '名称' },
    { key: 'platform', title: '平台' },
    {
      key: 'subscription_type',
      title: '订阅类型',
      render: (row) => (
        <Badge variant={row.subscription_type === 'subscription' ? 'info' : 'default'}>
          {row.subscription_type === 'subscription' ? '订阅制' : '标准'}
        </Badge>
      ),
    },
    {
      key: 'rate_multiplier',
      title: '倍率',
      width: '80px',
      render: (row) => `${row.rate_multiplier}x`,
    },
    {
      key: 'is_exclusive',
      title: '专属',
      width: '80px',
      render: (row) => (row.is_exclusive ? '是' : '否'),
    },
    { key: 'sort_weight', title: '排序权重', width: '100px' },
    {
      key: 'actions',
      title: '操作',
      render: (row) => (
        <Button size="sm" variant="ghost" onClick={() => setEditingGroup(row)}>
          编辑
        </Button>
      ),
    },
  ];

  return (
    <div>
      <PageHeader
        title="分组管理"
        actions={
          <Button onClick={() => setShowCreateModal(true)}>创建分组</Button>
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
        title="创建分组"
        onClose={() => setShowCreateModal(false)}
        onSubmit={(data) => createMutation.mutate(data as CreateGroupReq)}
        loading={createMutation.isPending}
      />

      {/* 编辑弹窗 */}
      {editingGroup && (
        <GroupFormModal
          open
          title="编辑分组"
          group={editingGroup}
          onClose={() => setEditingGroup(null)}
          onSubmit={(data) =>
            updateMutation.mutate({ id: editingGroup.id, data })
          }
          loading={updateMutation.isPending}
        />
      )}
    </div>
  );
}

// ==================== 分组表单弹窗 ====================

function GroupFormModal({
  open,
  title,
  group,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  title: string;
  group?: GroupResp;
  onClose: () => void;
  onSubmit: (data: CreateGroupReq | UpdateGroupReq) => void;
  loading: boolean;
}) {
  const isEdit = !!group;

  const [form, setForm] = useState({
    name: group?.name ?? '',
    platform: group?.platform ?? '',
    rate_multiplier: group?.rate_multiplier ?? 1,
    is_exclusive: group?.is_exclusive ?? false,
    subscription_type: group?.subscription_type ?? 'standard' as const,
    sort_weight: group?.sort_weight ?? 0,
  });

  // 模型路由和配额用 JSON 文本编辑（简化实现）
  const [modelRoutingJson, setModelRoutingJson] = useState(
    group?.model_routing ? JSON.stringify(group.model_routing, null, 2) : '',
  );
  const [quotasJson, setQuotasJson] = useState(
    group?.quotas ? JSON.stringify(group.quotas, null, 2) : '',
  );
  const [jsonError, setJsonError] = useState('');

  const handleSubmit = () => {
    if (!isEdit && (!form.name || !form.platform)) return;

    let model_routing: Record<string, number[]> | undefined;
    let quotas: Record<string, unknown> | undefined;

    try {
      if (modelRoutingJson.trim()) {
        model_routing = JSON.parse(modelRoutingJson);
      }
      if (quotasJson.trim()) {
        quotas = JSON.parse(quotasJson);
      }
      setJsonError('');
    } catch {
      setJsonError('JSON 格式错误，请检查');
      return;
    }

    onSubmit({
      ...form,
      subscription_type: form.subscription_type as 'standard' | 'subscription',
      model_routing,
      quotas,
    });
  };

  const handleClose = () => {
    setJsonError('');
    onClose();
  };

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title={title}
      width="560px"
      footer={
        <>
          <Button variant="secondary" onClick={handleClose}>
            取消
          </Button>
          <Button onClick={handleSubmit} loading={loading}>
            {isEdit ? '保存' : '创建'}
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
        />

        {isEdit ? (
          <Input label="平台" value={form.platform} disabled />
        ) : (
          <div className="space-y-1">
            <label className="block text-sm font-medium text-gray-700">
              平台 <span className="text-red-500">*</span>
            </label>
            <select
              className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
              value={form.platform}
              onChange={(e) => setForm({ ...form, platform: e.target.value })}
            >
              <option value="">请选择平台</option>
              {PLATFORMS.map((p) => (
                <option key={p} value={p}>
                  {p}
                </option>
              ))}
            </select>
          </div>
        )}

        <Input
          label="费率倍率"
          type="number"
          step="0.1"
          value={String(form.rate_multiplier)}
          onChange={(e) =>
            setForm({ ...form, rate_multiplier: Number(e.target.value) })
          }
        />

        <div className="flex items-center gap-2">
          <input
            type="checkbox"
            id="is_exclusive"
            checked={form.is_exclusive}
            onChange={(e) =>
              setForm({ ...form, is_exclusive: e.target.checked })
            }
            className="rounded border-gray-300 text-indigo-600"
          />
          <label htmlFor="is_exclusive" className="text-sm text-gray-700">
            专属分组（需要订阅才能使用）
          </label>
        </div>

        <div className="space-y-1">
          <label className="block text-sm font-medium text-gray-700">订阅类型</label>
          <select
            className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
            value={form.subscription_type}
            onChange={(e) =>
              setForm({
                ...form,
                subscription_type: e.target.value as 'standard' | 'subscription',
              })
            }
          >
            <option value="standard">标准</option>
            <option value="subscription">订阅制</option>
          </select>
        </div>

        <Input
          label="排序权重"
          type="number"
          value={String(form.sort_weight)}
          onChange={(e) =>
            setForm({ ...form, sort_weight: Number(e.target.value) })
          }
          hint="数值越大排序越靠前"
        />

        {/* 模型路由 JSON */}
        <Textarea
          label="模型路由 (JSON)"
          value={modelRoutingJson}
          rows={4}
          placeholder='{"gpt-4": [1, 2], "gpt-3.5-turbo": [3]}'
          onChange={(e) => setModelRoutingJson(e.target.value)}
        />

        {/* 配额 JSON */}
        <Textarea
          label="配额限制 (JSON)"
          value={quotasJson}
          rows={4}
          placeholder='{"daily": 100, "monthly": 3000}'
          onChange={(e) => setQuotasJson(e.target.value)}
          error={jsonError}
        />
      </div>
    </Modal>
  );
}
