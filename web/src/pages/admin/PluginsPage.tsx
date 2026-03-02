import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import { pluginsApi } from '../../shared/api/plugins';
import { useToast } from '../../shared/components/Toast';
import { PageHeader } from '../../shared/components/PageHeader';
import { Table, type Column } from '../../shared/components/Table';
import { Button } from '../../shared/components/Button';
import { Modal, ConfirmModal } from '../../shared/components/Modal';
import { Card } from '../../shared/components/Card';
import { Badge, StatusBadge } from '../../shared/components/Badge';
import { Textarea } from '../../shared/components/Input';
import type { PluginResp, MarketplacePluginResp } from '../../shared/types';

// 插件类型 Badge 颜色
const typeVariant: Record<string, 'info' | 'success' | 'warning'> = {
  gateway: 'info',
  payment: 'success',
  extension: 'warning',
};

export default function PluginsPage() {
  const { toast } = useToast();
  const queryClient = useQueryClient();

  const [activeTab, setActiveTab] = useState<'installed' | 'marketplace'>('installed');
  const [configTarget, setConfigTarget] = useState<PluginResp | null>(null);
  const [configJson, setConfigJson] = useState('');
  const [configError, setConfigError] = useState('');
  const [uninstallTarget, setUninstallTarget] = useState<PluginResp | null>(null);

  // 已安装插件列表
  const { data: pluginsData, isLoading: pluginsLoading } = useQuery({
    queryKey: ['plugins'],
    queryFn: () => pluginsApi.list({ page: 1, page_size: 100 }),
  });

  // 插件市场列表
  const { data: marketData, isLoading: marketLoading } = useQuery({
    queryKey: ['marketplace'],
    queryFn: () => pluginsApi.marketplace({ page: 1, page_size: 100 }),
    enabled: activeTab === 'marketplace',
  });

  // 启用插件
  const enableMutation = useMutation({
    mutationFn: (id: number) => pluginsApi.enable(id),
    onSuccess: () => {
      toast('success', '插件已启用');
      queryClient.invalidateQueries({ queryKey: ['plugins'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 禁用插件
  const disableMutation = useMutation({
    mutationFn: (id: number) => pluginsApi.disable(id),
    onSuccess: () => {
      toast('success', '插件已禁用');
      queryClient.invalidateQueries({ queryKey: ['plugins'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 卸载插件
  const uninstallMutation = useMutation({
    mutationFn: (id: number) => pluginsApi.uninstall(id),
    onSuccess: () => {
      toast('success', '插件已卸载');
      queryClient.invalidateQueries({ queryKey: ['plugins'] });
      queryClient.invalidateQueries({ queryKey: ['marketplace'] });
      setUninstallTarget(null);
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 安装插件
  const installMutation = useMutation({
    mutationFn: (name: string) => pluginsApi.install({ name }),
    onSuccess: () => {
      toast('success', '插件安装成功');
      queryClient.invalidateQueries({ queryKey: ['plugins'] });
      queryClient.invalidateQueries({ queryKey: ['marketplace'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 更新配置
  const configMutation = useMutation({
    mutationFn: ({ id, config }: { id: number; config: Record<string, unknown> }) =>
      pluginsApi.updateConfig(id, { config }),
    onSuccess: () => {
      toast('success', '配置已保存');
      queryClient.invalidateQueries({ queryKey: ['plugins'] });
      setConfigTarget(null);
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 打开配置弹窗
  function openConfig(plugin: PluginResp) {
    setConfigTarget(plugin);
    setConfigJson(JSON.stringify(plugin.config || {}, null, 2));
    setConfigError('');
  }

  // 保存配置
  function handleSaveConfig() {
    try {
      const parsed = JSON.parse(configJson);
      setConfigError('');
      configMutation.mutate({ id: configTarget!.id, config: parsed });
    } catch {
      setConfigError('JSON 格式不正确');
    }
  }

  // 切换启用/禁用
  function togglePlugin(plugin: PluginResp) {
    if (plugin.status === 'enabled') {
      disableMutation.mutate(plugin.id);
    } else {
      enableMutation.mutate(plugin.id);
    }
  }

  const installedColumns: Column<PluginResp>[] = [
    { key: 'name', title: '名称' },
    { key: 'platform', title: '平台' },
    { key: 'version', title: '版本' },
    {
      key: 'type',
      title: '类型',
      render: (row) => (
        <Badge variant={typeVariant[row.type] || 'default'}>{row.type}</Badge>
      ),
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
          <Button
            size="sm"
            variant="ghost"
            onClick={() => togglePlugin(row)}
            loading={enableMutation.isPending || disableMutation.isPending}
          >
            {row.status === 'enabled' ? '禁用' : '启用'}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => openConfig(row)}>
            配置
          </Button>
          <Button
            size="sm"
            variant="ghost"
            className="text-red-600"
            onClick={() => setUninstallTarget(row)}
          >
            卸载
          </Button>
        </div>
      ),
    },
  ];

  const tabs = [
    { key: 'installed' as const, label: '已安装' },
    { key: 'marketplace' as const, label: '插件市场' },
  ];

  return (
    <div className="p-6">
      <PageHeader title="插件管理" />

      {/* Tab 切换 */}
      <div className="flex gap-1 mb-6 border-b border-gray-200">
        {tabs.map((tab) => (
          <button
            key={tab.key}
            className={`px-4 py-2 text-sm font-medium border-b-2 transition-colors ${
              activeTab === tab.key
                ? 'border-indigo-600 text-indigo-600'
                : 'border-transparent text-gray-500 hover:text-gray-700'
            }`}
            onClick={() => setActiveTab(tab.key)}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* 已安装 Tab */}
      {activeTab === 'installed' && (
        <Table
          columns={installedColumns}
          data={(pluginsData?.list ?? [])}
          loading={pluginsLoading}
          rowKey={(row) => row.id as number}
        />
      )}

      {/* 插件市场 Tab */}
      {activeTab === 'marketplace' && (
        <div>
          {marketLoading ? (
            <div className="flex items-center justify-center py-12">
              <svg className="animate-spin h-6 w-6 text-indigo-600" viewBox="0 0 24 24">
                <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4" fill="none" />
                <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
              </svg>
              <span className="ml-2 text-gray-500">加载中...</span>
            </div>
          ) : (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {(marketData?.list ?? []).map((plugin: MarketplacePluginResp) => (
                <MarketplaceCard
                  key={plugin.name}
                  plugin={plugin}
                  onInstall={() => installMutation.mutate(plugin.name)}
                  installing={installMutation.isPending}
                />
              ))}
              {(marketData?.list ?? []).length === 0 && (
                <div className="col-span-full text-center py-12 text-gray-500">
                  暂无可用插件
                </div>
              )}
            </div>
          )}
        </div>
      )}

      {/* 配置弹窗 */}
      <Modal
        open={!!configTarget}
        onClose={() => setConfigTarget(null)}
        title={`配置 - ${configTarget?.name}`}
        width="600px"
        footer={
          <>
            <Button variant="secondary" onClick={() => setConfigTarget(null)}>
              取消
            </Button>
            <Button onClick={handleSaveConfig} loading={configMutation.isPending}>
              保存
            </Button>
          </>
        }
      >
        <Textarea
          label="插件配置 (JSON)"
          value={configJson}
          onChange={(e) => setConfigJson(e.target.value)}
          rows={12}
          className="font-mono text-sm"
          error={configError}
        />
      </Modal>

      {/* 卸载确认 */}
      <ConfirmModal
        open={!!uninstallTarget}
        onClose={() => setUninstallTarget(null)}
        onConfirm={() => uninstallTarget && uninstallMutation.mutate(uninstallTarget.id)}
        title="卸载插件"
        message={`确定要卸载插件「${uninstallTarget?.name}」吗？卸载后相关功能将不可用。`}
        loading={uninstallMutation.isPending}
        danger
      />
    </div>
  );
}

// 插件市场卡片组件
function MarketplaceCard({
  plugin,
  onInstall,
  installing,
}: {
  plugin: MarketplacePluginResp;
  onInstall: () => void;
  installing: boolean;
}) {
  return (
    <Card>
      <div className="flex flex-col h-full">
        <div className="flex items-start justify-between mb-2">
          <h3 className="font-semibold text-gray-900">{plugin.name}</h3>
          <Badge variant={typeVariant[plugin.type] || 'default'}>{plugin.type}</Badge>
        </div>
        <p className="text-sm text-gray-500 flex-1 mb-3">
          {plugin.description || '暂无描述'}
        </p>
        <div className="flex items-center justify-between text-xs text-gray-400">
          <span>作者: {plugin.author}</span>
          <span>v{plugin.version}</span>
        </div>
        <div className="mt-3 pt-3 border-t border-gray-100">
          {plugin.installed ? (
            <Badge variant="success">已安装</Badge>
          ) : (
            <Button size="sm" onClick={onInstall} loading={installing}>
              安装
            </Button>
          )}
        </div>
      </div>
    </Card>
  );
}
