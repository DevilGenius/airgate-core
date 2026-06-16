import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { apikeysApi } from '../../shared/api/apikeys';
import { useUrlPagination } from '../../shared/hooks/useUrlTableState';
import { groupsApi } from '../../shared/api/groups';
import { useToast } from '../../shared/ui';
import { Alert, AlertDialog, Button, EmptyState, Modal, Spinner, useOverlayState } from '@heroui/react';
import { DialogTriggerShim } from '../../shared/components/DialogTriggerShim';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { queryKeys } from '../../shared/queryKeys';
import { DEFAULT_PAGE_SIZE, FETCH_ALL_PARAMS } from '../../shared/constants';
import { getTotalPages } from '../../shared/utils/pagination';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import { CommonTable } from '../../shared/components/CommonTable';
import { TablePage } from '../../shared/components/TablePage';
import { MetricChips } from '../../shared/components/MetricChips';
import { NativeStatusChip } from '../../shared/components/NativeStatusChip';
import { TableRowActionButton } from '../../shared/components/TableRowActionButton';
import { TableRowMoreMenu } from '../../shared/components/TableRowMoreMenu';
import { dateInputToLocalStartRFC3339, formatAPIKeyHint, formatDateInputValue, formatExpiry } from '../../shared/utils/format';
import { useClipboard } from '../../shared/hooks/useClipboard';
import { useCopyFeedback } from '../../shared/hooks/useCopyFeedback';
import {
  formatRateMultiplier,
  isValidRateMultiplierValue,
  isValidSellRateValue,
  parseRateMultiplier,
} from '../../shared/utils/rateMultiplier';
import {
  AlertTriangle,
  Check,
  Copy,
  Plus,
  RefreshCw,
} from 'lucide-react';
import type { APIKeyResp, CreateAPIKeyReq, UpdateAPIKeyReq, GroupResp } from '../../shared/types';
import { useAuth } from '../../app/providers/AuthProvider';
import { EditKeyModal } from './userkeys/EditKeyModal';
import { CreateKeyModal } from './userkeys/CreateKeyModal';
import { UseKeyModal, useUseKeyModal } from './userkeys/UseKeyModal';
import { CcsImportModal, useCcsImportModal } from './userkeys/CcsImportModal';
import { type KeyForm, emptyForm } from './userkeys/types';

const API_KEY_AMOUNT_DECIMALS = 3;

function formatRateValue(rate: number | null | undefined) {
  return formatRateMultiplier(rate);
}

