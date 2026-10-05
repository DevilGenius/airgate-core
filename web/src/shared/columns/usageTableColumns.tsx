import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { createUsageClientColumn, useUsageColumns, type UsageColumnConfig, type UsageRow } from './usageColumns';
import { combineUsageTimingColumns, createUsageTpsColumn } from './usageTimingColumns';

export type UsageTableMode = 'admin' | 'user' | 'apiKey';

// Define raw metric order once; timing pairs are merged after visibility filtering.
const USAGE_COLUMN_ORDER = [
  'user_id', 'created_at', 'api_key', 'model',
  'ws_dial_ms', 'first_event_ms', 'first_token_ms', 'duration_ms', 'tps',
  'tokens', 'cost', 'stream', 'endpoint', 'account_name', 'client',
] as const;

export const ADMIN_USAGE_DEFAULT_COLUMN_KEYS = USAGE_COLUMN_ORDER.flatMap((key) =>
  key === 'duration_ms' ? [] : [key === 'first_token_ms' ? 'latency' : key],
);

function formatUsageTimingMs(value: number) {
  if (!Number.isFinite(value) || value <= 0) return '-';
  return value >= 1000 ? `${(value / 1000).toFixed(2)}s` : `${value}ms`;
}

export function buildUsageTableColumns(
  sharedColumns: UsageColumnConfig<UsageRow>[],
  mode: UsageTableMode,
  t: TFunction,
): UsageColumnConfig<UsageRow>[] {
  const adminColumns: UsageColumnConfig<UsageRow>[] = [
    {
      key: 'user_id',
      title: t('common.user'),
      width: '160px',
      render: (row) => {
        if (!('user_id' in row)) return null;
        const fallbackLabel = row.user_deleted ? t('usage.user_deleted') : `#${row.user_id}`;
        const label = row.user_email || fallbackLabel;

        return (
          <div className="flex min-w-0 items-center gap-1.5">
            <span className="shrink-0 font-mono text-xs text-text-tertiary">{row.user_id > 0 ? `#${row.user_id}` : '-'}</span>
            <span className={`min-w-0 truncate text-[13px] font-medium ${row.user_deleted ? 'text-text-tertiary' : 'text-text'}`} title={label}>
              {label}
            </span>
          </div>
        );
      },
    },
  ];
  const endpointColumn: UsageColumnConfig<UsageRow> = {
    key: 'endpoint',
    title: t('usage.endpoint', '端点'),
    width: mode === 'admin' ? '156px' : '180px',
    hideOnMobile: true,
    render: (row) => (
      <span className="block truncate font-mono text-xs leading-tight text-text-secondary" title={row.endpoint || '-'}>
        {row.endpoint || '-'}
      </span>
    ),
  };
  const apiKeyColumn: UsageColumnConfig<UsageRow> = {
    key: 'api_key',
    title: t('usage.api_key', 'API Key'),
    width: mode === 'admin' ? '88px' : '80px',
    hideOnMobile: true,
    render: (row) => {
      if (row.api_key_id === 0) {
        return <span className="block max-w-full truncate text-[13px] text-text-tertiary">{t('usage.api_key_plugin_call')}</span>;
      }
      if ('api_key_deleted' in row && row.api_key_deleted) {
        return <span className="block max-w-full truncate text-[13px] text-text-tertiary">{t('usage.api_key_deleted')}</span>;
      }
      const name = ('api_key_name' in row && row.api_key_name) || '-';
      return (
        <span className="block max-w-full truncate text-xs text-text-secondary" title={name}>{name}</span>
      );
    },
  };
  const accountColumn: UsageColumnConfig<UsageRow> = {
    key: 'account_name',
    title: t('usage.upstream_credential', 'Credential'),
    width: '172px',
    hideOnMobile: true,
    render: (row) => {
      if (!('account_id' in row)) return null;
      const name = row.account_name || '-';
      const accountID = row.account_id > 0 ? `#${row.account_id}` : '';
      const displayName = [accountID, name].filter(Boolean).join(' ');
      const email = row.account_email?.trim();
      const title = email && name !== '-' ? `${displayName}\n${email}` : displayName;
      return (
        <div className="flex w-full min-w-0 flex-col items-start text-left" title={title}>
          <span className={`flex w-full min-w-0 items-center gap-1 text-xs font-medium ${row.account_deleted ? 'text-text-tertiary' : 'text-text-secondary'}`}>
            <span className="shrink-0 font-mono text-[11px] text-warning">{accountID}</span>
            <span className={`min-w-0 truncate text-left ${row.account_deleted ? 'line-through' : ''}`}>{name}</span>
          </span>
          {email && name !== '-' ? (
            <span className={`block w-full min-w-0 truncate text-left text-[11px] leading-tight text-text-tertiary ${row.account_deleted ? 'line-through' : ''}`}>{email}</span>
          ) : null}
        </div>
      );
    },
  };
  const clientColumn = createUsageClientColumn(t);
  const wsDialColumn: UsageColumnConfig<UsageRow> = {
    key: 'ws_dial_ms',
    title: t('usage.ws_dial', 'WS'),
    width: '64px',
    hideOnMobile: true,
    render: (row) => (
      <span className="block text-center font-mono text-[13px] text-text-tertiary">
        {formatUsageTimingMs(row.ws_dial_ms)}
      </span>
    ),
  };

  const byKey = new Map(sharedColumns.map((column) => [column.key, column]));
  for (const column of [endpointColumn, clientColumn, createUsageTpsColumn<UsageRow>(t)]) {
    byKey.set(column.key, column);
  }
  if (mode !== 'apiKey') byKey.set(apiKeyColumn.key, apiKeyColumn);
  if (mode === 'admin') {
    for (const column of [...adminColumns, accountColumn, wsDialColumn]) {
      byKey.set(column.key, column);
    }
  }
  // Preserve the existing compact admin widths and roomier user widths.
  const widths: Record<string, string> = mode === 'admin'
    ? { model: '240px', cost: '120px', first_event_ms: '64px', first_token_ms: '64px', duration_ms: '64px' }
    : {};
  return USAGE_COLUMN_ORDER.flatMap((key) => {
    const column = byKey.get(key);
    return column ? [{ ...column, width: widths[key] ?? column.width }] : [];
  });
}

export function useUsageTableColumns(mode: UsageTableMode) {
  const { t } = useTranslation();
  const sharedColumns = useUsageColumns({ customerScope: mode === 'apiKey', adminView: mode === 'admin' });
  const allColumns = useMemo(() => buildUsageTableColumns(sharedColumns, mode, t), [sharedColumns, mode, t]);
  const columns = useMemo(() => combineUsageTimingColumns(allColumns), [allColumns]);
  return { allColumns, columns };
}
