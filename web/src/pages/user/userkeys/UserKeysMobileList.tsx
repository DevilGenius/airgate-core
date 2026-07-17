import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { MetricChips } from '../../../shared/components/MetricChips';
import { MobileRecordList, type MobileRecordItem } from '../../../shared/components/MobileRecordList';
import { NativeStatusChip } from '../../../shared/components/NativeStatusChip';
import type { TableRowMoreMenuItem } from '../../../shared/components/TableRowMoreMenu';
import type { APIKeyResp, GroupResp } from '../../../shared/types';
import { formatExpiry } from '../../../shared/utils/format';
import {
  KeyGroupRateStack,
  buildMarkupChipItems,
  buildQuotaChipItems,
  buildUsageChipItems,
  getUserKeyRowModel,
} from './keyRowModel';

/**
 * 用户与管理密钥页共用的移动端卡片列表（≤767px 替代表格）。
 * 与用户页桌面表格共用 keyRowModel 的派生数据与 chips 构建，保证展示一致。
 */
export function UserKeysMobileList({
  emptyTitle,
  getMoreMenuItems,
  groupMap,
  isLoading,
  renderActions,
  rows,
  userGroupRates,
}: {
  emptyTitle: string;
  getMoreMenuItems: (row: APIKeyResp) => TableRowMoreMenuItem[];
  groupMap: Map<number, GroupResp>;
  isLoading: boolean;
  renderActions: (row: APIKeyResp, showMoreMenu: boolean) => ReactNode;
  rows: APIKeyResp[];
  userGroupRates?: Record<number, number>;
}) {
  const { t } = useTranslation();
  const items: MobileRecordItem[] = rows.map((row) => {
    const model = getUserKeyRowModel(row, groupMap, userGroupRates, t);
    return {
      id: row.id,
      title: <span title={row.name}>{row.name}</span>,
      description: (
        <span
          className="ag-api-key-prefix-chip inline-flex items-center text-xs px-2 py-0.5 rounded-sm border border-glass-border bg-surface text-text-secondary font-mono"
          title={model.keyHint}
        >
          {model.keyHint}
        </span>
      ),
      meta: <NativeStatusChip status={model.displayStatus} />,
      fields: [
        {
          className: 'ag-user-keys-mobile-field--group',
          label: t('user_keys.group'),
          value: <KeyGroupRateStack model={model} t={t} />,
        },
        {
          className: 'ag-user-keys-mobile-field--expiry',
          label: t('user_keys.expires_at'),
          value: formatExpiry(row.expires_at, t('user_keys.never_expire')),
        },
        {
          className: 'ag-mobile-record-field--wide ag-user-keys-mobile-field--chips',
          label: t('user_keys.quota_table_header', '配额'),
          value: <MetricChips className="ag-metric-chips--quota" items={buildQuotaChipItems(row, t)} />,
        },
        {
          className: 'ag-mobile-record-field--wide ag-user-keys-mobile-field--chips',
          label: t('user_keys.markup_title', '成本/利润'),
          value: <MetricChips className="ag-metric-chips--markup" items={buildMarkupChipItems(row, model, t)} />,
        },
        {
          className: 'ag-mobile-record-field--wide ag-user-keys-mobile-field--chips',
          label: t('api_keys.usage_window', '用量(今日/30天)'),
          value: <MetricChips className="ag-metric-chips--usage" items={buildUsageChipItems(row, t)} />,
        },
      ],
      actions: renderActions(row, false),
      longPressMenu: {
        ariaLabel: t('common.actions'),
        items: getMoreMenuItems(row),
      },
    };
  });

  return (
    <div className="ag-api-keys-table ag-user-keys-mobile">
      <MobileRecordList emptyTitle={emptyTitle} isLoading={isLoading} items={items} />
    </div>
  );
}
