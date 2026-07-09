import { useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import {
  Button,
  Form,
  Input,
  TextField as HeroTextField,
  useOverlayState,
} from '@heroui/react';
import { Hash, Gauge, Diff } from 'lucide-react';
import { groupsApi } from '../../../shared/api/groups';
import { proxiesApi } from '../../../shared/api/proxies';
import { queryKeys } from '../../../shared/queryKeys';
import { FETCH_ALL_PARAMS } from '../../../shared/constants';
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
import type { BulkUpdateAccountsReq } from '../../../shared/types';
import {
  ACCOUNT_GROUP_PRIORITIES_EXTRA_KEY,
  ACCOUNT_PRIORITY_MAX,
  ACCOUNT_PRIORITY_MIN,
  commitAccountPriorityOffsetInput,
  commitAccountPriorityInput,
  DEFAULT_ACCOUNT_MAX_CONCURRENCY,
  DEFAULT_ACCOUNT_PRIORITY,
  getAccountPriorityOffsetRange,
  isAccountPriorityDraft,
  parseAccountPriorityOffsetInput,
  parseAccountPriorityInput,
  setAccountMessageLockEnabled,
} from './accountDefaults';

/**
 * 批量编辑弹窗：每个字段前有「启用」开关，只有启用的字段会进入 patch。
 * 分组为整体替换模式：启用后会用当前勾选列表覆盖账号已有分组。
 */
export function BulkEditAccountModal({
  open,
  count,
  initialGroupIds,
  initialGroupPriorities,
  initialMaxConcurrency,
  initialPriority,
  initialPriorityMax,
  initialPriorityMin,
  initialRateMultiplier,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  count: number;
  initialGroupIds?: number[];
  initialGroupPriorities?: Record<number, number>;
  initialMaxConcurrency?: number;
  initialPriority?: number;
  initialPriorityMax?: number;
  initialPriorityMin?: number;
  initialRateMultiplier?: number;
  onClose: () => void;
  onSubmit: (data: Omit<BulkUpdateAccountsReq, 'account_ids'>) => void;
  loading: boolean;
}) {
  const { t } = useTranslation();

  // 每个字段独立的「启用」开关
  const [enableStatus, setEnableStatus] = useState(false);
  const [enablePriority, setEnablePriority] = useState(false);
  const [enablePriorityOffset, setEnablePriorityOffset] = useState(false);
  const [enableConcurrency, setEnableConcurrency] = useState(false);
  const [enableRateMultiplier, setEnableRateMultiplier] = useState(false);
  const [enableGroups, setEnableGroups] = useState(false);
  const [enableProxy, setEnableProxy] = useState(false);
  const [enableMessageLock, setEnableMessageLock] = useState(false);

  // 字段值
  const [status, setStatus] = useState<'active' | 'disabled'>('active');
  const [priority, setPriority] = useState(() => initialPriority ?? DEFAULT_ACCOUNT_PRIORITY);
  const [priorityInput, setPriorityInput] = useState(() => String(initialPriority ?? DEFAULT_ACCOUNT_PRIORITY));
  const [priorityOffsetInput, setPriorityOffsetInput] = useState('');
  const [maxConcurrency, setMaxConcurrency] = useState(() => initialMaxConcurrency ?? DEFAULT_ACCOUNT_MAX_CONCURRENCY);
  const [rateMultiplier, setRateMultiplier] = useState(() => String(initialRateMultiplier ?? 1));
  const [groupIds, setGroupIds] = useState<number[]>(() => [...(initialGroupIds ?? [])]);
  const [groupPriorityInputs, setGroupPriorityInputs] = useState<Record<number, string>>(() => {
    const inputs: Record<number, string> = {};
    for (const [groupID, priority] of Object.entries(initialGroupPriorities ?? {})) {
      inputs[Number(groupID)] = String(priority);
    }
    return inputs;
  });
  const [proxyId, setProxyId] = useState<number | null>(null);
  const [messageLockEnabled, setMessageLockEnabled] = useState(false);

  const { data: groupsData } = useQuery({
    queryKey: queryKeys.groupsAll(),
    queryFn: () => groupsApi.list(FETCH_ALL_PARAMS),
  });

  const { data: proxiesData } = useQuery({
    queryKey: queryKeys.proxiesAll(),
    queryFn: () => proxiesApi.list(FETCH_ALL_PARAMS),
  });

  const hasAnyField =
    enableStatus ||
    enablePriority ||
    enablePriorityOffset ||
    enableConcurrency ||
    enableRateMultiplier ||
    enableGroups ||
    enableProxy ||
    enableMessageLock;
  const parsedRateMultiplier = parseRateMultiplier(rateMultiplier);
  const rateMultiplierEmpty = isEmptyRateMultiplierInput(rateMultiplier);
  const rateMultiplierValid =
    !enableRateMultiplier || rateMultiplierEmpty || isValidRateMultiplierValue(parsedRateMultiplier);
  const priorityOffsetRange = getAccountPriorityOffsetRange(initialPriorityMin, initialPriorityMax);
  const parsedPriorityOffset = parseAccountPriorityOffsetInput(priorityOffsetInput);
  const priorityOffsetValid = !enablePriorityOffset || (
    parsedPriorityOffset != null &&
    parsedPriorityOffset !== 0 &&
    parsedPriorityOffset >= priorityOffsetRange.min &&
    parsedPriorityOffset <= priorityOffsetRange.max
  );
  const canSubmit = hasAnyField && priorityOffsetValid && rateMultiplierValid && (!enableProxy || proxyId != null);

  const handleSubmit = () => {
    if (!canSubmit) return;

    const patch: Omit<BulkUpdateAccountsReq, 'account_ids'> = {};
    if (enableStatus) patch.state = status;
    if (enablePriority) patch.priority = commitAccountPriorityInput(priorityInput, priority);
    if (enablePriorityOffset && parsedPriorityOffset != null) patch.priority_offset = parsedPriorityOffset;
    if (enableConcurrency) patch.max_concurrency = maxConcurrency;
    if (enableRateMultiplier) {
      if (rateMultiplierEmpty) {
        patch.rate_multiplier = null;
      } else if (isValidRateMultiplierValue(parsedRateMultiplier)) {
        patch.rate_multiplier = parsedRateMultiplier;
      }
    }
    if (enableGroups) patch.group_ids = groupIds;
    if (enableProxy && proxyId != null) patch.proxy_id = proxyId;
    let extraPatch: Record<string, unknown> | undefined;
    if (enableGroups) {
      extraPatch = groupPrioritiesExtraPatch(groupIds, groupPriorityInputs);
    }
    if (enableMessageLock) {
      extraPatch = setAccountMessageLockEnabled(extraPatch, messageLockEnabled);
    }
    if (extraPatch) patch.extra = extraPatch;
    onSubmit(patch);
  };
  const proxyOptions = [
    { id: '', label: t('accounts.select_proxy'), endpoint: '' },
    ...(proxiesData?.list ?? []).map((p) => ({
      id: String(p.id),
      label: p.name,
      endpoint: `${p.protocol}://${p.address}:${p.port}`,
    })),
  ];
  const selectedProxyLabel =
    proxyOptions.find((item) => item.id === (proxyId == null ? '' : String(proxyId)))?.label ?? t('accounts.select_proxy');
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) onClose();
    },
  });
  const handlePriorityChange = (value: string) => {
    if (!isAccountPriorityDraft(value)) return;
    setPriorityInput(value);
    const nextPriority = parseAccountPriorityInput(value);
    if (nextPriority != null) {
      setPriority(nextPriority);
    }
  };
  const commitPriorityChange = () => {
    const nextPriority = commitAccountPriorityInput(priorityInput, priority);
    setPriority(nextPriority);
    setPriorityInput(String(nextPriority));
  };
  const handlePriorityToggle = (enabled: boolean) => {
    setEnablePriority(enabled);
    if (enabled) setEnablePriorityOffset(false);
  };
  const handlePriorityOffsetToggle = (enabled: boolean) => {
    setEnablePriorityOffset(enabled);
    if (enabled) setEnablePriority(false);
  };
  const handlePriorityOffsetChange = (value: string) => {
    if (!isAccountPriorityDraft(value)) return;
    setPriorityOffsetInput(value);
  };
  const commitPriorityOffsetChange = () => {
    const nextOffset = commitAccountPriorityOffsetInput(
      priorityOffsetInput,
      priorityOffsetRange.min,
      priorityOffsetRange.max,
    );
    setPriorityOffsetInput(nextOffset == null ? '' : String(nextOffset));
  };
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

  return (
    <CommonModal
      className="ag-account-page-modal"
      dialogStyle={{ maxWidth: '560px', width: 'min(100%, calc(100vw - 2rem))' }}
      footer={(
        <div className="flex w-full justify-end gap-2">
          <Button variant="secondary" onPress={onClose}>
            {t('common.cancel')}
          </Button>
          <Button variant="primary" onPress={handleSubmit} isDisabled={loading || !canSubmit} aria-busy={loading}>
            {t('common.save')}
          </Button>
        </div>
      )}
      size="md"
      state={modalState}
      title={`${t('accounts.bulk_update_title')} (${count})`}
    >
      <Form className="space-y-4" onSubmit={(event) => event.preventDefault()}>
        <p className="rounded-md border border-border bg-surface px-3 py-2 text-xs leading-5 text-text-secondary">
          {t('accounts.bulk_update_hint')}
        </p>

        {/* 调度状态 */}
        <FieldRow
          enabled={enableStatus}
          onToggle={setEnableStatus}
          label={t('accounts.enable_dispatch')}
        >
          <NativeSwitch
            isDisabled={!enableStatus}
            isSelected={status === 'active'}
            label={(
              <span className={enableStatus ? 'text-sm text-text' : 'text-sm text-text-tertiary'}>
                {status === 'active' ? t('common.enabled', '已启用') : t('common.disabled', '已禁用')}
              </span>
            )}
            onChange={(on) => setStatus(on ? 'active' : 'disabled')}
          />
        </FieldRow>

        {/* 优先级 */}
        <FieldRow
          enabled={enablePriority}
          onToggle={handlePriorityToggle}
          label={t('accounts.priority')}
        >
          <HeroTextField fullWidth isDisabled={!enablePriority}>
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
                disabled={!enablePriority}
                onBlur={commitPriorityChange}
                onChange={(e) => handlePriorityChange(e.target.value)}
              />
            </div>
          </HeroTextField>
        </FieldRow>

        {/* 优先级偏移 */}
        <FieldRow
          enabled={enablePriorityOffset}
          onToggle={handlePriorityOffsetToggle}
          label={t('accounts.priority_offset')}
        >
          <div>
            <HeroTextField fullWidth isDisabled={!enablePriorityOffset}>
              <div className="relative">
                <Diff className="pointer-events-none absolute left-3 top-1/2 z-10 w-4 h-4 -translate-y-1/2 text-text-tertiary" />
                <Input
                  className="pl-9"
                  type="text"
                  inputMode="numeric"
                  pattern="-?[0-9]*"
                  min={priorityOffsetRange.min}
                  max={priorityOffsetRange.max}
                  step={1}
                  value={priorityOffsetInput}
                  disabled={!enablePriorityOffset}
                  placeholder={t('accounts.priority_offset_placeholder')}
                  onBlur={commitPriorityOffsetChange}
                  onChange={(event) => handlePriorityOffsetChange(event.target.value)}
                />
              </div>
            </HeroTextField>
            <p className="mt-1 text-[11px] leading-4 text-text-tertiary">
              {t('accounts.priority_offset_hint', {
                min: priorityOffsetRange.min,
                max: priorityOffsetRange.max,
              })}
            </p>
          </div>
        </FieldRow>

        {/* 并发数 */}
        <FieldRow
          enabled={enableConcurrency}
          onToggle={setEnableConcurrency}
          label={t('accounts.concurrency')}
        >
          <HeroTextField fullWidth isDisabled={!enableConcurrency}>
            <div className="relative">
              <Gauge className="pointer-events-none absolute left-3 top-1/2 z-10 w-4 h-4 -translate-y-1/2 text-text-tertiary" />
              <Input
                className="pl-9"
                type="number"
                value={String(maxConcurrency)}
                disabled={!enableConcurrency}
                onChange={(e) => setMaxConcurrency(Number(e.target.value))}
              />
            </div>
          </HeroTextField>
        </FieldRow>

        {/* 费率倍率 */}
        <FieldRow
          enabled={enableRateMultiplier}
          onToggle={setEnableRateMultiplier}
          label={t('accounts.rate_multiplier')}
        >
          <HeroTextField fullWidth isDisabled={!enableRateMultiplier}>
            <Input
              type="number"
              min={MIN_POSITIVE_RATE_MULTIPLIER}
              max={MAX_RATE_MULTIPLIER}
              step={RATE_MULTIPLIER_STEP}
              value={rateMultiplier}
              disabled={!enableRateMultiplier}
              onChange={(e) => setRateMultiplier(e.target.value)}
            />
          </HeroTextField>
        </FieldRow>

        {/* 所属分组（直接替换） */}
        <FieldRow
          enabled={enableGroups}
          onToggle={setEnableGroups}
          label={t('accounts.groups')}
        >
          <div className="ag-create-account-group-list ag-bulk-account-group-list">
            {(groupsData?.list ?? []).map((group) => {
              const selected = groupIds.includes(group.id);
              return (
                <div
                  key={group.id}
                  className="ag-create-account-group-item"
                  data-checked={selected ? 'true' : undefined}
                  data-disabled={!enableGroups ? 'true' : undefined}
                >
                  <NativeCheckbox
                    className="ag-create-account-group-check"
                    isDisabled={!enableGroups}
                    isSelected={selected}
                    onChange={() => toggleGroup(group.id)}
                  >
                    <span className="min-w-0">
                      <span className="block truncate">{group.name}</span>
                      <span className="block truncate text-[10px] text-text-tertiary">
                        {group.platform}
                      </span>
                    </span>
                  </NativeCheckbox>
                  <Input
                    aria-label={t('accounts.group_priority')}
                    className="ag-create-account-group-priority-input"
                    disabled={!enableGroups || !selected}
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
        </FieldRow>

        {/* 代理 */}
        <FieldRow
          enabled={enableProxy}
          onToggle={setEnableProxy}
          label={t('accounts.proxy')}
        >
          <SimpleSelect
            fullWidth
            ariaLabel={t('accounts.proxy')}
            items={proxyOptions.map((item) => ({
              key: item.id,
              label: item.label,
              description: item.endpoint,
            }))}
            selectedKey={proxyId == null ? '' : String(proxyId)}
            isDisabled={!enableProxy}
            selectedLabel={<span className="block min-w-0 truncate">{selectedProxyLabel}</span>}
            onSelectionChange={(key) => setProxyId(key === '' ? null : Number(key))}
          />
        </FieldRow>

        <FieldRow
          enabled={enableMessageLock}
          onToggle={setEnableMessageLock}
          label={t('accounts.message_lock')}
        >
          <NativeSwitch
            isDisabled={!enableMessageLock}
            isSelected={messageLockEnabled}
            label={(
              <span className={enableMessageLock ? 'text-sm text-text' : 'text-sm text-text-tertiary'}>
                {messageLockEnabled ? t('common.enabled', '已启用') : t('common.disabled', '已禁用')}
              </span>
            )}
            onChange={setMessageLockEnabled}
          />
        </FieldRow>
      </Form>
    </CommonModal>
  );
}

function groupPrioritiesExtraPatch(groupIds: number[], priorityInputs: Record<number, string>) {
  const priorities: Record<string, number> = {};
  for (const groupID of groupIds) {
    const priority = parseAccountPriorityInput(priorityInputs[groupID] ?? '');
    if (priority != null) priorities[String(groupID)] = priority;
  }
  return { [ACCOUNT_GROUP_PRIORITIES_EXTRA_KEY]: priorities };
}

function omitGroupPriorityInput(values: Record<number, string>, groupID: number) {
  const next = { ...values };
  delete next[groupID];
  return next;
}

function FieldRow({
  enabled,
  onToggle,
  label,
  children,
}: {
  enabled: boolean;
  onToggle: (on: boolean) => void;
  label: string;
  children: ReactNode;
}) {
  return (
    <div className="grid items-center gap-3 border-t border-border-subtle pt-4 sm:grid-cols-[10rem_minmax(0,1fr)]">
      <NativeCheckbox
        className="self-center"
        isSelected={enabled}
        onChange={onToggle}
      >
        <span className={enabled ? 'text-sm text-text' : 'text-sm text-text-tertiary'}>
          {label}
        </span>
      </NativeCheckbox>
      <div className="min-w-0">{children}</div>
    </div>
  );
}
