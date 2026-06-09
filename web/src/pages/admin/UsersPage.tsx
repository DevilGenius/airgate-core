import { startTransition, useCallback, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertDialog, Button, EmptyState, Spinner } from '@heroui/react';
import { DialogTriggerShim } from '../../shared/components/DialogTriggerShim';
import { usersApi } from '../../shared/api/users';
import { settingsApi } from '../../shared/api/settings';
import { useUrlPagination, useUrlQueryParam } from '../../shared/hooks/useUrlTableState';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { queryKeys } from '../../shared/queryKeys';
import { DEFAULT_PAGE_SIZE } from '../../shared/constants';
import { getTotalPages } from '../../shared/utils/pagination';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { SearchFilterInput } from '../../shared/components/SearchFilterInput';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import { CommonTable } from '../../shared/components/CommonTable';
import { TablePage } from '../../shared/components/TablePage';
import { TableRowMoreMenu } from '../../shared/components/TableRowMoreMenu';
import { NativeSwitch } from '../../shared/components/NativeSwitch';
import { SimpleSelect } from '../../shared/components/SimpleSelect';
import { getAvatarColor } from '../../shared/utils/avatar';
import { formatDateTime } from '../../shared/utils/format';
import { CreateUserModal } from './users/CreateUserModal';
import { EditUserModal } from './users/EditUserModal';
import { BalanceModal } from './users/BalanceModal';
import { UserApiKeysModal } from './users/UserApiKeysModal';
import { BalanceHistoryModal } from './users/BalanceHistoryModal';
import { UserGroupsModal } from './users/UserGroupsModal';
import type { UserResp } from '../../shared/types';
import { Plus, RefreshCw } from 'lucide-react';

const FALLBACK_DEFAULT_USER_MAX_CONCURRENCY = 5;

type UserRowActionTone = 'default' | 'danger' | 'info' | 'muted' | 'primary' | 'success' | 'warning';

function NativeUserRoleChip({
  children,
  tone,
}: {
  children: string;
  tone: 'default' | 'warning';
}) {
  return (
    <span className="ag-users-role-chip" data-tone={tone}>
      <span className="ag-users-role-chip__label">{children}</span>
    </span>
  );
}

function UserRowActionButton({
  ariaLabel,
  children,
  isCircleSymbol = false,
  onClick,
  title,
  tone = 'default',
}: {
  ariaLabel: string;
  children: string;
  isCircleSymbol?: boolean;
  onClick: () => void;
  title?: string;
  tone?: UserRowActionTone;
}) {
  return (
    <button
      type="button"
      aria-label={ariaLabel}
      className="ag-table-row-native-action"
      data-tone={tone}
      title={title ?? ariaLabel}
      onClick={(event) => {
        event.stopPropagation();
        onClick();
      }}
    >
      <span className="sr-only">{ariaLabel}</span>
      <span
        aria-hidden="true"
        className={isCircleSymbol
          ? 'ag-table-row-native-action__label ag-table-row-native-action__label--circle'
          : 'ag-table-row-native-action__label'}
      >
        {children}
      </span>
    </button>
  );
}

function defaultUserMaxConcurrency(settings?: Array<{ key: string; value: string }>) {
  const raw = settings?.find((item) => item.key === 'default_concurrency')?.value;
  const value = Number.parseInt((raw ?? '').trim(), 10);
  return Number.isFinite(value) && value > 0 ? value : FALLBACK_DEFAULT_USER_MAX_CONCURRENCY;
}

