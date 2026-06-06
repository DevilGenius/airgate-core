import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { AlertDialog, Button, EmptyState, Modal, Skeleton, Spinner, useOverlayState } from '@heroui/react';
import { RotateCcw } from 'lucide-react';
import { DialogTriggerShim } from '../../../shared/components/DialogTriggerShim';
import {
  StatusChip,
} from '../../../shared/ui';
import { usersApi } from '../../../shared/api/users';
import { apikeysApi } from '../../../shared/api/apikeys';
import { queryKeys } from '../../../shared/queryKeys';
import { formatAPIKeyHint, formatDate } from '../../../shared/utils/format';
import { getTotalPages } from '../../../shared/utils/pagination';
import { CommonTable } from '../../../shared/components/CommonTable';
import { TablePaginationFooter } from '../../../shared/components/TablePaginationFooter';
import { DEFAULT_PAGE_SIZE } from '../../../shared/constants';
import { useCrudMutation } from '../../../shared/hooks/useCrudMutation';
import type { UserResp, APIKeyResp } from '../../../shared/types';

interface UserApiKeysModalProps {
  open: boolean;
  user: UserResp;
  onClose: () => void;
}

export function UserApiKeysModal({ open, user, onClose }: UserApiKeysModalProps) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [page, setPage] = useState(1);
  const [resetTarget, setResetTarget] = useState<APIKeyResp | null>(null);
  const pageSize = DEFAULT_PAGE_SIZE;
  const listQueryKey = ['user-api-keys', user.id] as const;

  const { data, isLoading } = useQuery({
    queryKey: [...listQueryKey, page, pageSize],
    queryFn: () => usersApi.apiKeys(user.id, { page, page_size: pageSize }),
    enabled: open,
  });
  const resetUsageMutation = useCrudMutation<APIKeyResp, number>({
    mutationFn: (id) => apikeysApi.adminResetUsage(id),
    queryKey: listQueryKey,
    successMessage: t('api_keys.reset_usage_success'),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.apikeys() });
      setResetTarget(null);
    },
  });

  const rows = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = getTotalPages(total, pageSize);
  const modalState = useOverlayState({
    isOpen: open,
    onOpenChange: (nextOpen) => {
      if (!nextOpen) onClose();
    },
  });

  return (
    <>
      <Modal state={modalState}>
        <DialogTriggerShim />
        <Modal.Backdrop>
          <Modal.Container placement="center" scroll="inside" size="md">
            <Modal.Dialog
              className="ag-elevation-modal ag-user-api-keys-modal"
            >
              <Modal.Header>
                <Modal.Heading>{`${t('users.api_keys')} - ${user.email}`}</Modal.Heading>
                <Modal.CloseTrigger />
              </Modal.Header>
              <Modal.Body>
                <CommonTable
                  ariaLabel={t('users.api_keys')}
                  className="ag-user-api-keys-table"
                  footer={(
                    <TablePaginationFooter
                      page={page}
                      setPage={setPage}
                      total={total}
                      totalPages={totalPages}
                    />
                  )}
                  contentStyle={{ tableLayout: 'fixed' }}
                >
                  <CommonTable.Header>
                    <CommonTable.Column id="name" isRowHeader>{t('api_keys.title')}</CommonTable.Column>
                    <CommonTable.Column id="key_prefix">{t('api_keys.key_prefix')}</CommonTable.Column>
                    <CommonTable.Column id="quota_usd">{t('api_keys.quota_used')}</CommonTable.Column>
                    <CommonTable.Column id="status">{t('common.status')}</CommonTable.Column>
                    <CommonTable.Column id="created_at">{t('users.created_at')}</CommonTable.Column>
                    <CommonTable.Column id="actions" style={{ width: 96 }}>{t('common.actions')}</CommonTable.Column>
                  </CommonTable.Header>
                  <CommonTable.Body>
                    {isLoading ? (
                      Array.from({ length: 5 }).map((_, index) => (
                        <CommonTable.Row id={`loading-${index}`} key={`loading-${index}`}>
                          {Array.from({ length: 6 }).map((__, cellIndex) => (
                            <CommonTable.Cell key={cellIndex}>
                              <Skeleton className="h-4 w-24" />
                            </CommonTable.Cell>
                          ))}
                        </CommonTable.Row>
                      ))
                    ) : rows.length === 0 ? (
                      <CommonTable.Row id="empty">
                        <CommonTable.Cell colSpan={6}>
                          <EmptyState>
                            <div className="text-sm text-default-500">{t('common.no_data')}</div>
                          </EmptyState>
                        </CommonTable.Cell>
                      </CommonTable.Row>
                    ) : (
                      rows.map((row: APIKeyResp) => (
                        <CommonTable.Row id={String(row.id)} key={row.id}>
                          <CommonTable.Cell>{row.name}</CommonTable.Cell>
                          <CommonTable.Cell>
                            <span className="font-mono text-xs text-text-secondary">
                              {formatAPIKeyHint(row.key_prefix)}
                            </span>
                          </CommonTable.Cell>
                          <CommonTable.Cell>
                            <span className="font-mono text-xs">
                              ${row.used_quota.toFixed(2)} / {row.quota_usd > 0 ? `$${row.quota_usd.toFixed(2)}` : '∞'}
                            </span>
                          </CommonTable.Cell>
                          <CommonTable.Cell>
                            <StatusChip status={row.status} />
                          </CommonTable.Cell>
                          <CommonTable.Cell>
                            <span className="text-xs text-text-secondary">{formatDate(row.created_at)}</span>
                          </CommonTable.Cell>
                          <CommonTable.Cell>
                            <div className="ag-table-row-actions flex justify-center">
                              <Button
                                isIconOnly
                                size="sm"
                                variant="secondary"
                                aria-label={t('api_keys.reset_usage')}
                                isDisabled={
                                  resetUsageMutation.isPending ||
                                  (row.used_quota <= 0 && row.used_quota_actual <= 0)
                                }
                                onPress={() => setResetTarget(row)}
                              >
                                {resetUsageMutation.isPending && resetUsageMutation.variables === row.id
                                  ? <Spinner size="sm" />
                                  : <RotateCcw className="w-3.5 h-3.5" />}
                              </Button>
                            </div>
                          </CommonTable.Cell>
                        </CommonTable.Row>
                      ))
                    )}
                  </CommonTable.Body>
                </CommonTable>
              </Modal.Body>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>

      <AlertDialog
        isOpen={!!resetTarget}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) setResetTarget(null);
        }}
      >
        <DialogTriggerShim />
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="sm">
            <AlertDialog.Dialog className="ag-elevation-modal">
              <AlertDialog.Header>
                <AlertDialog.Icon status="danger" />
                <AlertDialog.Heading>{t('api_keys.reset_usage')}</AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>{t('api_keys.reset_usage_confirm', { name: resetTarget?.name })}</AlertDialog.Body>
              <AlertDialog.Footer>
                <Button variant="secondary" onPress={() => setResetTarget(null)}>
                  {t('common.cancel')}
                </Button>
                <Button
                  aria-busy={resetUsageMutation.isPending}
                  isDisabled={resetUsageMutation.isPending}
                  variant="danger"
                  onPress={() => resetTarget && resetUsageMutation.mutate(resetTarget.id)}
                >
                  {resetUsageMutation.isPending ? <Spinner size="sm" /> : null}
                  {t('common.confirm')}
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </>
  );
}
