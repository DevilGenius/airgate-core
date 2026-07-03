import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Button, Form, Input, Label, Spinner, TextField as HeroTextField, useOverlayState } from '@heroui/react';
import { IdCard, Hash, Gauge } from 'lucide-react';
import type {
  PluginBatchAccountInput,
  PluginBatchImportResult,
} from '@devilgenius/airgate-theme/plugin';
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
import { SchemaCredentialsForm } from './CredentialForm';
import { CommonModal } from '../../../shared/components/CommonModal';
import { NativeCheckbox } from '../../../shared/components/NativeCheckbox';
import { NativeSwitch } from '../../../shared/components/NativeSwitch';
import { SimpleSelect } from '../../../shared/components/SimpleSelect';
import {
  MAX_RATE_MULTIPLIER,
  MIN_POSITIVE_RATE_MULTIPLIER,
  RATE_MULTIPLIER_STEP,
  isEmptyRateMultiplierInput,
  isValidRateMultiplierValue,
  parseRateMultiplier,
} from '../../../shared/utils/rateMultiplier';
import type { CreateAccountReq, AccountExportItem } from '../../../shared/types';
import {
  ACCOUNT_PRIORITY_MAX,
  ACCOUNT_PRIORITY_MIN,
  commitAccountPriorityInput,
  DEFAULT_ACCOUNT_MAX_CONCURRENCY,
  DEFAULT_ACCOUNT_PRIORITY,
  getAccountMessageLockEnabled,
  isAccountPriorityDraft,
  parseAccountPriorityInput,
  setAccountGroupPriorities,
  setAccountMessageLockEnabled,
} from './accountDefaults';

