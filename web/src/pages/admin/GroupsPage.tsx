import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { keepPreviousData, useQuery } from '@tanstack/react-query';
import {
  Plus,
  RefreshCw,
} from 'lucide-react';
import { AlertDialog, Button, EmptyState, Spinner } from '@heroui/react';
import { DialogTriggerShim } from '../../shared/components/DialogTriggerShim';
import { groupsApi } from '../../shared/api/groups';
import { usePlatforms } from '../../shared/hooks/usePlatforms';
import { useUrlPagination, useUrlQueryParam } from '../../shared/hooks/useUrlTableState';
import { useCrudMutation } from '../../shared/hooks/useCrudMutation';
import { queryKeys } from '../../shared/queryKeys';
import { DEFAULT_PAGE_SIZE } from '../../shared/constants';
import { getTotalPages } from '../../shared/utils/pagination';
import { TablePaginationFooter } from '../../shared/components/TablePaginationFooter';
import { TableLoadingRow } from '../../shared/components/TableLoadingRow';
import { CommonTable } from '../../shared/components/CommonTable';
import { TablePage } from '../../shared/components/TablePage';
import { TableRowMoreMenu } from '../../shared/components/TableRowMoreMenu';
import { MetricChips } from '../../shared/components/MetricChips';
import { SimpleSelect } from '../../shared/components/SimpleSelect';
import { GroupFormModal } from './groups/EditGroupModal';
import { GroupRateOverridesModal } from './groups/GroupRateOverridesModal';
import type { GroupResp, CreateGroupReq, UpdateGroupReq } from '../../shared/types';

type GroupRowActionTone = 'danger' | 'default' | 'primary';
type GroupChipTone = 'accent' | 'default' | 'warning';

function NativeGroupChip({
  children,
  tone,
}: {
  children: string;
  tone: GroupChipTone;
}) {
  return (
    <span className="ag-groups-soft-chip" data-tone={tone}>
      <span className="ag-groups-soft-chip__label">{children}</span>
    </span>
  );
}

function GroupRowActionButton({
  ariaLabel,
  children,
  onClick,
  title,
  tone = 'default',
}: {
  ariaLabel: string;
  children: string;
  onClick: () => void;
  title?: string;
  tone?: GroupRowActionTone;
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
      <span aria-hidden="true" className="ag-table-row-native-action__label">{children}</span>
    </button>
  );
}

