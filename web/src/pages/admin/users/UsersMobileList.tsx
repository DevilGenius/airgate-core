import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { MobileRecordList, type MobileRecordItem } from '../../../shared/components/MobileRecordList';
import type { TableRowMoreMenuItem } from '../../../shared/components/TableRowMoreMenu';
import type { UserResp } from '../../../shared/types';
import { getAvatarColor } from '../../../shared/utils/avatar';
import { formatDateTime } from '../../../shared/utils/format';

export function UsersMobileList({
  emptyTitle,
  getMoreMenuItems,
  isLoading,
  renderActions,
  renderStatus,
  rows,
}: {
  emptyTitle: string;
  getMoreMenuItems: (row: UserResp) => TableRowMoreMenuItem[];
  isLoading: boolean;
  renderActions: (row: UserResp) => ReactNode;
  renderStatus: (row: UserResp) => ReactNode;
  rows: UserResp[];
}) {
  const { t } = useTranslation();
  const items: MobileRecordItem[] = rows.map((row) => ({
    id: row.id,
    title: (
      <span className="flex min-w-0 items-center gap-2">
        <span
          className="flex h-6 w-6 flex-shrink-0 items-center justify-center rounded-full text-white text-xs font-semibold"
          style={{ backgroundColor: getAvatarColor(row.email) }}
        >
          {(row.email[0] ?? '?').toUpperCase()}
        </span>
        <span className="truncate" title={row.email}>{row.email}</span>
      </span>
    ),
    description: row.username || '-',
    meta: (
      <span className="ag-users-role-chip" data-tone={row.role === 'admin' ? 'warning' : 'default'}>
        <span className="ag-users-role-chip__label">
          {row.role === 'admin' ? t('users.role_admin') : t('users.role_user')}
        </span>
      </span>
    ),
    fields: [
      {
        label: t('users.balance'),
        value: <span className="font-mono">${row.balance.toFixed(2)}</span>,
      },
      {
        label: t('common.status'),
        value: renderStatus(row),
      },
      {
        label: t('users.created_at'),
        value: formatDateTime(row.created_at),
      },
      {
        label: 'ID',
        value: <span className="font-mono">{row.id}</span>,
      },
    ],
    actions: renderActions(row),
    longPressMenu: {
      ariaLabel: t('common.actions'),
      items: getMoreMenuItems(row),
    },
  }));

  return (
    <div className="ag-users-mobile">
      <MobileRecordList emptyTitle={emptyTitle} isLoading={isLoading} items={items} />
    </div>
  );
}