const CREATE_ACCOUNT_FORM_ID = 'create-account-form';

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
    priority: DEFAULT_ACCOUNT_PRIORITY,
    max_concurrency: DEFAULT_ACCOUNT_MAX_CONCURRENCY,
    rate_multiplier: 1,
  });
  const [credentials, setCredentials] = useState<Record<string, string>>({});
  const [groupIds, setGroupIds] = useState<number[]>([]);
  const [groupPriorityInputs, setGroupPriorityInputs] = useState<Record<number, string>>({});
  const [batchMode, setBatchMode] = useState(false);
  const [priorityInput, setPriorityInput] = useState(String(DEFAULT_ACCOUNT_PRIORITY));
  const [rateMultiplierInput, setRateMultiplierInput] = useState('1');

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
  const { Form: PluginAccountForm, pluginId } = usePluginAccountForm(platform, 'create');
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
    setForm({ name: '', priority: DEFAULT_ACCOUNT_PRIORITY, max_concurrency: DEFAULT_ACCOUNT_MAX_CONCURRENCY, rate_multiplier: 1, upstream_is_pool: false });
    setPriorityInput(String(DEFAULT_ACCOUNT_PRIORITY));
    setRateMultiplierInput('1');
    setCredentials({});
    setGroupIds([]);
    setGroupPriorityInputs({});
    setBatchMode(false);
  }, [open]);

  // 平台变化时重置凭证和账号类型
  const handlePlatformChange = (newPlatform: string) => {
    setPlatform(newPlatform);
    setCredentials({});
    setAccountType('');
    setGroupIds([]);
    setGroupPriorityInputs({});
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
    const priority = commitAccountPriorityInput(priorityInput, form.priority ?? DEFAULT_ACCOUNT_PRIORITY);
    const rateMultiplier = isEmptyRateMultiplierInput(rateMultiplierInput)
      ? null
      : parseRateMultiplier(rateMultiplierInput) ?? 1;
    const toImport: AccountExportItem[] = accounts.map((a, i) => ({
      name: prefix ? `${prefix}${i + 1}` : a.name || `Claude Code ${i + 1}`,
      platform,
      type: a.type || 'oauth',
      credentials: a.credentials,
      priority,
      max_concurrency: form.max_concurrency ?? DEFAULT_ACCOUNT_MAX_CONCURRENCY,
      rate_multiplier: rateMultiplier,
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
    if (loading || batchMode || !platform || !form.name) return;
    const priority = commitAccountPriorityInput(priorityInput, form.priority ?? DEFAULT_ACCOUNT_PRIORITY);
    const rateMultiplierValue = parseRateMultiplier(rateMultiplierInput);
    const rateMultiplierEmpty = isEmptyRateMultiplierInput(rateMultiplierInput);
    if (!rateMultiplierEmpty && !isValidRateMultiplierValue(rateMultiplierValue)) return;
    const rateMultiplier = rateMultiplierEmpty ? null : rateMultiplierValue;
    onSubmit({
      ...form,
      priority,
      rate_multiplier: rateMultiplier,
      platform,
      type: accountType || undefined,
      credentials,
      extra: extraWithGroupPriorities(form.extra, groupIds, groupPriorityInputs),
      group_ids: groupIds,
    });
  };

  const handleClose = () => {
    setPlatform('');
    setAccountType('');
    setForm({ name: '', priority: DEFAULT_ACCOUNT_PRIORITY, max_concurrency: DEFAULT_ACCOUNT_MAX_CONCURRENCY, rate_multiplier: 1, upstream_is_pool: false });
    setPriorityInput(String(DEFAULT_ACCOUNT_PRIORITY));
    setRateMultiplierInput('1');
    setCredentials({});
    setGroupIds([]);
    setGroupPriorityInputs({});
    setBatchMode(false);
    onClose();
  };
  const handlePriorityChange = (value: string) => {
    if (!isAccountPriorityDraft(value)) return;
    setPriorityInput(value);
    const priority = parseAccountPriorityInput(value);
    if (priority != null) {
      setForm((prev) => ({ ...prev, priority }));
    }
  };
  const commitPriorityChange = () => {
    const priority = commitAccountPriorityInput(priorityInput, form.priority ?? DEFAULT_ACCOUNT_PRIORITY);
    setPriorityInput(String(priority));
    setForm((prev) => ({ ...prev, priority }));
  };
  const platformOptions = [
    { id: '', label: t('accounts.select_platform') },
    ...platforms.map((p) => ({ id: p, label: pName(p) })),
  ];
  const selectedPlatformLabel = platformOptions.find((item) => item.id === platform)?.label ?? t('accounts.select_platform');
  const rateMultiplierValid =
    isEmptyRateMultiplierInput(rateMultiplierInput) ||
    isValidRateMultiplierValue(parseRateMultiplier(rateMultiplierInput));
  const proxyOptions = [
    { id: '', label: t('accounts.no_proxy') },
    ...(proxiesData?.list ?? []).map((p) => ({
      id: String(p.id),
      label: `${p.name} (${p.protocol}://${p.address}:${p.port})`,
    })),
  ];
  const selectedProxyLabel =
    proxyOptions.find((item) => item.id === (form.proxy_id == null ? '' : String(form.proxy_id)))?.label ?? t('accounts.no_proxy');
  const availableGroups = (groupsData?.list ?? []).filter((group) => !platform || group.platform === platform);
  const toggleGroup = (id: number) => {
    if (groupIds.includes(id)) {
      setGroupIds((prev) => prev.filter((groupId) => groupId !== id));
      setGroupPriorityInputs((inputs) => omitGroupPriorityInput(inputs, id));
      return;
    }
    setGroupIds((prev) => [...prev, id]);
  };
  const handleGroupPriorityChange = (groupID: number, value: string) => {
    if (!isAccountPriorityDraft(value)) return;
    setGroupPriorityInputs((prev) => ({ ...prev, [groupID]: value }));
  };
  const commitGroupPriorityChange = (groupID: number) => {
    const value = groupPriorityInputs[groupID] ?? '';
    const priority = parseAccountPriorityInput(value);
    setGroupPriorityInputs((prev) => {
      if (priority == null) return omitGroupPriorityInput(prev, groupID);
      return { ...prev, [groupID]: String(priority) };
    });
  };
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) handleClose();
    },
  });

  return (
    <CommonModal
      className="ag-account-page-modal ag-create-account-modal"
      footer={(
        <div className="flex w-full justify-end gap-2">
          <Button variant="secondary" onPress={handleClose}>
            {t('common.cancel')}
          </Button>
          {!batchMode && (
            <Button
              aria-busy={loading}
              form={CREATE_ACCOUNT_FORM_ID}
              isDisabled={loading || !platform || !form.name || !rateMultiplierValid}
              type="submit"
              variant="primary"
            >
              {loading ? <Spinner size="sm" /> : null}
              {t('common.create')}
            </Button>
          )}
        </div>
      )}
      size="lg"
      state={modalState}
      title={t('accounts.create')}
    >
      <Form
        id={CREATE_ACCOUNT_FORM_ID}
        className="ag-form-scroll-safe ag-create-account-form"
        onSubmit={(event) => {
          event.preventDefault();
          handleSubmit();
        }}
      >
                <section className="space-y-4">
                  <div className="grid gap-4 md:grid-cols-2">
                    <div className="space-y-1.5">
                      <Label>{t('accounts.platform')}</Label>
                      <SimpleSelect
                        ariaLabel={t('accounts.platform')}
                      fullWidth
                        items={platformOptions.map((item) => ({ key: item.id, label: item.label }))}
                      selectedKey={platform}
                        selectedLabel={selectedPlatformLabel}
                        onSelectionChange={handlePlatformChange}
                      />
                    </div>

                    <HeroTextField fullWidth isRequired={!batchMode}>
                      <Label>{t('common.name')}</Label>
                      <div className="relative">
                        <IdCard className="pointer-events-none absolute left-3 top-1/2 z-10 w-4 h-4 -translate-y-1/2 text-text-tertiary" />
                        <Input
                          className="pl-9"
                          name="name"
                          autoComplete="off"
                          value={form.name}
                          onChange={(e) => setForm({ ...form, name: e.target.value })}
                          required={!batchMode}
                        />
                      </div>
                    </HeroTextField>
                  </div>
                </section>

                {PluginAccountForm ? (
                  <section
                    className="ag-plugin-scope border-t border-border pt-4"
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
                  </section>
                ) : schema && getSchemaVisibleFields(schema, accountType).length > 0 ? (
                  <SchemaCredentialsForm
                    schema={schema}
                    accountType={accountType}
                    onAccountTypeChange={handleSchemaAccountTypeChange}
                    credentials={credentials}
                    onCredentialsChange={setCredentials}
                  />
                ) : null}

                <section className="ag-create-account-advanced space-y-4">
                  <div className="grid gap-4 md:grid-cols-2">
                    <HeroTextField fullWidth>
                      <Label>{t('accounts.priority_hint')}</Label>
                      <div className="relative">
                        <Hash className="pointer-events-none absolute left-3 top-1/2 z-10 w-4 h-4 -translate-y-1/2 text-text-tertiary" />
                        <Input
                          className="pl-9"
                          type="text"
                          inputMode="numeric"
                          pattern="-?[0-9]*"
                          min={ACCOUNT_PRIORITY_MIN}
                          max={ACCOUNT_PRIORITY_MAX}
                          step={1}
                          value={priorityInput}
                          onBlur={commitPriorityChange}
                          onChange={(e) => handlePriorityChange(e.target.value)}
                        />
                      </div>
                    </HeroTextField>

                    <HeroTextField fullWidth>
                      <Label>{t('accounts.concurrency')}</Label>
                      <div className="relative">
                        <Gauge className="pointer-events-none absolute left-3 top-1/2 z-10 w-4 h-4 -translate-y-1/2 text-text-tertiary" />
                        <Input
                          className="pl-9"
                          type="number"
                          value={String(form.max_concurrency ?? DEFAULT_ACCOUNT_MAX_CONCURRENCY)}
                          onChange={(e) =>
                            setForm({ ...form, max_concurrency: Number(e.target.value) })
                          }
                        />
                      </div>
                    </HeroTextField>

                    <HeroTextField fullWidth>
                      <Label>{t('accounts.rate_multiplier')}</Label>
                      <Input
                        type="number"
                        min={MIN_POSITIVE_RATE_MULTIPLIER}
                        max={MAX_RATE_MULTIPLIER}
                        step={RATE_MULTIPLIER_STEP}
                        value={rateMultiplierInput}
                        onChange={(e) => setRateMultiplierInput(e.target.value)}
                      />
                    </HeroTextField>

                    <div className="space-y-1.5">
                      <Label>{t('accounts.proxy')}</Label>
                      <SimpleSelect
                        ariaLabel={t('accounts.proxy')}
                      fullWidth
                        items={proxyOptions.map((item) => ({ key: item.id, label: item.label }))}
                      selectedKey={form.proxy_id == null ? '' : String(form.proxy_id)}
                        selectedLabel={selectedProxyLabel}
                      onSelectionChange={(key) =>
                        setForm({
                          ...form,
                          proxy_id: key ? Number(key) : undefined,
                        })
                      }
                      />
                    </div>
                  </div>

                  <div className="ag-account-switch-row">
                    <NativeSwitch
                      className="ag-account-option-switch"
                      isSelected={form.upstream_is_pool ?? false}
                      label={<span className="text-sm text-text">{t('accounts.upstream_is_pool', '池模式')}</span>}
                      onChange={(checked) => setForm({ ...form, upstream_is_pool: checked })}
                    />

                    <NativeSwitch
                      className="ag-account-option-switch"
                      isSelected={getAccountMessageLockEnabled(form.extra)}
                      label={<span className="text-sm text-text">{t('accounts.message_lock')}</span>}
                      onChange={(checked) =>
                        setForm({ ...form, extra: setAccountMessageLockEnabled(form.extra, checked) })
                      }
                    />
                  </div>

                  {availableGroups.length > 0 && (
                    <div className="ag-create-account-groups">
                      <Label>{t('accounts.groups')}</Label>
                      <div className="ag-create-account-group-list">
                        {availableGroups.map((group) => {
                          const selected = groupIds.includes(group.id);
                          return (
                            <div
                              key={group.id}
                              className="ag-create-account-group-item"
                              data-checked={selected ? 'true' : undefined}
                            >
                              <NativeCheckbox
                                className="ag-create-account-group-check"
                                isSelected={selected}
                                onChange={() => toggleGroup(group.id)}
                              >
                                <span className="min-w-0">
                                  <span className="block truncate">{group.name}</span>
                                  <span className="block truncate text-[10px] text-text-tertiary">
                                    {pName(group.platform)}
                                  </span>
                                </span>
                              </NativeCheckbox>
                              <Input
                                aria-label={t('accounts.group_priority')}
                                className="ag-create-account-group-priority-input"
                                disabled={!selected}
                                inputMode="numeric"
                                pattern="-?[0-9]*"
                                placeholder={t('accounts.group_priority_fallback')}
                                type="text"
                                value={groupPriorityInputs[group.id] ?? ''}
                                onBlur={() => commitGroupPriorityChange(group.id)}
                                onChange={(event) => handleGroupPriorityChange(group.id, event.target.value)}
                              />
                            </div>
                          );
                        })}
                      </div>
                    </div>
                  )}
                </section>
      </Form>
    </CommonModal>
  );
}

function extraWithGroupPriorities(
  extra: Record<string, unknown> | undefined,
  groupIds: number[],
  priorityInputs: Record<number, string>,
) {
  const priorities: Record<number, number> = {};
  for (const groupID of groupIds) {
    const priority = parseAccountPriorityInput(priorityInputs[groupID] ?? '');
    if (priority != null) priorities[groupID] = priority;
  }
  return setAccountGroupPriorities(extra, priorities);
}

function omitGroupPriorityInput(values: Record<number, string>, groupID: number) {
  const next = { ...values };
  delete next[groupID];
  return next;
}
