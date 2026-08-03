import { useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertTriangle, Check, Copy, Plus, RefreshCw } from 'lucide-react';
import { Alert, AlertDialog, Button, EmptyState, Modal, Spinner, useOverlayState } from '@heroui/react';
import { DialogTriggerShim } from '../../shared/components/DialogTriggerShim';
import { apikeysApi } from '../../shared/api/apikeys';
import { groupsApi } from '../../shared/api/groups';
import { usePagination } from '../../shared/hooks/usePagination';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { queryKeys } from '../../shared/queryKeys';
import { DEFAULT_PAGE_SIZE, FETCH_ALL_PARAMS } from '../../shared/constants';
import { formatExpiry } from '../../shared/utils/format';
import { getAvatarColor } from '../../shared/utils/avatar';
import { getTotalPages } from '../../shared/utils/pagination';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { APIKeySearchFilterComboBox } from '../../shared/components/APIKeySearchFilterComboBox';
import { UserSearchFilterComboBox } from '../../shared/components/UserSearchFilterComboBox';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import { CommonTable } from '../../shared/components/CommonTable';
import { TablePage } from '../../shared/components/TablePage';
import { MetricChips } from '../../shared/components/MetricChips';
import { NativeStatusChip } from '../../shared/components/NativeStatusChip';
import { TableRowActionButton } from '../../shared/components/TableRowActionButton';
import { TableRowMoreMenu, type TableRowMoreMenuItem } from '../../shared/components/TableRowMoreMenu';
import { useClipboard } from '../../shared/hooks/useClipboard';
import { useCopyFeedback } from '../../shared/hooks/useCopyFeedback';
import { useMediaQuery } from '../../shared/hooks/useMediaQuery';
import { useToast } from '../../shared/ui';
import { CreateKeyModal } from './apikeys/CreateKeyModal';
import { EditKeyModal } from './apikeys/EditKeyModal';
import { UserKeysMobileList } from '../user/userkeys/UserKeysMobileList';
import { KeyGroupRateStack, getUserKeyRowModel } from '../user/userkeys/keyRowModel';
import type { APIKeyResp, GroupResp } from '../../shared/types';

const API_KEY_AMOUNT_DECIMALS = 3;

type EditingKeyState = {
  apiKey: APIKeyResp;
  originalKey: string;
};

type RevealKeyRequest = {
  row: APIKeyResp;
  target: 'view' | 'edit';
};