export default function GroupsPage() {
  const { t } = useTranslation();
  const { platforms, platformName, instructionPresets } = usePlatforms();

  const PLATFORM_OPTIONS = [
    { value: '', label: t('groups.all_platforms') },
    ...platforms.map((p) => ({ value: p, label: platformName(p) })),
  ];
  // 筛选状态
  const { page, setPage, pageSize, setPageSize } = useUrlPagination(DEFAULT_PAGE_SIZE, 'admin.groups');
  const [platformFilter, setPlatformFilter] = useUrlQueryParam('platform');

  // 弹窗状态
  const [showCreateModal, setShowCreateModal] = useState(false);
  const [editingGroup, setEditingGroup] = useState<GroupResp | null>(null);
  const [deletingGroup, setDeletingGroup] = useState<GroupResp | null>(null);
  const [rateOverrideGroup, setRateOverrideGroup] = useState<GroupResp | null>(null);

  // 查询分组列表
  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: queryKeys.groups(page, pageSize, platformFilter),
    queryFn: () =>
      groupsApi.list({
        page,
        page_size: pageSize,
        platform: platformFilter || undefined,
      }),
    placeholderData: keepPreviousData,
  });

  // 创建分组
  const createMutation = useCrudMutation<unknown, CreateGroupReq>({
    mutationFn: (data) => groupsApi.create(data),
    successMessage: t('groups.create_success'),
    queryKey: queryKeys.groups(),
    onSuccess: () => setShowCreateModal(false),
  });

  // 更新分组
  const updateMutation = useCrudMutation<unknown, { id: number; data: UpdateGroupReq }>({
    mutationFn: ({ id, data }) => groupsApi.update(id, data),
    successMessage: t('groups.update_success'),
    queryKey: queryKeys.groups(),
    onSuccess: () => setEditingGroup(null),
  });

  // 删除分组
  const deleteMutation = useCrudMutation<unknown, number>({
    mutationFn: (id) => groupsApi.delete(id),
    successMessage: t('groups.delete_success'),
    queryKey: queryKeys.groups(),
    onSuccess: () => {
      setDeletingGroup(null);
      if ((data?.list?.length ?? 0) === 1 && page > 1) {
        setPage(page - 1);
      }
    },
  });

  const rows = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const selectedPlatformLabel = PLATFORM_OPTIONS.find((option) => option.value === platformFilter)?.label ?? t('groups.all_platforms');

  return (
    <TablePage
      toolbar={(
        <div className="ag-page-toolbar-filter-row">
            <div className="w-full sm:w-48">
              <SimpleSelect
                ariaLabel={t('groups.platform')}
                fullWidth
                items={PLATFORM_OPTIONS.map((item) => ({ key: item.value, label: item.label }))}
                selectedKey={platformFilter}
                selectedLabel={selectedPlatformLabel}
                onSelectionChange={(key) => {
                  setPlatformFilter(key);
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
            size="sm"
            variant="ghost"
            onPress={() => refetch()}
          >
            <RefreshCw className="w-4 h-4" />
          </Button>
          <Button className="ag-page-toolbar-button" variant="primary" onPress={() => setShowCreateModal(true)}>
            <Plus className="w-4 h-4" />
            {t('groups.create')}
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

      {/* 表格 */}
      <CommonTable
        ariaLabel={t('groups.title', 'Groups')}
        className="ag-groups-table"
        contentClassName="ag-groups-table-content"
        minWidth={1064}
      >
            <CommonTable.Header>
              <CommonTable.Column id="name" style={{ width: 144 }}>{t('common.name')}</CommonTable.Column>
              <CommonTable.Column id="platform" style={{ width: 104 }}>{t('groups.platform')}</CommonTable.Column>
              <CommonTable.Column id="subscription_type" style={{ width: 84 }}>{t('groups.subscription_type')}</CommonTable.Column>
              <CommonTable.Column id="rate_multiplier" style={{ width: 72 }}>
                {t('groups.rate_multiplier')}
              </CommonTable.Column>
              <CommonTable.Column id="is_exclusive" style={{ width: 72 }}>
                {t('groups.group_type')}
              </CommonTable.Column>
              <CommonTable.Column id="account_stats" style={{ width: '10.25rem' }}>
                {t('groups.account_stats')}
              </CommonTable.Column>
              <CommonTable.Column id="usage" style={{ width: '10.25rem' }}>
                {t('groups.usage')}
              </CommonTable.Column>
              <CommonTable.Column id="capacity" style={{ width: 100 }}>
                {t('groups.capacity')}
              </CommonTable.Column>
              <CommonTable.Column id="sort_weight" style={{ width: 64 }}>
                {t('groups.sort_weight')}
              </CommonTable.Column>
              <CommonTable.Column id="actions" style={{ width: 96 }}>
                {t('common.actions')}
              </CommonTable.Column>
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
                rows.map((row) => (
                    <CommonTable.Row id={String(row.id)} key={row.id}>
                    <CommonTable.Cell>
                      <span className="inline-flex max-w-[9.5rem] items-center">
                        <span style={{ color: 'var(--ag-text)' }} className="truncate font-medium">
                          {row.name}
                        </span>
                      </span>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <span className="inline-flex max-w-[6.5rem] items-center">
                        <span className="truncate">{platformName(row.platform)}</span>
                      </span>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <NativeGroupChip tone={row.subscription_type === 'subscription' ? 'accent' : 'default'}>
                        {row.subscription_type === 'subscription' ? t('groups.type_subscription') : t('groups.type_standard')}
                      </NativeGroupChip>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <div className="min-w-0">
                        <span className="font-mono" style={{ color: 'var(--ag-primary)' }}>
                          {row.rate_multiplier}x
                        </span>
                      </div>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      {row.is_exclusive ? (
                        <NativeGroupChip tone="warning">{t('groups.type_exclusive')}</NativeGroupChip>
                      ) : (
                        <NativeGroupChip tone="default">{t('groups.type_public')}</NativeGroupChip>
                      )}
                    </CommonTable.Cell>
                    <CommonTable.Cell className="ag-groups-metric-cell">
                      <MetricChips
                        className="ag-metric-chips--stack ag-metric-chips--markup ag-metric-chips--account-stats ag-metric-chips--compact-y"
                        items={[
                          {
                            color: 'default' as const,
                            label: `${t('groups.account_available')}/${t('groups.account_total')}`,
                            value: `${row.account_active}/${row.account_total}`,
                          },
                          {
                            color: row.account_error > 0 ? 'danger' as const : 'default' as const,
                            label: t('groups.account_error'),
                            value: String(row.account_error),
                          },
                        ]}
                      />
                    </CommonTable.Cell>
                    <CommonTable.Cell className="ag-groups-metric-cell">
                      <MetricChips
                        className="ag-metric-chips--stack ag-metric-chips--markup ag-metric-chips--compact-y"
                        items={[
                          {
                            amount: row.today_cost,
                            color: 'warning' as const,
                            dollarTone: 'warning',
                            label: t('groups.today_cost'),
                            mutedWhenZero: true,
                          },
                          {
                            amount: row.total_cost,
                            color: 'warning' as const,
                            dollarTone: 'warning',
                            label: t('groups.total_cost'),
                            mutedWhenZero: true,
                          },
                        ]}
                      />
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <div className="inline-flex min-w-[6.75rem] items-center justify-end whitespace-nowrap font-mono tabular-nums">
                        <span className="font-mono" style={{ color: row.capacity_used > 0 ? 'var(--ag-primary)' : undefined }}>
                          {row.capacity_used}
                        </span>
                        <span className="mx-0.5" style={{ color: 'var(--ag-text-tertiary)' }}>/</span>
                        <span className="font-mono">{row.capacity_total}</span>
                      </div>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <span className="inline-flex items-center font-mono">
                        {row.sort_weight}
                      </span>
                    </CommonTable.Cell>
                    <CommonTable.Cell>
                      <div className="ag-table-row-actions flex items-center justify-center gap-0.5">
                        <GroupRowActionButton
                          ariaLabel={t('common.edit')}
                          title={t('common.edit')}
                          onClick={() => setEditingGroup(row)}
                        >
                          {t('common.edit_short', '编辑')}
                        </GroupRowActionButton>
                        <GroupRowActionButton
                          ariaLabel={t('groups.rate_override_manage')}
                          title={t('groups.rate_override_manage')}
                          tone="primary"
                          onClick={() => setRateOverrideGroup(row)}
                        >
                          {t('groups.rate_override_short', '倍率')}
                        </GroupRowActionButton>
                        <TableRowMoreMenu
                          ariaLabel={t('common.more')}
                          menuLabel={t('common.actions')}
                          items={[
                            {
                              key: 'delete',
                              label: t('common.delete'),
                              onSelect: () => setDeletingGroup(row),
                              tone: 'danger',
                            },
                          ]}
                        />
                      </div>
                    </CommonTable.Cell>
                    </CommonTable.Row>
                ))
              )}
            </CommonTable.Body>
      </CommonTable>

      {/* 创建弹窗 */}
      <GroupFormModal
        open={showCreateModal}
        title={t('groups.create')}
        onClose={() => setShowCreateModal(false)}
        onSubmit={(data) => createMutation.mutate(data as CreateGroupReq)}
        loading={createMutation.isPending}
        platforms={platforms}
        instructionPresets={instructionPresets}
      />

      {/* 编辑弹窗 */}
      {editingGroup && (
        <GroupFormModal
          open
          title={t('groups.edit')}
          group={editingGroup}
          onClose={() => setEditingGroup(null)}
          onSubmit={(data) =>
            updateMutation.mutate({ id: editingGroup.id, data })
          }
          loading={updateMutation.isPending}
          platforms={platforms}
          instructionPresets={instructionPresets}
        />
      )}

      {/* 分组专属倍率管理 */}
      {rateOverrideGroup && (
        <GroupRateOverridesModal
          open
          group={rateOverrideGroup}
          onClose={() => setRateOverrideGroup(null)}
        />
      )}

      {/* 删除确认 */}
      <AlertDialog
        isOpen={!!deletingGroup}
        onOpenChange={(open) => {
          if (!open) setDeletingGroup(null);
        }}
      >
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('groups.delete_title')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('groups.delete_confirm', { name: deletingGroup?.name })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setDeletingGroup(null)}>
                  {t('common.cancel')}
                </Button>
                <Button
                  aria-busy={deleteMutation.isPending}
                  isDisabled={deleteMutation.isPending}
                  variant="danger"
                  onPress={() => deletingGroup && deleteMutation.mutate(deletingGroup.id)}
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
