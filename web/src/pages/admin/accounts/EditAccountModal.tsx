import { useEffect, useRef, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import {
  Button,
  Form,
  Input,
  Label,
  TextField as HeroTextField,
  useOverlayState,
} from '@heroui/react';
import { Gauge, Hash, Layers } from 'lucide-react';
import { accountsApi } from '../../../shared/api/accounts';
import { groupsApi } from '../../../shared/api/groups';
import { proxiesApi } from '../../../shared/api/proxies';
import { usePlatforms } from '../../../shared/hooks/usePlatforms';
import { queryKeys } from '../../../shared/queryKeys';
import { FETCH_ALL_PARAMS } from '../../../shared/constants';
import {
  usePluginAccountForm,
  createPluginOAuthBridge,
  detectCredentialAccountType,
  getSchemaSelectedAccountType,
  getSchemaVisibleFields,
  filterCredentialsForAccountType,
  syncAccountIdentity,
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
import type { AccountResp, UpdateAccountReq } from '../../../shared/types';
import {
  ACCOUNT_PRIORITY_MAX,
  ACCOUNT_PRIORITY_MIN,
  commitAccountPriorityInput,
  DEFAULT_ACCOUNT_MAX_CONCURRENCY,
  DEFAULT_ACCOUNT_PRIORITY,
  getAccountGroupPriorities,
  getAccountMessageLockEnabled,
  isAccountPriorityDraft,
  parseAccountPriorityInput,
  setAccountGroupPriorities,
  setAccountMessageLockEnabled,
} from './accountDefaults';

export function EditAccountModal({
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
  const { platformName: pName } = usePlatforms();
  const initialAccountType = account.type || detectCredentialAccountType(account.credentials);
  const initialDispatchEnabled = account.state !== 'disabled';
  const [accountType, setAccountType] = useState(initialAccountType);
  const [form, setForm] = useState<UpdateAccountReq>({
    name: account.name,
    email: account.email,
    type: initialAccountType || undefined,
    priority: account.priority,
    max_concurrency: account.max_concurrency,
    rate_multiplier: account.rate_multiplier,
    upstream_is_pool: account.upstream_is_pool,
    proxy_id: account.proxy_id,
    extra: account.extra ?? {},
  });
  const origCredentials = useRef(account.credentials);
  const [credentials, setCredentials] = useState<Record<string, string>>(account.credentials);
  const [groupIds, setGroupIds] = useState<number[]>(account.group_ids ?? []);
  const [groupPriorityInputs, setGroupPriorityInputs] = useState<Record<number, string>>(() => {
    const priorities = getAccountGroupPriorities(account.extra);
    const inputs: Record<number, string> = {};
    for (const [groupID, priority] of Object.entries(priorities)) {
      inputs[Number(groupID)] = String(priority);
    }
    return inputs;
  });
  const [dispatchEnabled, setDispatchEnabled] = useState(initialDispatchEnabled);
  const [priorityInput, setPriorityInput] = useState(String(account.priority ?? DEFAULT_ACCOUNT_PRIORITY));
  const [rateMultiplierInput, setRateMultiplierInput] = useState(String(account.rate_multiplier ?? 1));

  const { data: schema } = useQuery({
    queryKey: queryKeys.credentialsSchema(account.platform),
    queryFn: () => accountsApi.credentialsSchema(account.platform),
  });

  const { data: groupsData } = useQuery({
    queryKey: queryKeys.groupsAll(),
    queryFn: () => groupsApi.list(FETCH_ALL_PARAMS),
  });

  const { data: proxiesData } = useQuery({
    queryKey: queryKeys.proxiesAll(),
    queryFn: () => proxiesApi.list(FETCH_ALL_PARAMS),
  });

  const { Form: PluginAccountForm, pluginId } = usePluginAccountForm(account.platform, 'edit');
  const pluginOAuth = createPluginOAuthBridge(pluginId);
  const passwordFieldsCleared = useRef(false);

  useEffect(() => {
    // 插件有自定义表单时，由插件自己控制脱敏展示，不清空 password 字段
    if (PluginAccountForm || !schema || passwordFieldsCleared.current) return;
    const passwordKeys = getSchemaVisibleFields(schema, accountType)
      .filter((field) => field.type === 'password')
      .map((field) => field.key);
    if (passwordKeys.length === 0) return;

    passwordFieldsCleared.current = true;
    setCredentials((prev) => {
      const next = { ...prev };
      for (const key of passwordKeys) next[key] = '';
      return next;
    });
  }, [schema, accountType, PluginAccountForm]);

  useEffect(() => {
    const selectedType = getSchemaSelectedAccountType(schema, accountType);
    if (!selectedType || selectedType.key === accountType) return;
    setAccountType(selectedType.key);
    setForm((prev) => ({ ...prev, type: selectedType.key || undefined }));
  }, [schema, accountType]);

  const handleAccountTypeChange = (type: string) => {
    setAccountType(type);
    setForm((prev) => ({ ...prev, type: type || undefined }));
  };

  const handleCredentialsChange = (next: Record<string, string>) => {
    setCredentials(next);
  };

  const handleSchemaAccountTypeChange = (type: string) => {
    const selectedType = getSchemaSelectedAccountType(schema, type);
    handleAccountTypeChange(type);
    setCredentials((prev) => filterCredentialsForAccountType(prev, selectedType));
  };

  const handleSubmit = () => {
    const priority = commitAccountPriorityInput(priorityInput, form.priority ?? DEFAULT_ACCOUNT_PRIORITY);
    const rateMultiplierValue = parseRateMultiplier(rateMultiplierInput);
    const rateMultiplierEmpty = isEmptyRateMultiplierInput(rateMultiplierInput);
    if (!rateMultiplierEmpty && !isValidRateMultiplierValue(rateMultiplierValue)) return;
    const rateMultiplier = rateMultiplierEmpty ? null : rateMultiplierValue;
    const merged = { ...credentials };
    const passwordKeys = new Set(
      getSchemaVisibleFields(schema, accountType)
        .filter((field) => field.type === 'password')
        .map((field) => field.key),
    );

    for (const [key, value] of Object.entries(origCredentials.current)) {
      if (passwordKeys.has(key) && merged[key] === '' && value) merged[key] = value;
    }

    const nextState = dispatchEnabled === initialDispatchEnabled
      ? undefined
      : dispatchEnabled ? 'active' : 'disabled';

    const identity = syncAccountIdentity(merged, form.email);

    onSubmit({
      ...form,
      email: identity.email,
      ...(nextState ? { state: nextState } : {}),
      priority,
      rate_multiplier: rateMultiplier,
      type: accountType || undefined,
      credentials: identity.credentials,
      extra: extraWithGroupPriorities(form.extra, groupIds, groupPriorityInputs),
      group_ids: groupIds,
    });
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

  const proxyOptions = [
    { id: '', label: t('accounts.no_proxy') },
    ...(proxiesData?.list ?? []).map((proxy) => ({
      id: String(proxy.id),
      label: `${proxy.name} (${proxy.protocol}://${proxy.address}:${proxy.port})`,
    })),
  ];
  const selectedProxyLabel =
    proxyOptions.find((item) => item.id === (form.proxy_id == null ? '' : String(form.proxy_id)))
      ?.label ?? t('accounts.no_proxy');
  const rateMultiplierValid =
    isEmptyRateMultiplierInput(rateMultiplierInput) ||
    isValidRateMultiplierValue(parseRateMultiplier(rateMultiplierInput));
  const availableGroups = (groupsData?.list ?? []).filter(
    (group) => group.platform === account.platform,
  );

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
      if (!nextOpen) onClose();
    },
  });

  return (
    <CommonModal
      className="ag-account-page-modal ag-create-account-modal"
      footer={(
        <div className="flex w-full justify-end gap-2">
          <Button variant="secondary" onPress={onClose}>
            {t('common.cancel')}
          </Button>
          <Button
            variant="primary"
            onPress={handleSubmit}
            isDisabled={loading || !form.name || !rateMultiplierValid}
            aria-busy={loading}
          >
            {t('common.save')}
          </Button>
        </div>
      )}
      icon={<Layers className="size-5" />}
      size="lg"
      state={modalState}
      title={t('accounts.edit')}
    >
              <Form
                className="ag-form-scroll-safe ag-create-account-form"
                onSubmit={(event) => event.preventDefault()}
              >
                <section className="space-y-4">
                  <div className="grid gap-4 md:grid-cols-2">
                    <HeroTextField fullWidth isDisabled>
                      <Label>{t('accounts.platform')}</Label>
                      <Input name="platform" value={pName(account.platform)} disabled />
                    </HeroTextField>

                    <HeroTextField fullWidth isRequired>
                      <Label>{t('common.name')}</Label>
                      <div className="relative">
                        <Layers className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-text-tertiary" />
                        <Input
                          className="pl-9"
                          name="name"
                          autoComplete="off"
                          value={form.name ?? ''}
                          onChange={(event) => setForm({ ...form, name: event.target.value })}
                          required
                        />
                      </div>
                    </HeroTextField>

                  </div>
                </section>

                {PluginAccountForm ? (
                  <section className="ag-plugin-scope border-t border-border pt-4">
                    <PluginAccountForm
                      credentials={credentials}
                      onChange={handleCredentialsChange}
                      mode="edit"
                      accountType={accountType}
                      onAccountTypeChange={handleAccountTypeChange}
                      oauth={pluginOAuth}
                    />
                  </section>
                ) : schema && getSchemaVisibleFields(schema, accountType).length > 0 ? (
                  <SchemaCredentialsForm
                    schema={schema}
                    accountType={accountType}
                    onAccountTypeChange={handleSchemaAccountTypeChange}
                    credentials={credentials}
                    onCredentialsChange={handleCredentialsChange}
                    mode="edit"
                  />
                ) : null}

                <section className="ag-create-account-advanced space-y-4">
                  <NativeSwitch
                    isSelected={dispatchEnabled}
                    label={<span className="text-sm text-text">{t('accounts.enable_dispatch')}</span>}
                    onChange={setDispatchEnabled}
                  />

                  <div className="grid gap-4 md:grid-cols-2">
                    <HeroTextField fullWidth>
                      <Label>{t('accounts.priority_hint')}</Label>
                      <div className="relative">
                        <Hash className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-text-tertiary" />
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
                          onChange={(event) => handlePriorityChange(event.target.value)}
                        />
                      </div>
                    </HeroTextField>

                    <HeroTextField fullWidth>
                      <Label>{t('accounts.concurrency')}</Label>
                      <div className="relative">
                        <Gauge className="pointer-events-none absolute left-3 top-1/2 z-10 h-4 w-4 -translate-y-1/2 text-text-tertiary" />
                        <Input
                          className="pl-9"
                          type="number"
                          value={String(form.max_concurrency ?? DEFAULT_ACCOUNT_MAX_CONCURRENCY)}
                          onChange={(event) =>
                            setForm({ ...form, max_concurrency: Number(event.target.value) })
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
                        onChange={(event) => setRateMultiplierInput(event.target.value)}
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
                          proxy_id: key ? Number(key) : null,
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