export default function APIKeysPage() {
  const { t } = useTranslation();
  const { toast } = useToast();
  const copy = useClipboard();
  const queryClient = useQueryClient();

  const { page, setPage, pageSize, setPageSize } = usePagination(DEFAULT_PAGE_SIZE, 'admin.api-keys');
  const [selectedAPIKeyID, setSelectedAPIKeyID] = useState('');
  const [selectedAPIKeyLabel, setSelectedAPIKeyLabel] = useState('');
  const [selectedUserID, setSelectedUserID] = useState('');
  const [selectedUserLabel, setSelectedUserLabel] = useState('');
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [editingKey, setEditingKey] = useState<EditingKeyState | null>(null);
  const [deletingKey, setDeletingKey] = useState<APIKeyResp | null>(null);
  const [createdKey, setCreatedKey] = useState<string | null>(null);
  const [revealedKey, setRevealedKey] = useState<string | null>(null);
  const {
    copied: revealedKeyCopied,
    showCopied: showRevealedKeyCopied,
    resetCopied: resetRevealedKeyCopied,
  } = useCopyFeedback();

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: queryKeys.apikeys(page, pageSize, selectedAPIKeyID, selectedUserID),
    queryFn: ({ signal }) => apikeysApi.adminList({
      page,
      page_size: pageSize,
      user_id: selectedUserID ? Number(selectedUserID) : undefined,
      api_key_id: selectedAPIKeyID ? Number(selectedAPIKeyID) : undefined,
      include_usage: true,
    }, { signal }),
    placeholderData: keepPreviousData,
  });

  const { data: groupsData } = useQuery({
    queryKey: queryKeys.groupsAll(),
    queryFn: () => groupsApi.list(FETCH_ALL_PARAMS),
  });

  const createMutation = useCrudMutation({
    mutationFn: apikeysApi.create,
    successMessage: t('api_keys.create_success'),
    queryKey: queryKeys.apikeys(),
    onSuccess: (resp) => {
      setShowCreateModal(false);
      if (resp.key) setCreatedKey(resp.key);
    },
  });

  const updateMutation = useCrudMutation({
    mutationFn: ({ id, data }: { id: number; data: Parameters<typeof apikeysApi.adminUpdate>[1] }) =>
      apikeysApi.adminUpdate(id, data),
    successMessage: t('api_keys.update_success'),
    queryKey: queryKeys.apikeys(),
    onSuccess: () => setEditingKey(null),
  });

  const deleteMutation = useCrudMutation({
    mutationFn: apikeysApi.delete,
    successMessage: t('api_keys.delete_success'),
    queryKey: queryKeys.apikeys(),
    onSuccess: () => setDeletingKey(null),
  });

  const revealMutation = useMutation({
    mutationFn: async ({ row, target }: RevealKeyRequest) => ({
      row,
      target,
      revealed: await apikeysApi.reveal(row.id),
    }),
    onSuccess: ({ row, target, revealed }) => {
      const originalKey = revealed.key ?? '';
      if (target === 'edit') {
        setEditingKey({ apiKey: row, originalKey });
      } else if (originalKey) {
        setRevealedKey(originalKey);
      }
    },
    onError: (error: Error) => {
      toast('error', error.message);
    },
  });

  const toggleStatusMutation = useMutation({
    mutationFn: ({ id, status }: { id: number; status: 'active' | 'disabled' }) =>
      apikeysApi.adminUpdate(id, { status }),
    onSuccess: (_resp, variables) => {
      toast(
        'success',
        variables.status === 'active'
          ? t('user_keys.enable_success')
          : t('user_keys.disable_success'),
      );
      queryClient.invalidateQueries({ queryKey: queryKeys.apikeys() });
    },
    onError: (error: Error) => toast('error', error.message),
  });

  const rows = data?.list ?? [];
  const groupById = new Map<number, GroupResp>(
    (groupsData?.list ?? []).map((group: GroupResp) => [group.id, group]),
  );
  const total = data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const isMobileLayout = useMediaQuery('(max-width: 767px)');
  const closeRevealedKeyModal = () => {
    resetRevealedKeyCopied();
    setRevealedKey(null);
  };
  const handleAPIKeySelectionChange = useCallback((value: string, label: string) => {
    setSelectedAPIKeyID(value);
    setSelectedAPIKeyLabel(label);
    setPage(1);
  }, [setPage]);
  const handleUserSelectionChange = useCallback((value: string, label: string) => {
    setSelectedUserID(value);
    setSelectedUserLabel(label);
    setPage(1);
  }, [setPage]);
  const handleCopyRevealedKey = async () => {
    if (await copy(revealedKey ?? '')) {
      showRevealedKeyCopied();
    }
  };
  const createdKeyModalState = useOverlayState({
    isOpen: !!createdKey,
    onOpenChange: (open) => {
      if (!open) setCreatedKey(null);
    },
  });
  const revealedKeyModalState = useOverlayState({
    isOpen: !!revealedKey,
    onOpenChange: (open) => {
      if (!open) closeRevealedKeyModal();
    },
  });

  const getKeyMoreMenuItems = (row: APIKeyResp): TableRowMoreMenuItem[] => [
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
      key: 'delete',
      label: t('common.delete'),
      onSelect: () => setDeletingKey(row),
      tone: 'danger',
    },
  ];

  const renderKeyActions = (row: APIKeyResp, showMoreMenu = true) => (
    <div className="ag-table-row-actions flex items-center justify-center gap-0.5">
      <TableRowActionButton
        ariaBusy={revealMutation.isPending}
        ariaLabel={t('api_keys.reveal')}
        isDisabled={revealMutation.isPending}
        title={t('api_keys.reveal')}
        onClick={() => revealMutation.mutate({ row, target: 'view' })}
      >
        {t('api_keys.reveal_short', '查看')}
      </TableRowActionButton>
      <TableRowActionButton
        ariaBusy={revealMutation.isPending}
        ariaLabel={t('common.edit')}
        isDisabled={revealMutation.isPending}
        title={t('common.edit')}
        onClick={() => revealMutation.mutate({ row, target: 'edit' })}
      >
        {t('common.edit_short', '编辑')}
      </TableRowActionButton>
      {showMoreMenu ? (
        <TableRowMoreMenu
          ariaLabel={t('common.more')}
          menuLabel={t('common.actions')}
          items={getKeyMoreMenuItems(row)}
        />
      ) : null}
    </div>
  );

  const renderKeyOwner = (row: APIKeyResp) => {
    const email = row.user_email || `#${row.user_id}`;
    const username = row.username || `#${row.user_id}`;
    return (
      <div className="flex min-w-0 items-center gap-2" title={username}>
        <span
          className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full text-xs font-semibold text-white"
          style={{ backgroundColor: getAvatarColor(email) }}
        >
          {(email[0] ?? '?').toUpperCase()}
        </span>
        <span className="min-w-0 truncate text-[13px] font-medium text-text">
          <span className="text-text-tertiary">#{row.user_id}</span> {email}
        </span>
      </div>
    );
  };

  return (
    <TablePage
      className="ag-api-keys-page"
      toolbar={(
        <div className="ag-page-toolbar-filter-row">
            <div className="w-full sm:w-56">
              <APIKeySearchFilterComboBox
                ariaLabel={t('usage.search_api_key', '搜索 API Key')}
                emptyPrompt={t('usage.search_api_key', '搜索 API Key')}
                loadingLabel={t('common.loading')}
                noDataLabel={t('common.no_data')}
                placeholder={t('usage.search_api_key', '搜索 API Key')}
                scope="admin"
                selectedKey={selectedAPIKeyID || null}
                selectedLabel={selectedAPIKeyLabel}
                onSelectionChange={handleAPIKeySelectionChange}
              />
            </div>
            <div className="w-full sm:w-56">
              <UserSearchFilterComboBox
                ariaLabel={t('api_keys.user_search_placeholder')}
                emptyPrompt={t('api_keys.user_search_placeholder')}
                loadingLabel={t('common.loading')}
                noDataLabel={t('common.no_data')}
                placeholder={t('api_keys.user_search_placeholder')}
                selectedKey={selectedUserID || null}
                selectedLabel={selectedUserLabel}
                onSelectionChange={handleUserSelectionChange}
              />
            </div>
        </div>
      )}
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
          <Button className="ag-page-toolbar-button" variant="primary" onPress={() => setShowCreateModal(true)}>
            <Plus className="w-4 h-4" />
            {t('api_keys.create')}
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

      {isMobileLayout ? (
        <UserKeysMobileList
          emptyTitle={t('common.no_data')}
          getMoreMenuItems={getKeyMoreMenuItems}
          groupMap={groupById}
          isLoading={isLoading}
          renderActions={renderKeyActions}
          renderOwner={renderKeyOwner}
          rows={rows}
        />
      ) : (
      <CommonTable
        ariaLabel={t('api_keys.title', 'API keys')}
        className="ag-api-keys-table"
        minWidth={1264}
      >
        <CommonTable.Header>
          <CommonTable.Column id="id" style={{ width: 72 }}>
            {t('common.id')}
          </CommonTable.Column>
          <CommonTable.Column id="name" style={{ minWidth: '8.5rem', width: '8.5rem' }}>{t('common.name')}</CommonTable.Column>
          <CommonTable.Column id="user" style={{ minWidth: '11rem', width: '11rem' }}>{t('common.user')}</CommonTable.Column>
          <CommonTable.Column id="key_prefix" style={{ minWidth: '10rem', width: '10rem' }}>{t('user_keys.key_table_header', '密钥')}</CommonTable.Column>
          <CommonTable.Column id="group_id" style={{ width: '15rem' }}>{t('user_keys.group')}</CommonTable.Column>
          <CommonTable.Column id="status" style={{ width: '5.5rem' }}>{t('common.status')}</CommonTable.Column>
          <CommonTable.Column id="quota" style={{ width: '18.5rem' }}>{t('api_keys.quota_used')}</CommonTable.Column>
          <CommonTable.Column id="usage" style={{ width: '20.875rem' }}>{t('api_keys.usage_window', '用量(今日/30天)')}</CommonTable.Column>
          <CommonTable.Column id="expires_at" style={{ width: '7rem' }}>{t('api_keys.expire_time')}</CommonTable.Column>
          <CommonTable.Column id="actions" style={{ width: 112 }}>{t('common.actions')}</CommonTable.Column>
        </CommonTable.Header>
        <CommonTable.Body>
          {isLoading ? (
            <TableLoadingRow colSpan={10} />
          ) : rows.length === 0 ? (
            <CommonTable.Row id="empty">
              <CommonTable.Cell colSpan={10}>
                <EmptyState>
                  <div className="text-sm text-default-500">{t('common.no_data')}</div>
                </EmptyState>
              </CommonTable.Cell>
            </CommonTable.Row>
          ) : (
            rows.map((row: APIKeyResp) => {
              const model = getUserKeyRowModel(row, groupById, undefined, t);

              return (
                <CommonTable.Row id={String(row.id)} key={row.id}>
                  <CommonTable.Cell>
                    <span className="font-mono">{row.id}</span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <span className="ag-api-key-name truncate font-medium" title={row.name}>
                      {row.name}
                    </span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    {renderKeyOwner(row)}
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <span
                      className="ag-api-key-prefix-chip inline-flex items-center text-xs px-2 py-0.5 rounded-sm border border-glass-border bg-surface text-text-secondary font-mono"
                      title={model.keyHint}
                    >
                      {model.keyHint}
                    </span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <KeyGroupRateStack model={model} t={t} />
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    <NativeStatusChip status={model.displayStatus} />
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
                          label: t('api_keys.quota_used_short', '使用'),
                        },
                        {
                          amount: row.quota_usd > 0 ? row.quota_usd : undefined,
                          color: 'success',
                          decimals: API_KEY_AMOUNT_DECIMALS,
                          label: t('api_keys.quota_total_short', '配额'),
                          value: '∞',
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
                    <span className="font-mono">{formatExpiry(row.expires_at)}</span>
                  </CommonTable.Cell>
                  <CommonTable.Cell>
                    {renderKeyActions(row)}
                  </CommonTable.Cell>
                </CommonTable.Row>
              );
            })
          )}
        </CommonTable.Body>
      </CommonTable>
      )}

      <CreateKeyModal
        open={showCreateModal}
        groups={groupsData?.list ?? []}
        onClose={() => setShowCreateModal(false)}
        onSubmit={(data) => createMutation.mutate(data)}
        loading={createMutation.isPending}
      />

      <Modal state={createdKeyModalState}>
        <DialogTriggerShim />
        <Modal.Backdrop>
          <Modal.Container placement="center" scroll="inside" size="md">
            <Modal.Dialog className="ag-elevation-modal">
              <Modal.Header>
                <Modal.Heading>{t('api_keys.key_created')}</Modal.Heading>
                <Modal.CloseTrigger />
              </Modal.Header>
              <Modal.Body>
                <div className="space-y-4">
                  <Alert status="warning">
                    <Alert.Indicator>
                      <AlertTriangle className="h-4 w-4" />
                    </Alert.Indicator>
                    <Alert.Content>
                      <Alert.Description>{t('api_keys.key_created_warning')}</Alert.Description>
                    </Alert.Content>
                  </Alert>
                  <div className="flex items-center gap-2">
                    <code className="flex-1 break-all rounded-md border border-glass-border bg-surface px-3 py-2 font-mono text-sm text-text">
                      {createdKey ?? ''}
                    </code>
                    <Button size="sm" variant="secondary" onPress={() => copy(createdKey ?? '')}>
                      <Copy className="h-3.5 w-3.5" />
                      {t('common.copy')}
                    </Button>
                  </div>
                </div>
              </Modal.Body>
              <Modal.Footer>
                <Button variant="primary" onPress={() => setCreatedKey(null)}>
                  {t('api_keys.key_saved_close')}
                </Button>
              </Modal.Footer>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>

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
                      {revealedKey ?? ''}
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

      {editingKey && (
        <EditKeyModal
          open
          apiKey={editingKey.apiKey}
          originalKey={editingKey.originalKey}
          groups={groupsData?.list ?? []}
          onClose={() => setEditingKey(null)}
          onSubmit={(data) => updateMutation.mutate({ id: editingKey.apiKey.id, data })}
          loading={updateMutation.isPending}
        />
      )}

      <AlertDialog
        isOpen={!!deletingKey}
        onOpenChange={(open) => {
          if (!open) setDeletingKey(null);
        }}
      >
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('api_keys.delete_key')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('api_keys.delete_key_confirm', { name: deletingKey?.name })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setDeletingKey(null)}>
                  {t('common.cancel')}
                </Button>
                <Button
                  aria-busy={deleteMutation.isPending}
                  isDisabled={deleteMutation.isPending}
                  variant="danger"
                  onPress={() => deletingKey && deleteMutation.mutate(deletingKey.id)}
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
