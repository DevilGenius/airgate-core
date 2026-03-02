import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { proxiesApi } from '../../shared/api/proxies';
import { useToast } from '../../shared/components/Toast';
import { PageHeader } from '../../shared/components/PageHeader';
import { Table, type Column } from '../../shared/components/Table';
import { Button } from '../../shared/components/Button';
import { Input } from '../../shared/components/Input';
import { Modal, ConfirmModal } from '../../shared/components/Modal';
import { Badge, StatusBadge } from '../../shared/components/Badge';
import type { ProxyResp, CreateProxyReq, UpdateProxyReq } from '../../shared/types';

// 代理表单数据
interface ProxyForm {
  name: string;
  protocol: 'http' | 'socks5';
  address: string;
  port: string;
  username: string;
  password: string;
}

const emptyForm: ProxyForm = {
  name: '',
  protocol: 'http',
  address: '',
  port: '',
  username: '',
  password: '',
};

export default function ProxiesPage() {
  const { toast } = useToast();
  const queryClient = useQueryClient();

  const [page, setPage] = useState(1);
  const [modalOpen, setModalOpen] = useState(false);
  const [editingProxy, setEditingProxy] = useState<ProxyResp | null>(null);
  const [form, setForm] = useState<ProxyForm>(emptyForm);
  const [deleteTarget, setDeleteTarget] = useState<ProxyResp | null>(null);
  const [testingId, setTestingId] = useState<number | null>(null);

  // 查询代理列表
  const { data, isLoading } = useQuery({
    queryKey: ['proxies', page],
    queryFn: () => proxiesApi.list({ page, page_size: 20 }),
  });

  // 创建代理
  const createMutation = useMutation({
    mutationFn: (data: CreateProxyReq) => proxiesApi.create(data),
    onSuccess: () => {
      toast('success', '代理创建成功');
      queryClient.invalidateQueries({ queryKey: ['proxies'] });
      closeModal();
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 更新代理
  const updateMutation = useMutation({
    mutationFn: ({ id, data }: { id: number; data: UpdateProxyReq }) =>
      proxiesApi.update(id, data),
    onSuccess: () => {
      toast('success', '代理更新成功');
      queryClient.invalidateQueries({ queryKey: ['proxies'] });
      closeModal();
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 删除代理
  const deleteMutation = useMutation({
    mutationFn: (id: number) => proxiesApi.delete(id),
    onSuccess: () => {
      toast('success', '代理已删除');
      queryClient.invalidateQueries({ queryKey: ['proxies'] });
      setDeleteTarget(null);
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 测试连通性
  const testMutation = useMutation({
    mutationFn: (id: number) => proxiesApi.test(id),
    onSuccess: (result) => {
      if (result.success) {
        toast('success', `连通性测试成功，延迟: ${result.latency_ms}ms`);
      } else {
        toast('error', `连通性测试失败: ${result.error_msg || '未知错误'}`);
      }
      setTestingId(null);
    },
    onError: (err: Error) => {
      toast('error', err.message);
      setTestingId(null);
    },
  });

  // 打开创建弹窗
  function openCreate() {
    setEditingProxy(null);
    setForm(emptyForm);
    setModalOpen(true);
  }

  // 打开编辑弹窗
  function openEdit(proxy: ProxyResp) {
    setEditingProxy(proxy);
    setForm({
      name: proxy.name,
      protocol: proxy.protocol,
      address: proxy.address,
      port: String(proxy.port),
      username: proxy.username || '',
      password: '',
    });
    setModalOpen(true);
  }

  // 关闭弹窗
  function closeModal() {
    setModalOpen(false);
    setEditingProxy(null);
    setForm(emptyForm);
  }

  // 提交表单
  function handleSubmit() {
    if (!form.name || !form.address || !form.port) {
      toast('error', '请填写必填项');
      return;
    }

    const payload = {
      name: form.name,
      protocol: form.protocol,
      address: form.address,
      port: Number(form.port),
      username: form.username || undefined,
      password: form.password || undefined,
    };

    if (editingProxy) {
      updateMutation.mutate({ id: editingProxy.id, data: payload });
    } else {
      createMutation.mutate(payload as CreateProxyReq);
    }
  }

  // 测试连通性
  function handleTest(id: number) {
    setTestingId(id);
    testMutation.mutate(id);
  }

  const columns: Column<ProxyResp>[] = [
    { key: 'id', title: 'ID', width: '60px' },
    { key: 'name', title: '名称' },
    {
      key: 'protocol',
      title: '协议',
      render: (row) => (
        <Badge variant={row.protocol === 'http' ? 'info' : 'warning'}>
          {row.protocol}
        </Badge>
      ),
    },
    {
      key: 'endpoint',
      title: '地址',
      render: (row) => `${row.address}:${row.port}`,
    },
    { key: 'username', title: '用户名', render: (row) => row.username || '-' },
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
            loading={testingId === row.id}
            onClick={() => handleTest(row.id)}
          >
            测试
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
        title="代理池"
        actions={<Button onClick={openCreate}>添加代理</Button>}
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
        title={editingProxy ? '编辑代理' : '添加代理'}
        footer={
          <>
            <Button variant="secondary" onClick={closeModal}>
              取消
            </Button>
            <Button onClick={handleSubmit} loading={saving}>
              {editingProxy ? '保存' : '创建'}
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
            placeholder="例如：美国代理-1"
          />
          <div className="space-y-1">
            <label className="block text-sm font-medium text-gray-700">
              协议 <span className="text-red-500 ml-1">*</span>
            </label>
            <select
              className="block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:border-indigo-500"
              value={form.protocol}
              onChange={(e) =>
                setForm({
                  ...form,
                  protocol: e.target.value as 'http' | 'socks5',
                })
              }
            >
              <option value="http">HTTP</option>
              <option value="socks5">SOCKS5</option>
            </select>
          </div>
          <Input
            label="地址"
            required
            value={form.address}
            onChange={(e) => setForm({ ...form, address: e.target.value })}
            placeholder="例如：proxy.example.com"
          />
          <Input
            label="端口"
            required
            type="number"
            value={form.port}
            onChange={(e) => setForm({ ...form, port: e.target.value })}
            placeholder="例如：1080"
          />
          <Input
            label="用户名"
            value={form.username}
            onChange={(e) => setForm({ ...form, username: e.target.value })}
            placeholder="可选"
          />
          <Input
            label="密码"
            type="password"
            value={form.password}
            onChange={(e) => setForm({ ...form, password: e.target.value })}
            placeholder={editingProxy ? '留空则不修改' : '可选'}
          />
        </div>
      </Modal>

      {/* 删除确认 */}
      <ConfirmModal
        open={!!deleteTarget}
        onClose={() => setDeleteTarget(null)}
        onConfirm={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
        title="删除代理"
        message={`确定要删除代理「${deleteTarget?.name}」吗？使用此代理的账号将受到影响。`}
        loading={deleteMutation.isPending}
        danger
      />
    </div>
  );
}
