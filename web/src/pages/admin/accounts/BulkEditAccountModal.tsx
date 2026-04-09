import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { Hash, Gauge } from 'lucide-react';
import { Button } from '../../../shared/components/Button';
import { Input, Select } from '../../../shared/components/Input';
import { Switch } from '../../../shared/components/Switch';
import { Modal } from '../../../shared/components/Modal';
import { groupsApi } from '../../../shared/api/groups';
import { proxiesApi } from '../../../shared/api/proxies';
import { queryKeys } from '../../../shared/queryKeys';
import { FETCH_ALL_PARAMS } from '../../../shared/constants';
import { GroupCheckboxList } from './CredentialForm';
import type { BulkUpdateAccountsReq } from '../../../shared/types';

/**
 * 批量编辑弹窗：每个字段前有「启用」开关，只有启用的字段会进入 patch。
 * 分组为追加模式（add_group_ids）：新勾选的分组会并到账号已有分组中，不会移除。
 */
export function BulkEditAccountModal({
  open,
  count,
  onClose,
  onSubmit,
  loading,
}: {
  open: boolean;
  count: number;
  onClose: () => void;
  onSubmit: (data: Omit<BulkUpdateAccountsReq, 'account_ids'>) => void;
  loading: boolean;
}) {
  const { t } = useTranslation();

  // 每个字段独立的「启用」开关
  const [enableStatus, setEnableStatus] = useState(false);
  const [enablePriority, setEnablePriority] = useState(false);
  const [enableConcurrency, setEnableConcurrency] = useState(false);
  const [enableRateMultiplier, setEnableRateMultiplier] = useState(false);
  const [enableGroups, setEnableGroups] = useState(false);
  const [enableProxy, setEnableProxy] = useState(false);

  // 字段值
  const [status, setStatus] = useState<'active' | 'disabled'>('active');
  const [priority, setPriority] = useState(50);
  const [maxConcurrency, setMaxConcurrency] = useState(5);
  const [rateMultiplier, setRateMultiplier] = useState(1);
  const [groupIds, setGroupIds] = useState<number[]>([]);
  const [proxyId, setProxyId] = useState<number | null>(null);

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
    enableConcurrency ||
    enableRateMultiplier ||
    enableGroups ||
    enableProxy;

  const handleSubmit = () => {
    const patch: Omit<BulkUpdateAccountsReq, 'account_ids'> = {};
    if (enableStatus) patch.status = status;
    if (enablePriority) patch.priority = priority;
    if (enableConcurrency) patch.max_concurrency = maxConcurrency;
    if (enableRateMultiplier) patch.rate_multiplier = rateMultiplier;
    if (enableGroups) patch.group_ids = groupIds;
    if (enableProxy && proxyId != null) patch.proxy_id = proxyId;
    onSubmit(patch);
  };

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`${t('accounts.bulk_update_title')} (${count})`}
      width="560px"
      footer={
        <div className="flex justify-end gap-2 w-full">
          <Button variant="secondary" onClick={onClose}>
            {t('common.cancel')}
          </Button>
          <Button onClick={handleSubmit} loading={loading} disabled={!hasAnyField}>
            {t('common.save')}
          </Button>
        </div>
      }
    >
      <div className="space-y-4">
        <div
          className="text-xs px-3 py-2 rounded"
          style={{
            background: 'var(--ag-bg-surface)',
            border: '1px solid var(--ag-glass-border)',
            color: 'var(--ag-text-secondary)',
          }}
        >
          {t('accounts.bulk_update_hint')}
        </div>

        {/* 调度状态 */}
        <FieldRow
          enabled={enableStatus}
          onToggle={setEnableStatus}
          label={t('accounts.enable_dispatch')}
        >
          <Switch
            checked={status === 'active'}
            onChange={(on) => setStatus(on ? 'active' : 'disabled')}
          />
        </FieldRow>

        {/* 优先级 */}
        <FieldRow
          enabled={enablePriority}
          onToggle={setEnablePriority}
          label={t('accounts.priority')}
        >
          <Input
            type="number"
            min={0}
            max={999}
            step={1}
            value={String(priority)}
            disabled={!enablePriority}
            onChange={(e) => {
              const v = Math.round(Number(e.target.value));
              setPriority(Math.max(0, Math.min(999, v)));
            }}
            icon={<Hash className="w-4 h-4" />}
          />
        </FieldRow>

        {/* 并发数 */}
        <FieldRow
          enabled={enableConcurrency}
          onToggle={setEnableConcurrency}
          label={t('accounts.concurrency')}
        >
          <Input
            type="number"
            value={String(maxConcurrency)}
            disabled={!enableConcurrency}
            onChange={(e) => setMaxConcurrency(Number(e.target.value))}
            icon={<Gauge className="w-4 h-4" />}
          />
        </FieldRow>

        {/* 费率倍率 */}
        <FieldRow
          enabled={enableRateMultiplier}
          onToggle={setEnableRateMultiplier}
          label={t('accounts.rate_multiplier')}
        >
          <Input
            type="number"
            step="0.1"
            value={String(rateMultiplier)}
            disabled={!enableRateMultiplier}
            onChange={(e) => setRateMultiplier(Number(e.target.value))}
          />
        </FieldRow>

        {/* 所属分组（直接替换） */}
        <FieldRow
          enabled={enableGroups}
          onToggle={setEnableGroups}
          label={t('accounts.groups')}
        >
          {enableGroups && (
            <GroupCheckboxList
              groups={groupsData?.list ?? []}
              selectedIds={groupIds}
              onChange={setGroupIds}
            />
          )}
        </FieldRow>

        {/* 代理 */}
        <FieldRow
          enabled={enableProxy}
          onToggle={setEnableProxy}
          label={t('accounts.proxy')}
        >
          <Select
            value={proxyId == null ? '' : String(proxyId)}
            disabled={!enableProxy}
            onChange={(e) => setProxyId(e.target.value ? Number(e.target.value) : null)}
            options={[
              { value: '', label: t('accounts.select_proxy') },
              ...(proxiesData?.list ?? []).map((p) => ({
                value: String(p.id),
                label: `${p.name} (${p.protocol}://${p.address}:${p.port})`,
              })),
            ]}
          />
        </FieldRow>
      </div>
    </Modal>
  );
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
  children: React.ReactNode;
}) {
  return (
    <div
      className="flex items-start gap-3 py-2"
      style={{ borderTop: '1px solid var(--ag-border-subtle)' }}
    >
      <label className="flex items-center gap-2 shrink-0 pt-2 cursor-pointer" style={{ minWidth: 120 }}>
        <input
          type="checkbox"
          checked={enabled}
          onChange={(e) => onToggle(e.target.checked)}
          className="w-4 h-4 cursor-pointer"
          style={{ accentColor: 'var(--ag-primary)' }}
        />
        <span
          className="text-sm"
          style={{ color: enabled ? 'var(--ag-text)' : 'var(--ag-text-tertiary)' }}
        >
          {label}
        </span>
      </label>
      <div className="flex-1 min-w-0">{children}</div>
    </div>
  );
}