export default function UsersPage() {
  const { t } = useTranslation();
  const queryClient = useQueryClient();

  const { page, setPage, pageSize, setPageSize } = useUrlPagination(DEFAULT_PAGE_SIZE, 'admin.users');
  const [keyword, setKeyword] = useUrlQueryParam('q');
  const [statusFilter, setStatusFilter] = useUrlQueryParam('status');

  const [showCreateModal, setShowCreateModal] = useState(false);
  const [editingUser, setEditingUser] = useState<UserResp | null>(null);
  const [balanceUser, setBalanceUser] = useState<{ user: UserResp; defaultAction: 'add' | 'subtract' } | null>(null);
  const [deletingUser, setDeletingUser] = useState<UserResp | null>(null);
  const [disablingUser, setDisablingUser] = useState<UserResp | null>(null);
  const [apiKeysUser, setApiKeysUser] = useState<UserResp | null>(null);
  const [balanceHistoryUser, setBalanceHistoryUser] = useState<UserResp | null>(null);
  const [groupsUser, setGroupsUser] = useState<UserResp | null>(null);

  const { data, isLoading, refetch, isFetching } = useQuery({
    queryKey: queryKeys.users(page, pageSize, keyword, statusFilter),
    queryFn: () =>
      usersApi.list({
        page,
        page_size: pageSize,
        keyword: keyword || undefined,
        status: statusFilter || undefined,
      }),
    placeholderData: keepPreviousData,
  });

  const { data: settings } = useQuery({
    queryKey: queryKeys.settings(),
    queryFn: settingsApi.list,
  });

  const createMutation = useCrudMutation({
    mutationFn: usersApi.create,
    successMessage: t('users.create_success'),
    queryKey: queryKeys.users(),
    onSuccess: () => setShowCreateModal(false),
  });

  const updateMutation = useCrudMutation({
    mutationFn: ({ id, data }: { id: number; data: Parameters<typeof usersApi.update>[1] }) =>
      usersApi.update(id, data),
    successMessage: t('users.update_success'),
    queryKey: queryKeys.users(),
    onSuccess: () => setEditingUser(null),
  });

  const balanceMutation = useCrudMutation({
    mutationFn: ({ id, data }: { id: number; data: Parameters<typeof usersApi.adjustBalance>[1] }) =>
      usersApi.adjustBalance(id, data),
    successMessage: t('users.balance_success'),
    queryKey: queryKeys.users(),
    onSuccess: () => setBalanceUser(null),
  });

  const toggleMutation = useCrudMutation({
    mutationFn: usersApi.toggleStatus,
    successMessage: t('users.toggle_success'),
    queryKey: queryKeys.users(),
    onSuccess: () => setDisablingUser(null),
  });

  const deleteMutation = useCrudMutation({
    mutationFn: usersApi.delete,
    successMessage: t('users.delete_success'),
    queryKey: queryKeys.users(),
    onSuccess: () => setDeletingUser(null),
  });

  const rows = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const statusOptions = [
    { id: '', label: t('users.all_status') },
    { id: 'active', label: t('status.active') },
    { id: 'disabled', label: t('status.disabled') },
  ];
  const selectedStatusLabel = statusOptions.find((item) => item.id === statusFilter)?.label ?? t('users.all_status');
  const handleKeywordChange = useCallback((nextKeyword: string) => {
    startTransition(() => {
      setKeyword(nextKeyword);
      setPage(1);
    });
  }, [setPage]);

  return (
    <TablePage
      className="ag-users-page ag-toolbar-standard-page"
      toolbar={(
        <div className="ag-page-toolbar-filter-row">
            <div className="w-full sm:w-48">
              <SearchFilterInput
                ariaLabel={t('users.search_placeholder')}
                placeholder={t('users.search_placeholder')}
                value={keyword}
                onSearchChange={handleKeywordChange}
              />
            </div>
            <div className="w-full sm:w-48">
              <SimpleSelect
                ariaLabel={t('common.status')}
                fullWidth
                items={statusOptions.map((item) => ({ key: item.id, label: item.label }))}
                selectedKey={statusFilter}
                selectedLabel={selectedStatusLabel}
                onSelectionChange={(key) => {
                  setStatusFilter(key);
                  setPage(1);
                }}
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
            isDisabled={isFetching}
            size="sm"
            variant="ghost"
            onPress={() => refetch()}
          >
            <RefreshCw className={`w-4 h-4 ${isFetching ? 'animate-spin' : ''}`} />
          </Button>
          <Button className="ag-page-toolbar-button" variant="primary" onPress={() => setShowCreateModal(true)}>
            <Plus className="w-4 h-4" />
            {t('users.create')}
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
        ariaLabel={t('users.title', 'Users')}
        minWidth={1180}
      >
            <CommonTable.Header>
              <CommonTable.Column id="id" style={{ width: 72 }}>
                ID
              </CommonTable.Column>
              <CommonTable.Column id="email">{t('users.email')}</CommonTable.Column>
              <CommonTable.Column id="username">{t('users.username')}</CommonTable.Column>
              <CommonTable.Column id="role">{t('users.role')}</CommonTable.Column>
              <CommonTable.Column id="balance">{t('users.balance')}</CommonTable.Column>
              <CommonTable.Column id="status">{t('common.status')}</CommonTable.Column>
              <CommonTable.Column id="created_at">{t('users.created_at')}</CommonTable.Column>
              <CommonTable.Column id="actions" style={{ width: 224 }}>{t('common.actions')}</CommonTable.Column>
            </CommonTable.Header>
            <CommonTable.Body>
              {isLoading ? (
                <TableLoadingRow colSpan={8} />
              ) : rows.length === 0 ? (
                <CommonTable.Row id="empty">
                  <CommonTable.Cell colSpan={8}>
                    <EmptyState>
                      <div className="text-sm text-default-500">{t('common.no_data')}</div>
                    </EmptyState>
                  </CommonTable.Cell>
                </CommonTable.Row>
              ) : (
                rows.map((row) => (
                  <CommonTable.Row id={String(row.id)} key={row.id}>
                    <CommonTable.Cell>
                      <span className="text-text-tertiary font-mono">{row.id}</span>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <div className="flex items-center gap-2.5">
                        <div
                          className="w-7 h-7 rounded-full flex items-center justify-center text-white text-xs font-semibold flex-shrink-0"
                          style={{ backgroundColor: getAvatarColor(row.email) }}
                        >
                          {(row.email[0] ?? '?').toUpperCase()}
                        </div>
                        <span className="text-text truncate">{row.email}</span>
                      </div>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <span className="text-text-secondary">{row.username || '-'}</span>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <NativeUserRoleChip tone={row.role === 'admin' ? 'warning' : 'default'}>
                        {row.role === 'admin' ? t('users.role_admin') : t('users.role_user')}
                      </NativeUserRoleChip>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <span className="font-mono">${row.balance.toFixed(2)}</span>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <NativeSwitch
                        ariaLabel={row.status === 'active' ? t('users.disable') : t('users.enable')}
                        isDisabled={row.role === 'admin'}
                        isSelected={row.status === 'active'}
                        contentClassName="text-xs"
                        contentStyle={{ color: row.status === 'active' ? 'var(--ag-success)' : 'var(--ag-text-tertiary)' }}
                        label={row.status === 'active' ? t('status.enabled') : t('status.disabled')}
                        onChange={(isSelected) => {
                          if (isSelected) {
                            toggleMutation.mutate(row.id);
                          } else {
                            setDisablingUser(row);
                          }
                        }}
                      />
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <span className="text-xs text-text-secondary">{formatDateTime(row.created_at)}</span>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <div className="ag-table-row-actions ag-users-row-actions flex items-center justify-center gap-0.5">
                        <UserRowActionButton
                          ariaLabel={t('common.edit')}
                          title={t('common.edit')}
                          onClick={() => setEditingUser(row)}
                        >
                          {t('common.edit_short', '编辑')}
                        </UserRowActionButton>
                        <UserRowActionButton
                          ariaLabel={t('users.api_keys')}
                          title={t('users.api_keys')}
                          tone="primary"
                          onClick={() => setApiKeysUser(row)}
                        >
                          {t('users.api_keys_short', '密钥')}
                        </UserRowActionButton>
                        <UserRowActionButton
                          ariaLabel={t('users.groups')}
                          title={t('users.groups')}
                          tone="info"
                          onClick={() => setGroupsUser(row)}
                        >
                          {t('users.groups_short', '分组')}
                        </UserRowActionButton>
                        <UserRowActionButton
                          ariaLabel={t('users.topup')}
                          title={t('users.topup')}
                          tone="success"
                          isCircleSymbol
                          onClick={() => setBalanceUser({ user: row, defaultAction: 'add' })}
                        >
                          +
                        </UserRowActionButton>
                        <UserRowActionButton
                          ariaLabel={t('users.refund')}
                          title={t('users.refund')}
                          tone="warning"
                          isCircleSymbol
                          onClick={() => setBalanceUser({ user: row, defaultAction: 'subtract' })}
                        >
                          -
                        </UserRowActionButton>
                        <UserRowActionButton
                          ariaLabel={t('users.balance_history')}
                          title={t('users.balance_history')}
                          onClick={() => setBalanceHistoryUser(row)}
                        >
                          {t('users.balance_history_short', '记录')}
                        </UserRowActionButton>
                        {row.role !== 'admin' ? (
                          <TableRowMoreMenu
                            ariaLabel={t('common.more')}
                            menuLabel={t('common.actions')}
                            items={[
                              {
                                key: 'delete',
                                label: t('common.delete'),
                                onSelect: () => setDeletingUser(row),
                                tone: 'danger',
                              },
                            ]}
                          />
                        ) : null}
                      </div>
                    </CommonTable.Cell>
                  </CommonTable.Row>
                ))
              )}
            </CommonTable.Body>
      </CommonTable>

      <CreateUserModal
        open={showCreateModal}
        onClose={() => setShowCreateModal(false)}
        onSubmit={(data) => createMutation.mutate(data)}
        loading={createMutation.isPending}
        defaultMaxConcurrency={defaultUserMaxConcurrency(settings)}
      />

      {editingUser && (
        <EditUserModal
          open
          user={editingUser}
          onClose={() => setEditingUser(null)}
          onSubmit={(data) => updateMutation.mutate({ id: editingUser.id, data })}
          loading={updateMutation.isPending}
        />
      )}

      {balanceUser && (
        <BalanceModal
          open
          user={balanceUser.user}
          defaultAction={balanceUser.defaultAction}
          onClose={() => setBalanceUser(null)}
          onSubmit={(data) => balanceMutation.mutate({ id: balanceUser.user.id, data })}
          loading={balanceMutation.isPending}
        />
      )}

      <AlertDialog
        isOpen={!!disablingUser}
        onOpenChange={(open) => {
          if (!open) setDisablingUser(null);
        }}
      >
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('users.disable_title')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('users.disable_confirm', { email: disablingUser?.email })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setDisablingUser(null)}>
                  {t('common.cancel')}
                </Button>
                <Button
                  aria-busy={toggleMutation.isPending}
                  isDisabled={toggleMutation.isPending}
                  variant="danger"
                  onPress={() => disablingUser && toggleMutation.mutate(disablingUser.id)}
                >
                  {toggleMutation.isPending ? <Spinner size="sm" /> : null}
                  {t('common.confirm')}
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>

      <AlertDialog
        isOpen={!!deletingUser}
        onOpenChange={(open) => {
          if (!open) setDeletingUser(null);
        }}
      >
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('users.delete_title')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('users.delete_confirm', { email: deletingUser?.email })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setDeletingUser(null)}>
                  {t('common.cancel')}
                </Button>
                <Button
                  aria-busy={deleteMutation.isPending}
                  isDisabled={deleteMutation.isPending}
                  variant="danger"
                  onPress={() => deletingUser && deleteMutation.mutate(deletingUser.id)}
                >
                  {deleteMutation.isPending ? <Spinner size="sm" /> : null}
                  {t('common.confirm')}
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>

      {apiKeysUser && (
        <UserApiKeysModal open user={apiKeysUser} onClose={() => setApiKeysUser(null)} />
      )}

      {balanceHistoryUser && (
        <BalanceHistoryModal open user={balanceHistoryUser} onClose={() => setBalanceHistoryUser(null)} />
      )}

      {groupsUser && (
        <UserGroupsModal
          open
          user={groupsUser}
          onClose={() => setGroupsUser(null)}
          onSaved={() => {
            queryClient.invalidateQueries({ queryKey: queryKeys.users() });
            setGroupsUser(null);
          }}
        />
      )}
    </TablePage>
  );
}