export default function UserKeysPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const copy = useClipboard();
  const queryClient = useQueryClient();
  const { user } = useAuth();

  const { page, setPage, pageSize, setPageSize } = useUrlPagination(DEFAULT_PAGE_SIZE, 'user.keys');
  const [modalOpen, setModalOpen] = useState(false);
  const [editingKey, setEditingKey] = useState<APIKeyResp | null>(null);
  const [form, setForm] = useState<KeyForm>(emptyForm);
  const [deleteTarget, setDeleteTarget] = useState<APIKeyResp | null>(null);

  // 显示新创建密钥的弹窗
  const [createdKey, setCreatedKey] = useState<string | null>(null);
  const [revealedKey, setRevealedKey] = useState<string | null>(null);
  const {
    copied: revealedKeyCopied,
    showCopied: showRevealedKeyCopied,
    resetCopied: resetRevealedKeyCopied,
  } = useCopyFeedback();

  // 密钥列表
  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: queryKeys.userKeys(page, pageSize),
    queryFn: () => apikeysApi.list({ page, page_size: pageSize }),
    placeholderData: keepPreviousData,
  });

  // 分组列表（用于选择）
  const { data: groupsData } = useQuery({
    queryKey: queryKeys.groupsForKeys(),
    queryFn: () => groupsApi.listAvailable(FETCH_ALL_PARAMS),
  });

  // 创建密钥
  const createMutation = useCrudMutation<{ key?: string }, CreateAPIKeyReq>({
    mutationFn: (data) => apikeysApi.create(data),
    successMessage: t('user_keys.create_success'),
    queryKey: queryKeys.userKeys(),
    onSuccess: (result) => {
      closeModal();
      // 显示完整密钥
      if (result.key) {
        setCreatedKey(result.key);
      }
    },
  });

  // 更新密钥
  const updateMutation = useCrudMutation<unknown, { id: number; data: UpdateAPIKeyReq }>({
    mutationFn: ({ id, data }) => apikeysApi.update(id, data),
    successMessage: t('user_keys.update_success'),
    queryKey: queryKeys.userKeys(),
    onSuccess: () => closeModal(),
  });

  // 删除密钥
  const deleteMutation = useCrudMutation<unknown, number>({
    mutationFn: (id) => apikeysApi.delete(id),
    successMessage: t('user_keys.delete_success'),
    queryKey: queryKeys.userKeys(),
    onSuccess: () => setDeleteTarget(null),
  });

  // 查看密钥
  const revealMutation = useMutation({
    mutationFn: (id: number) => apikeysApi.reveal(id),
    onSuccess: (resp) => {
      if (resp.key) {
        setRevealedKey(resp.key);
      }
    },
    onError: (err: Error) => toast('error', err.message),
  });

  // 禁用/启用密钥（动态成功消息，无法使用 useCrudMutation）
  const toggleStatusMutation = useMutation({
    mutationFn: ({ id, status }: { id: number; status: 'active' | 'disabled' }) =>
      apikeysApi.update(id, { status }),
    onSuccess: (_resp, variables) => {
      toast(
        'success',
        variables.status === 'active'
          ? t('user_keys.enable_success')
          : t('user_keys.disable_success'),
      );
      queryClient.invalidateQueries({ queryKey: queryKeys.userKeys() });
    },
    onError: (err: Error) => toast('error', err.message),
  });

  function openCreate() {
    if (!hasAvailableGroups) {
      toast('error', t('user_keys.no_groups_available'));
      return;
    }
    setEditingKey(null);
    setForm(emptyForm);
    setModalOpen(true);
  }

  function openEdit(key: APIKeyResp) {
    setEditingKey(key);
    setForm({
      name: key.name,
      group_id: key.group_id == null ? '' : String(key.group_id),
      quota_usd: key.quota_usd ? String(key.quota_usd) : '',
      sell_rate: Number.isFinite(key.sell_rate) ? String(key.sell_rate) : '1',
      max_concurrency: key.max_concurrency ? String(key.max_concurrency) : '',
      balance_alert_enabled: key.balance_alert_enabled,
      balance_alert_email: key.balance_alert_email || '',
      balance_alert_threshold: key.balance_alert_threshold ? String(key.balance_alert_threshold) : '',
      expires_at: formatDateInputValue(key.expires_at),
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
      toast('error', t('user_keys.name_placeholder'));
      return;
    }
    if (!editingKey && !form.group_id) {
      toast('error', t('user_keys.select_group'));
      return;
    }

    // 后端要求 RFC3339 格式；空字符串表示显式清除过期时间
    const expiresAt = dateInputToLocalStartRFC3339(form.expires_at);
    const sellRate = form.sell_rate.trim() ? parseRateMultiplier(form.sell_rate) : 1;
    if (!isValidSellRateValue(sellRate)) {
      toast('error', t('user_keys.sell_rate_invalid', '销售倍率必须为 0，或在 0.01 到 100 之间'));
      return;
    }
    const normalizedSellRate = sellRate ?? 1;
    const balanceAlertEmail = form.balance_alert_email.trim();
    const balanceAlertThreshold = form.balance_alert_threshold.trim()
      ? Number(form.balance_alert_threshold)
      : 0;
    if (form.balance_alert_enabled && !balanceAlertEmail) {
      toast('error', t('user_keys.balance_alert_email_required'));
      return;
    }
    if (form.balance_alert_enabled && (!Number.isFinite(balanceAlertThreshold) || balanceAlertThreshold <= 0)) {
      toast('error', t('user_keys.balance_alert_threshold_required'));
      return;
    }

    if (editingKey) {
      const payload: UpdateAPIKeyReq = {
        name: form.name,
        group_id: form.group_id ? Number(form.group_id) : undefined,
        // 空字符串显式改为 0 = 无限配额；省略字段只表示不修改旧配额
        quota_usd: form.quota_usd.trim() ? Number(form.quota_usd) : 0,
        sell_rate: normalizedSellRate,
        // 空字符串显式改为 0 = 关闭并发限制；后端看到 0 会清除旧值
        max_concurrency: form.max_concurrency ? Number(form.max_concurrency) : 0,
        balance_alert_enabled: form.balance_alert_enabled,
        balance_alert_email: balanceAlertEmail,
        balance_alert_threshold: balanceAlertThreshold,
        expires_at: expiresAt,
      };
      updateMutation.mutate({ id: editingKey.id, data: payload });
    } else {
      const payload: CreateAPIKeyReq = {
        name: form.name,
        group_id: Number(form.group_id),
        quota_usd: form.quota_usd ? Number(form.quota_usd) : undefined,
        sell_rate: normalizedSellRate,
        max_concurrency: form.max_concurrency ? Number(form.max_concurrency) : undefined,
        balance_alert_enabled: form.balance_alert_enabled,
        balance_alert_email: balanceAlertEmail,
        balance_alert_threshold: balanceAlertThreshold,
        expires_at: expiresAt,
      };
      createMutation.mutate(payload);
    }
  }

  // 查找分组
  const groupList = useMemo(() => groupsData?.list ?? [], [groupsData?.list]);
  const groupMap = useMemo(() => new Map<number, GroupResp>(groupList.map((g) => [g.id, g])), [groupList]);

  const hasAvailableGroups = groupList.length > 0;

  // 分组选项（如果用户有专属倍率，右侧显示划线原价 + 专属倍率）
  const userGroupRates = user?.group_rates;
  const groupOptions = useMemo(() => groupList.map((g) => {
    const override = userGroupRates?.[g.id];
    const hasOverride = isValidRateMultiplierValue(override ?? null) && override !== g.rate_multiplier;
    return {
      value: String(g.id),
      label: g.name,
      suffix: hasOverride ? (
        <span className="text-text-tertiary">
          <span className="line-through opacity-60">{formatRateValue(g.rate_multiplier)}x</span>{' '}
          <span className="text-primary font-medium">{formatRateValue(override)}x</span>
        </span>
      ) : (
        <span className="text-text-tertiary">{formatRateValue(g.rate_multiplier)}x {t('user_keys.rate_suffix', '倍率')}</span>
      ),
    };
  }), [groupList, t, userGroupRates]);

  // 使用配置弹窗
  const {
    useKeyTarget,
    useKeyValue,
    useKeyTab,
    setUseKeyTab,
    useKeyShell,
    setUseKeyShell,
    useKeyPlatform,
    showClientTabs,
    openUseKeyModal,
    closeUseKeyModal,
  } = useUseKeyModal(groupMap);

  // CCS 导入弹窗
  const {
    ccsTarget,
    ccsKeyValue,
    ccsPlatform,
    openCcsModal,
    closeCcsModal,
  } = useCcsImportModal(groupMap);

  const saving = createMutation.isPending || updateMutation.isPending;
  const rows = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const closeRevealedKeyModal = () => {
    resetRevealedKeyCopied();
    setRevealedKey(null);
  };
  const handleCopyRevealedKey = async () => {
    if (await copy(revealedKey || '')) {
      showRevealedKeyCopied();
    }
  };
  const revealedKeyModalState = useOverlayState({
    isOpen: !!revealedKey,
    onOpenChange: (open) => {
      if (!open) closeRevealedKeyModal();
    },
  });

  return (
    <TablePage
      className="ag-api-keys-page"
      actions={(
        <>
          <Button
            isIconOnly
            aria-label={t('common.refresh', 'Refresh')}
            className="ag-page-toolbar-button"
            size="md"
            variant="ghost"
            onPress={() => refetch()}
          >
            <RefreshCw className="w-4 h-4" />
          </Button>
          <Button
            className="ag-page-toolbar-button"
            isDisabled={!hasAvailableGroups}
            variant="primary"
            onPress={openCreate}
          >
            <Plus className="w-4 h-4" />
            {hasAvailableGroups ? t('user_keys.create') : t('user_keys.create_disabled_no_groups')}
          </Button>
        </>
      )}
      footer={(
        <TablePaginationFooter
          page={page}
          pageSize={pageSize}
          setPage={setPage}
          setPageSize={setPageSize}
          total={total}
          totalPages={totalPages}
        />
      )}
      isFetching={isFetching && !isLoading}
    >

      <CommonTable
        ariaLabel={t('user_keys.title', 'API keys')}
        className="ag-api-keys-table"
        contentStyle={{ width: '100%' }}
      >
        <CommonTable.Header>
          <CommonTable.Column id="name" style={{ minWidth: '10.5rem', width: '10.5rem' }}>{t('common.name')}</CommonTable.Column>
          <CommonTable.Column id="key_prefix" style={{ minWidth: '10rem', width: '10rem' }}>{t('user_keys.key_table_header', '密钥')}</CommonTable.Column>
          <CommonTable.Column id="group_id" style={{ width: '15rem' }}>{t('user_keys.group')}</CommonTable.Column>
          <CommonTable.Column id="status" style={{ width: '5.5rem' }}>{t('common.status')}</CommonTable.Column>
          <CommonTable.Column id="quota" style={{ width: '18.5rem' }}>{t('user_keys.quota_table_header', '配额')}</CommonTable.Column>
          <CommonTable.Column id="markup" style={{ width: '10.75rem' }}>{t('user_keys.markup_title', '成本/利润')}</CommonTable.Column>
          <CommonTable.Column id="usage" style={{ width: '20.875rem' }}>{t('api_keys.usage_window', '用量(今日/30天)')}</CommonTable.Column>
          <CommonTable.Column id="expires_at" style={{ width: '7rem' }}>{t('user_keys.expires_at')}</CommonTable.Column>
          <CommonTable.Column id="actions" style={{ width: 132 }}>
            {t('common.actions')}
          </CommonTable.Column>
        </CommonTable.Header>
        <CommonTable.Body>
          {isLoading ? (
            <TableLoadingRow colSpan={9} />
          ) : rows.length === 0 ? (
            <CommonTable.Row id="empty">
              <CommonTable.Cell colSpan={9}>
                <EmptyState>
                  <div className="text-sm text-default-500">{t('common.no_data')}</div>
                </EmptyState>
              </CommonTable.Cell>
            </CommonTable.Row>
          ) : (
            rows.map((row) => {
              const group = row.group_id == null ? null : groupMap.get(row.group_id);
              const isGroupUnbound = row.group_id == null;
              const groupName = isGroupUnbound
                ? t('user_keys.group_unbound')
                : group?.name || `#${row.group_id}`;
              const sellRate = isValidSellRateValue(row.sell_rate ?? null) && row.sell_rate != null ? row.sell_rate : 1;
              const hasSellRate = sellRate !== 1;
              const userOverride = row.group_id == null ? undefined : user?.group_rates?.[row.group_id];
              const hasUserOverride =
                isValidRateMultiplierValue(userOverride ?? null);
              const responseGroupRate =
                isValidRateMultiplierValue(row.group_rate ?? null)
                  ? row.group_rate
                  : undefined;
              const groupRate = responseGroupRate ?? (hasUserOverride ? userOverride : group?.rate_multiplier);
              const normalizedGroupRate = isValidRateMultiplierValue(groupRate ?? null) ? groupRate : undefined;
              const hasGroupRate = normalizedGroupRate != null;
              const effectiveRate = hasGroupRate ? normalizedGroupRate * sellRate : undefined;
              const profit = (row.used_quota || 0) - (row.used_quota_actual || 0);
              const isExpired = row.expires_at && new Date(row.expires_at) < new Date();
              const displayStatus = isExpired ? 'expired' : row.status;
              const keyHint = formatAPIKeyHint(row.key_prefix);

              return (
                <CommonTable.Row id={String(row.id)} key={row.id}>
                  <CommonTable.Cell>
                    <span className="block max-w-[10rem] truncate font-medium text-text" title={row.name}>{row.name}</span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <span className="ag-api-key-prefix-chip inline-flex items-center text-xs px-2 py-0.5 rounded-sm border border-glass-border bg-surface text-text-secondary font-mono">
                      <span className="ag-api-key-prefix-text" title={keyHint}>
                        {keyHint}
                      </span>
                    </span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <div className="ag-api-key-group-stack">
                      <div className="ag-api-key-group-line">
                        <span
                          className="ag-api-key-group-name-chip inline-flex h-6 min-w-0 max-w-full items-center justify-center gap-1 rounded-[var(--radius)] px-1.5 text-[13px] font-medium leading-none text-text-secondary"
                          data-tone={isGroupUnbound ? 'warning' : 'default'}
                          title={groupName}
                        >
                          <span className="min-w-0 truncate">{groupName}</span>
                        </span>
                        {hasGroupRate ? (
                          <span
                            className="ag-api-key-effective-rate-chip"
                            title={`${t('user_keys.effective_rate_short', '综合倍率')} ${formatRateValue(effectiveRate)}`}
                          >
                            <span className="ag-api-key-effective-rate-chip-value">{formatRateValue(effectiveRate)}</span>
                          </span>
                        ) : null}
                      </div>
                      {(hasGroupRate || hasSellRate) && (
                        <div
                          className="ag-api-key-rate-row"
                          title={`${t('user_keys.group_sell_rate_short', '分组倍率x销售倍率')} ${formatRateValue(normalizedGroupRate)}x${formatRateValue(sellRate)}`}
                        >
                          <span className="ag-api-key-rate-label">
                            {t('user_keys.group_sell_rate_short', '分组倍率x销售倍率')}
                          </span>
                          <span className="ag-api-key-rate-value">
                            {formatRateValue(normalizedGroupRate)}x{formatRateValue(sellRate)}
                          </span>
                        </div>
                      )}
                    </div>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <NativeStatusChip status={displayStatus} />
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <MetricChips
                      className="ag-metric-chips--quota"
                      items={[
                        {
                          amount: row.used_quota,
                          color: 'warning',
                          decimals: API_KEY_AMOUNT_DECIMALS,
                          highlightDollar: true,
                          label: t('user_keys.quota_used_short', '使用'),
                        },
                        {
                          amount: row.quota_usd > 0 ? row.quota_usd : undefined,
                          color: 'success',
                          decimals: API_KEY_AMOUNT_DECIMALS,
                          label: t('user_keys.quota_total_short', '配额'),
                          value: '∞',
                        },
                      ]}
                    />
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <MetricChips
                      className="ag-metric-chips--stack ag-metric-chips--markup"
                      items={[
                        {
                          amount: row.used_quota_actual || 0,
                          color: 'default',
                          decimals: API_KEY_AMOUNT_DECIMALS,
                          dollarTone: 'warning',
                          label: t('user_keys.cost_actual', '成本'),
                        },
                        {
                          amount: profit,
                          color: 'default',
                          decimals: API_KEY_AMOUNT_DECIMALS,
                          dollarTone: 'success',
                          label: t('user_keys.profit', '利润'),
                        },
                      ]}
                    />
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <MetricChips
                      className="ag-metric-chips--usage"
                      items={[
                        {
                          amount: row.today_cost,
                          color: 'warning',
                          decimals: API_KEY_AMOUNT_DECIMALS,
                          dollarTone: 'warning',
                          label: t('api_keys.sales', '销售'),
                          mutedWhenZero: true,
                        },
                        {
                          amount: row.thirty_day_cost,
                          color: 'warning',
                          decimals: API_KEY_AMOUNT_DECIMALS,
                          dollarTone: 'warning',
                          label: t('api_keys.sales', '销售'),
                          mutedWhenZero: true,
                        },
                        {
                          amount: row.today_actual_cost ?? 0,
                          color: 'warning',
                          decimals: API_KEY_AMOUNT_DECIMALS,
                          dollarTone: 'warning',
                          label: t('api_keys.consumption', '消耗'),
                          mutedWhenZero: true,
                        },
                        {
                          amount: row.thirty_day_actual_cost ?? 0,
                          color: 'warning',
                          decimals: API_KEY_AMOUNT_DECIMALS,
                          dollarTone: 'warning',
                          label: t('api_keys.consumption', '消耗'),
                          mutedWhenZero: true,
                        },
                      ]}
                    />
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    {formatExpiry(row.expires_at, t('user_keys.never_expire'))}
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <div className="ag-table-row-actions flex items-center justify-center gap-0.5">
                      <TableRowActionButton
                        ariaBusy={revealMutation.isPending}
                        ariaLabel={t('api_keys.reveal')}
                        isDisabled={revealMutation.isPending}
                        title={t('api_keys.reveal')}
                        onClick={() => revealMutation.mutate(row.id)}
                      >
                        {t('api_keys.reveal_short', '查看')}
                      </TableRowActionButton>
                      <TableRowActionButton
                        ariaLabel={t('common.edit')}
                        title={t('common.edit')}
                        onClick={() => openEdit(row)}
                      >
                        {t('common.edit_short', '编辑')}
                      </TableRowActionButton>
                      <TableRowMoreMenu
                        ariaLabel={t('common.more')}
                        menuLabel={t('common.actions')}
                        items={[
                          {
                            key: 'import_ccs',
                            label: t('user_keys.import_ccs'),
                            onSelect: () => openCcsModal(row),
                          },
                          {
                            key: 'toggle',
                            label: row.status === 'active' ? t('user_keys.disable') : t('user_keys.enable'),
                            isDisabled: toggleStatusMutation.isPending,
                            onSelect: () => toggleStatusMutation.mutate({
                              id: row.id,
                              status: row.status === 'active' ? 'disabled' : 'active',
                            }),
                          },
                          {
                            key: 'use_key',
                            label: t('user_keys.use_key'),
                            onSelect: () => openUseKeyModal(row),
                          },
                          {
                            key: 'delete',
                            label: t('common.delete'),
                            onSelect: () => setDeleteTarget(row),
                            tone: 'danger',
                          },
                        ]}
                      />
                    </div>
                  </CommonTable.Cell>
                </CommonTable.Row>
              );
            })
          )}
        </CommonTable.Body>
      </CommonTable>

      {/* 创建/编辑弹窗 */}
      <EditKeyModal
        open={modalOpen}
        isEdit={!!editingKey}
        form={form}
        setForm={setForm}
        groupOptions={groupOptions}
        onClose={closeModal}
        onSubmit={handleSubmit}
        loading={saving}
      />

      {/* 新建密钥后显示完整密钥 */}
      <CreateKeyModal
        open={!!createdKey}
        createdKey={createdKey}
        onClose={() => setCreatedKey(null)}
      />

      {/* 查看密钥弹窗 */}
      <Modal state={revealedKeyModalState}>
        <DialogTriggerShim />
        <Modal.Backdrop>
          <Modal.Container placement="center" scroll="inside" size="md">
            <Modal.Dialog className="ag-elevation-modal">
              <Modal.Header>
                <Modal.Heading>{t('api_keys.reveal')}</Modal.Heading>
                <Modal.CloseTrigger />
              </Modal.Header>
              <Modal.Body>
                <div className="space-y-4">
                  <Alert status="warning">
                    <Alert.Indicator>
                      <AlertTriangle className="h-4 w-4" />
                    </Alert.Indicator>
                    <Alert.Content>
                      <Alert.Description>{t('api_keys.key_reveal_warning')}</Alert.Description>
                    </Alert.Content>
                  </Alert>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 break-all rounded-md border border-glass-border bg-surface px-3 py-2 font-mono text-sm text-text">
                      {revealedKey || ''}
                    </code>
                    <Button size="sm" variant="secondary" onPress={handleCopyRevealedKey}>
                      {revealedKeyCopied
                        ? <Check className="h-3.5 w-3.5 text-success" />
                        : <Copy className="h-3.5 w-3.5" />}
                      <span className={revealedKeyCopied ? 'text-success' : undefined}>
                        {t('common.copy')}
                      </span>
                    </Button>
                  </div>
                </div>
              </Modal.Body>
              <Modal.Footer>
                <Button variant="primary" onPress={closeRevealedKeyModal}>
                  {t('common.close')}
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>

      {/* 使用 API 密钥配置弹窗 */}
      <UseKeyModal
        useKeyTarget={useKeyTarget}
        useKeyValue={useKeyValue}
        useKeyPlatform={useKeyPlatform}
        showClientTabs={showClientTabs}
        useKeyTab={useKeyTab}
        setUseKeyTab={setUseKeyTab}
        useKeyShell={useKeyShell}
        setUseKeyShell={setUseKeyShell}
        onClose={closeUseKeyModal}
      />

      {/* CCS 导入弹窗 */}
      <CcsImportModal
        open={!!ccsTarget}
        ccsKeyValue={ccsKeyValue}
        ccsPlatform={ccsPlatform}
        onClose={closeCcsModal}
      />

      {/* 删除确认 */}
      <AlertDialog
        isOpen={!!deleteTarget}
        onOpenChange={(open) => {
          if (!open) setDeleteTarget(null);
        }}
      >
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('user_keys.delete_key')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('user_keys.delete_confirm', { name: deleteTarget?.name })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setDeleteTarget(null)}>
                  {t('common.cancel')}
                </Button>
                <Button
                  aria-busy={deleteMutation.isPending}
                  isDisabled={deleteMutation.isPending}
                  variant="danger"
                  onPress={() => deleteTarget && deleteMutation.mutate(deleteTarget.id)}
                >
                  {deleteMutation.isPending ? <Spinner size="sm" /> : null}
                  {t('common.confirm')}
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </TablePage>
  );
}
