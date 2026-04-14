import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Layers, Hash, Gauge } from 'lucide-react';
import { Button } from '../../../shared/components/Button';
import { Input, Select } from '../../../shared/components/Input';
import { Modal } from '../../../shared/components/Modal';
import { accountsApi } from '../../../shared/api/accounts';
import { groupsApi } from '../../../shared/api/groups';
import { proxiesApi } from '../../../shared/api/proxies';
import { usePlatforms } from '../../../shared/hooks/usePlatforms';
import { queryKeys } from '../../../shared/queryKeys';
import { FETCH_ALL_PARAMS } from '../../../shared/constants';
import {
  usePluginAccountForm,
  createPluginOAuthBridge,
  getSchemaSelectedAccountType,
  getSchemaVisibleFields,
  filterCredentialsForAccountType,
} from './accountUtils';
import { SchemaCredentialsForm, GroupCheckboxList } from './CredentialForm';
import type { CreateAccountReq, AccountExportItem } from '../../../shared/types';
import type {
  PluginBatchAccountInput,
  PluginBatchImportResult,
} from '../../../app/plugin-loader';

export function CreateAccountModal({
  open,
  onClose,
  onSubmit,
  onBatchImport,
  loading,
  platforms,
}: {
  open: boolean;
  onClose: () => void;
  onSubmit: (data: CreateAccountReq) => void;
  onBatchImport?: (accounts: AccountExportItem[]) => Promise<PluginBatchImportResult>;
  loading: boolean;
  platforms: string[];
}) {
  const { t } = useTranslation();
  const { platformName: pName } = usePlatforms();
  const [platform, setPlatform] = useState('');
  const [accountType, setAccountType] = useState('');
  const [form, setForm] = useState<Omit<CreateAccountReq, 'platform' | 'credentials' | 'type'>>({
    name: '',
    priority: 0,
    max_concurrency: 5,
    rate_multiplier: 1,
  });
  const [credentials, setCredentials] = useState<Record<string, string>>({});
  const [groupIds, setGroupIds] = useState<number[]>([]);
  const [step, setStep] = useState(1);
  const [batchMode, setBatchMode] = useState(false);

  // 根据平台获取凭证字段定义
  const { data: schema } = useQuery({
    queryKey: queryKeys.credentialsSchema(platform),
    queryFn: () => accountsApi.credentialsSchema(platform),
    enabled: !!platform,
  });

  // 查询分组列表
  const { data: groupsData } = useQuery({
    queryKey: queryKeys.groupsAll(),
    queryFn: () => groupsApi.list(FETCH_ALL_PARAMS),
  });

  // 查询代理列表
  const { data: proxiesData } = useQuery({
    queryKey: queryKeys.proxiesAll(),
    queryFn: () => proxiesApi.list(FETCH_ALL_PARAMS),
  });

  // 加载插件自定义表单组件
  const { Form: PluginAccountForm, pluginId } = usePluginAccountForm(platform);
  const pluginOAuth = createPluginOAuthBridge(pluginId);

  useEffect(() => {
    const selectedType = getSchemaSelectedAccountType(schema, accountType);
    if (!selectedType || selectedType.key === accountType) return;
    setAccountType(selectedType.key);
  }, [schema, accountType]);

  // 弹窗关闭时重置所有内部状态，避免父组件直接 setShowCreateModal(false)
  // 绕过 handleClose 导致的状态残留（例如重开后停留在第 2 步）
  useEffect(() => {
    if (open) return;
    setPlatform('');
    setAccountType('');
    setForm({ name: '', priority: 0, max_concurrency: 5, rate_multiplier: 1 });
    setCredentials({});
    setGroupIds([]);
    setStep(1);
    setBatchMode(false);
  }, [open]);

  // 平台变化时重置凭证和账号类型
  const handlePlatformChange = (newPlatform: string) => {
    setPlatform(newPlatform);
    setCredentials({});
    setAccountType('');
    setBatchMode(false);
  };

  // 插件表单触发的批量导入：补全 platform/元数据后交给外层 import
  // 命名规则：
  //  1. 填了名称 → 作为前缀，生成 {prefix}1 / {prefix}2 / ...
  //  2. 未填名称 → 优先用插件返回的账号名（通常是邮箱）
  //  3. 兜底 → "Claude Code {i+1}"
  const handlePluginBatchImport = async (
    accounts: PluginBatchAccountInput[],
  ): Promise<PluginBatchImportResult> => {
    if (!onBatchImport) return { imported: 0, failed: accounts.length };
    const prefix = form.name.trim();
    const toImport: AccountExportItem[] = accounts.map((a, i) => ({
      name: prefix ? `${prefix}${i + 1}` : a.name || `Claude Code ${i + 1}`,
      platform,
      type: a.type || 'oauth',
      credentials: a.credentials,
      priority: form.priority ?? 0,
      max_concurrency: form.max_concurrency ?? 5,
      rate_multiplier: form.rate_multiplier ?? 1,
      group_ids: groupIds.length ? groupIds : undefined,
      proxy_id: form.proxy_id,
    }));
    return onBatchImport(toImport);
  };

  const handleSchemaAccountTypeChange = (type: string) => {
    const selectedType = getSchemaSelectedAccountType(schema, type);
    setAccountType(type);
    setCredentials((prev) => filterCredentialsForAccountType(prev, selectedType));
  };

  const handleSubmit = () => {
    if (!platform || !form.name) return;
    onSubmit({
      ...form,
      platform,
      type: accountType || undefined,
      credentials,
      group_ids: groupIds,
    });
  };

  const handleClose = () => {
    setPlatform('');
    setAccountType('');
    setForm({ name: '', priority: 0, max_concurrency: 5, rate_multiplier: 1 });
    setCredentials({});
    setGroupIds([]);
    setStep(1);
    setBatchMode(false);
    onClose();
  };

  return (
    <Modal
      open={open}
      onClose={handleClose}
      title={`${t('accounts.create')} (${step}/2)`}
      width="560px"
      footer={
        <div className="flex justify-between w-full">
          <div>
            {step > 1 && !batchMode && (
              <Button variant="secondary" onClick={() => setStep(1)}>
                {t('common.back', '上一步')}
              </Button>
            )}
          </div>
          <div className="flex gap-2">
            <Button variant="secondary" onClick={handleClose}>
              {t('common.cancel')}
            </Button>
            {!batchMode && (step === 1 ? (
              <Button onClick={() => setStep(2)} disabled={!platform || !form.name}>
                {t('common.next', '下一步')}
              </Button>
            ) : (
              <Button onClick={handleSubmit} loading={loading}>
                {t('common.create')}
              </Button>
            ))}
          </div>
        </div>
      }
    >
      {step === 1 ? (
        <div className="space-y-4">
          <Select
            label={t('accounts.platform')}
            required
            value={platform}
            onChange={(e) => handlePlatformChange(e.target.value)}
            options={[
              { value: '', label: t('accounts.select_platform') },
              ...platforms.map((p) => ({ value: p, label: pName(p) })),
            ]}
          />

          <Input
            label={t('common.name')}
            required={!batchMode}
            value={form.name}
            onChange={(e) => setForm({ ...form, name: e.target.value })}
            icon={<Layers className="w-4 h-4" />}
          />

          {/* 凭证区域：插件自定义表单 or 默认 schema 驱动 */}
          {PluginAccountForm ? (
            <div
              className="ag-plugin-scope pt-4"
              style={{ borderTop: '1px solid var(--ag-border)' }}
            >
              <PluginAccountForm
                credentials={credentials}
                onChange={setCredentials}
                mode="create"
                accountType={accountType}
                onAccountTypeChange={setAccountType}
                onSuggestedName={(name) =>
                  setForm((prev) => (prev.name ? prev : { ...prev, name }))
                }
                onBatchModeChange={setBatchMode}
                onBatchImport={handlePluginBatchImport}
                oauth={pluginOAuth}
              />
            </div>
          ) : schema && getSchemaVisibleFields(schema, accountType).length > 0 ? (
            <SchemaCredentialsForm
              schema={schema}
              accountType={accountType}
              onAccountTypeChange={handleSchemaAccountTypeChange}
              credentials={credentials}
              onCredentialsChange={setCredentials}
            />
          ) : null}
        </div>
      ) : (
        <div className="space-y-4">
          <Input
            label={t('accounts.priority_hint')}
            type="number"
            min={0}
            max={999}
            step={1}
            value={String(form.priority ?? 50)}
            onChange={(e) => {
              const v = Math.round(Number(e.target.value));
              setForm({ ...form, priority: Math.max(0, Math.min(999, v)) });
            }}
            icon={<Hash className="w-4 h-4" />}
          />

          <GroupCheckboxList
            groups={(groupsData?.list ?? []).filter(g => g.platform === platform)}
            selectedIds={groupIds}
            onChange={setGroupIds}
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
          <Select
            label={t('accounts.proxy')}
            value={form.proxy_id == null ? '' : String(form.proxy_id)}
            onChange={(e) =>
              setForm({
                ...form,
                proxy_id: e.target.value ? Number(e.target.value) : undefined,
              })
            }
            options={[
              { value: '', label: t('accounts.no_proxy') },
              ...(proxiesData?.list ?? []).map((p) => ({
                value: String(p.id),
                label: `${p.name} (${p.protocol}://${p.address}:${p.port})`,
              })),
            ]}
          />
        </div>
      )}
    </Modal>
  );
}
