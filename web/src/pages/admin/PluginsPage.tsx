import { useState } from 'react';
import { useTranslation } from 'react-i18next';
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
import {
  Power, PowerOff, Settings, Trash2, Download, Loader2,
  Package, User, Tag,
} from 'lucide-react';
import type { PluginResp, MarketplacePluginResp } from '../../shared/types';

// 插件类型 Badge 颜色
const typeVariant: Record<string, 'info' | 'success' | 'warning'> = {
  gateway: 'info',
  payment: 'success',
  extension: 'warning',
};

export default function PluginsPage() {
  const { t } = useTranslation();
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
      toast('success', t('plugins.enable_success'));
      queryClient.invalidateQueries({ queryKey: ['plugins'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 禁用插件
  const disableMutation = useMutation({
    mutationFn: (id: number) => pluginsApi.disable(id),
    onSuccess: () => {
      toast('success', t('plugins.disable_success'));
      queryClient.invalidateQueries({ queryKey: ['plugins'] });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 卸载插件
  const uninstallMutation = useMutation({
    mutationFn: (id: number) => pluginsApi.uninstall(id),
    onSuccess: () => {
      toast('success', t('plugins.uninstall_success'));
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
      toast('success', t('plugins.install_success'));
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
      toast('success', t('plugins.config_success'));
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
    {
      key: 'name',
      title: t('common.name'),
      render: (row) => <span className="text-[var(--ag-text)] font-medium">{row.name}</span>,
    },
    { key: 'platform', title: t('plugins.platform') },
    {
      key: 'version',
      title: t('common.version'),
      render: (row) => (
        <span style={{ fontFamily: 'var(--ag-font-mono)' }}>{row.version}</span>
      ),
    },
    {
      key: 'type',
      title: t('common.type'),
      render: (row) => (
        <Badge variant={typeVariant[row.type] || 'default'}>{row.type}</Badge>
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
          <Button
            size="sm"
            variant="ghost"
            icon={
              row.status === 'enabled'
                ? <PowerOff className="w-3.5 h-3.5" />
                : <Power className="w-3.5 h-3.5" />
            }
            onClick={() => togglePlugin(row)}
            loading={enableMutation.isPending || disableMutation.isPending}
          >
            {row.status === 'enabled' ? t('common.disable') : t('common.enable')}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            icon={<Settings className="w-3.5 h-3.5" />}
            onClick={() => openConfig(row)}
          >
            {t('plugins.config')}
          </Button>
          <Button
            size="sm"
            variant="ghost"
            icon={<Trash2 className="w-3.5 h-3.5" />}
            className="text-[var(--ag-danger)]"
            onClick={() => setUninstallTarget(row)}
          >
            {t('common.uninstall')}
          </Button>
        </div>
      ),
    },
  ];

  const tabs = [
    { key: 'installed' as const, label: t('plugins.installed_tab') },
    { key: 'marketplace' as const, label: t('plugins.marketplace_tab') },
  ];

  return (
    <div>
      <PageHeader
        title={t('plugins.title')}
        description={t('plugins.description')}
      />

      {/* Tab 切换 */}
      <div className="flex gap-1 mb-6 border-b border-[var(--ag-border)]">
        {tabs.map((tab) => (
          <button
            key={tab.key}
            className={`px-4 py-2.5 text-xs font-semibold uppercase tracking-wider border-b-2 transition-all duration-200 cursor-pointer ${
              activeTab === tab.key
                ? 'border-[var(--ag-primary)] text-[var(--ag-primary)] shadow-[0_2px_8px_var(--ag-primary-glow)]'
                : 'border-transparent text-[var(--ag-text-tertiary)] hover:text-[var(--ag-text-secondary)]'
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
          data={pluginsData?.list ?? []}
          loading={pluginsLoading}
          rowKey={(row) => row.id as number}
        />
      )}

      {/* 插件市场 Tab */}
      {activeTab === 'marketplace' && (
        <div>
          {marketLoading ? (
            <div className="flex items-center justify-center py-16">
              <Loader2 className="w-5 h-5 animate-spin text-[var(--ag-primary)]" />
              <span className="ml-2 text-sm text-[var(--ag-text-tertiary)]">{t('common.loading')}</span>
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
                <div className="col-span-full text-center py-16 text-[var(--ag-text-tertiary)]">
                  {t('plugins.no_plugins')}
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
        title={`${t('plugins.config_title')} - ${configTarget?.name}`}
        width="600px"
        footer={
          <>
            <Button variant="secondary" onClick={() => setConfigTarget(null)}>
              {t('common.cancel')}
            </Button>
            <Button onClick={handleSaveConfig} loading={configMutation.isPending}>
              {t('common.save')}
            </Button>
          </>
        }
      >
        <Textarea
          label={t('plugins.config_label')}
          value={configJson}
          onChange={(e) => setConfigJson(e.target.value)}
          rows={12}
          style={{ fontFamily: 'var(--ag-font-mono)' }}
          error={configError}
        />
      </Modal>

      {/* 卸载确认 */}
      <ConfirmModal
        open={!!uninstallTarget}
        onClose={() => setUninstallTarget(null)}
        onConfirm={() => uninstallTarget && uninstallMutation.mutate(uninstallTarget.id)}
        title={t('plugins.uninstall_title')}
        message={t('plugins.uninstall_confirm', { name: uninstallTarget?.name })}
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
  const { t } = useTranslation();

  return (
    <Card>
      <div className="flex flex-col h-full">
        <div className="flex items-start justify-between mb-3">
          <div className="flex items-center gap-2">
            <Package className="w-4 h-4 text-[var(--ag-primary)]" />
            <h3 className="font-semibold text-[var(--ag-text)]">{plugin.name}</h3>
          </div>
          <Badge variant={typeVariant[plugin.type] || 'default'}>{plugin.type}</Badge>
        </div>
        <p className="text-sm text-[var(--ag-text-tertiary)] flex-1 mb-4 leading-relaxed">
          {plugin.description || t('common.no_data_desc')}
        </p>
        <div className="flex items-center justify-between text-xs text-[var(--ag-text-tertiary)] mb-3">
          <span className="flex items-center gap-1">
            <User className="w-3 h-3" />
            {plugin.author}
          </span>
          <span className="flex items-center gap-1" style={{ fontFamily: 'var(--ag-font-mono)' }}>
            <Tag className="w-3 h-3" />
            v{plugin.version}
          </span>
        </div>
        <div className="pt-3 border-t border-[var(--ag-border)]">
          {plugin.installed ? (
            <Badge variant="success">{t('plugins.already_installed')}</Badge>
          ) : (
            <Button
              size="sm"
              icon={<Download className="w-3.5 h-3.5" />}
              onClick={onInstall}
              loading={installing}
            >
              {t('common.install')}
            </Button>
          )}
        </div>
      </div>
    </Card>
  );
}
