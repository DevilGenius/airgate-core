import { memo, useMemo, type CSSProperties, type MouseEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { useQueryClient } from '@tanstack/react-query';
import { RefreshCw } from 'lucide-react';
import { getPluginAccountIdentity } from '../../../app/plugin-frontend-registry';
import { accountsApi } from '../../../shared/api/accounts';
import { queryKeys } from '../../../shared/queryKeys';
import type { AccountResp } from '../../../shared/types';
import { useToast } from '../../../shared/ui';
import {
  AccountCapacityChip,
  AccountRowActions,
  AccountSchedulingSwitch,
  AccountStatusCell,
  useUsageResetClock,
  type AccountTableColumn,
  type AccountUsageCredits,
  type AccountUsageData,
  type AccountUsageInfo,
  type AccountUsageTodayStats,
  type AccountUsageWindow,
} from './AccountPageSupport';

type QuotaRefreshResult = Awaited<ReturnType<typeof accountsApi.refreshQuota>>;

const ACCOUNT_GROUP_CARD_STYLE: CSSProperties = {
  background: 'var(--ag-bg-surface)',
  boxShadow: 'inset 0 0 0 1px color-mix(in oklab, var(--ag-primary) 28%, transparent)',
  color: 'var(--ag-text-secondary)',
};

const ACCOUNT_USAGE_BADGE_STYLE: CSSProperties = {
  background: 'var(--ag-bg-surface)',
  border: '1px solid var(--ag-glass-border)',
};

const ACCOUNT_USAGE_CELL_STYLE: CSSProperties = {
  fontFamily: 'var(--ag-font-mono)',
};

const ACCOUNT_USAGE_EMPTY_TEXT_STYLE: CSSProperties = {
  color: 'var(--ag-text-tertiary)',
};

const ACCOUNT_USAGE_REFRESH_ICON_STYLE: CSSProperties = {
  color: 'var(--ag-text-tertiary)',
};

type UsageMetricTone = 'info' | 'primary' | 'success' | 'warning';

type AccountUsageMetricLabels = {
  accountCostShort: string;
  imageCountInlineLabel: string;
  imageCountTooltip: string;
  refreshUsage: string;
  refreshUsageFailed: string;
  refreshUsageSuccess: string;
  todayAccessCount: string;
  todayStatsTooltip: string;
  userCostShort: string;
  windowAccountCost: string;
  windowUserCost: string;
};

type PreparedUsageWindowRow = {
  barPercent: number;
  color: string;
  id: string;
  label: string;
  percent: number;
  resetText: string;
  title: string;
};

type PreparedUsageView = {
  accessImageText: string;
  accessRequestsText: string;
  accessText: string;
  canRefresh: boolean;
  credits: AccountUsageCredits | null;
  hasContent: boolean;
  hasTodayStats: boolean;
  hideAccessLabel: boolean;
  missing: boolean;
  showImageCount: boolean;
  todayAccountCostText: string;
  todayStats: AccountUsageTodayStats | null;
  todayTokensText: string;
  todayUserCostText: string;
  windowRows: PreparedUsageWindowRow[];
  windowsClassName: string;
};

type AccountRowRenderMeta = {
  groupNames: string[];
  hiddenGroupCount: number;
  lastUsedRelative: string;
  lastUsedTitle: string;
  usage: PreparedUsageView;
  visibleGroups: string[];
};

type UsageWindowRow = { id: string; window: AccountUsageWindow };

const AccountUsageMetricChip = memo(function AccountUsageMetricChip({
  currency,
  label,
  labelSecondary,
  mutedLabel,
  solo,
  title,
  tone,
  value,
  valueSecondary,
}: {
  currency?: boolean;
  label: string;
  labelSecondary?: string;
  mutedLabel?: boolean;
  solo?: boolean;
  title?: string;
  tone: UsageMetricTone;
  value: string;
  valueSecondary?: string;
}) {
  const hasLabelSecondary = Boolean(labelSecondary);
  const hasValueSecondary = valueSecondary !== undefined;
  const labelClassName = mutedLabel
    ? 'ag-account-usage-metric-label text-text-secondary'
    : 'ag-account-usage-metric-label';
  const valueClassName = solo
    ? 'ag-account-usage-metric-value ag-account-usage-metric-value--solo'
    : 'ag-account-usage-metric-value';

  return (
    <span className="ag-account-usage-metric" data-tone={tone} title={title}>
      {solo ? null : (
        <span className={labelClassName}>
          <span className="ag-account-usage-metric-segment">{label}</span>
          {hasLabelSecondary ? (
            <>
              <span aria-hidden="true" className="ag-account-usage-metric-separator">/</span>
              <span className="ag-account-usage-metric-segment">{labelSecondary}</span>
            </>
          ) : null}
        </span>
      )}
      <span className={valueClassName}>
        {currency ? <span aria-hidden="true" className="ag-account-usage-metric-currency">$</span> : null}
        <span className="ag-account-usage-metric-segment">{value}</span>
        {hasValueSecondary ? (
          <>
            <span aria-hidden="true" className="ag-account-usage-metric-separator">/</span>
            <span className="ag-account-usage-metric-segment">{valueSecondary}</span>
          </>
        ) : null}
      </span>
    </span>
  );
});

const AccountUsageTodayMetricChips = memo(function AccountUsageTodayMetricChips({
  accessImageText,
  accessRequestsText,
  accessText,
  accountCostText,
  hideAccessLabel,
  labels,
  showImageCount,
  tokensText,
  userCostText,
}: {
  accessImageText: string;
  accessRequestsText: string;
  accessText: string;
  accountCostText: string;
  hideAccessLabel: boolean;
  labels: AccountUsageMetricLabels;
  showImageCount: boolean;
  tokensText: string;
  userCostText: string;
}) {
  return (
    <div className="ag-account-usage-metrics" title={labels.todayStatsTooltip}>
      <AccountUsageMetricChip
        label={labels.todayAccessCount}
        labelSecondary={showImageCount ? labels.imageCountInlineLabel : undefined}
        mutedLabel
        solo={hideAccessLabel}
        title={showImageCount ? labels.imageCountTooltip : undefined}
        tone="info"
        value={showImageCount ? accessRequestsText : accessText}
        valueSecondary={showImageCount ? accessImageText : undefined}
      />
      <AccountUsageMetricChip
        label="Token"
        mutedLabel
        tone="primary"
        value={tokensText}
      />
      <AccountUsageMetricChip
        currency
        label={labels.userCostShort}
        title={labels.windowUserCost}
        tone="warning"
        value={userCostText}
      />
      <AccountUsageMetricChip
        currency
        label={labels.accountCostShort}
        title={labels.windowAccountCost}
        tone="success"
        value={accountCostText}
      />
    </div>
  );
});

function toFiniteNumber(value: unknown) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function formatCompact(num: number, allowBillions = true) {
  if (!num) return '0';
  const abs = Math.abs(num);
  if (allowBillions && abs >= 1_000_000_000) return `${(num / 1_000_000_000).toFixed(1)}B`;
  if (abs >= 1_000_000) return `${(num / 1_000_000).toFixed(1)}M`;
  if (abs >= 1_000) return `${(num / 1_000).toFixed(1)}K`;
  return String(num);
}

function getResetSeconds(w: AccountUsageWindow, resetNow: number) {
  if (w.reset_at) {
    const delta = Date.parse(w.reset_at) - resetNow;
    if (Number.isFinite(delta)) return Math.max(0, Math.floor(delta / 1000));
  }
  if (typeof w.reset_seconds === 'number') return w.reset_seconds;
  if (typeof w.reset_after_seconds === 'number') return w.reset_after_seconds;
  return 0;
}

function formatReset(seconds: number) {
  if (!seconds || seconds <= 0) return '-';
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) {
    if (h > 0 || m === 0) return `${d}d${h}h`;
    return `${d}d${m}m`;
  }
  if (h > 0) return m > 0 ? `${h}h${m}m` : `${h}h`;
  return `${m}m`;
}

function usageColor(pct: number) {
  if (pct < 50) return 'var(--ag-success)';
  if (pct < 80) return 'var(--ag-warning)';
  return 'var(--ag-danger)';
}

function normalizeWindowToken(value?: string) {
  return value?.trim().toLowerCase().replace(/_/g, '-') || '';
}

function getWindowSlot(w: AccountUsageWindow) {
  const key = w.key || '';
  const label = w.label || '';
  const normalizedKey = normalizeWindowToken(key);
  const normalizedLabel = normalizeWindowToken(label);
  const slot = normalizeWindowToken(w.slot)
    || (normalizedKey.includes(':7d') || normalizedKey === '7d' || normalizedLabel.startsWith('7d') ? '7d'
      : normalizedKey === 'monthly' || normalizedKey.includes('monthly') || normalizedLabel.includes('monthly') ? 'monthly'
        : '5h');
  const group = w.group?.trim()
    || (key.startsWith('model:') ? key.replace(/^model:(5h|7d):/, 'model:') : 'base');
  return { group, slot };
}

function getWindowGroupLabel(group: string, slot: string, label: string) {
  const labelParts = label.trim().split(/\s+/);
  if (labelParts.length > 1 && normalizeWindowToken(labelParts[0]) === slot) {
    return labelParts.slice(1).join(' ');
  }
  const rawGroup = group.replace(/^model:/, '').trim();
  if (!rawGroup || rawGroup === 'base') return '';
  const parts = rawGroup.split(/[-\s:]+/).filter(Boolean);
  return parts[parts.length - 1] ?? rawGroup;
}

function getWindowDisplay(w: AccountUsageWindow) {
  const { group, slot } = getWindowSlot(w);
  const explicitLabel = w.display_label?.trim();
  const fallbackLabel = explicitLabel || slot || w.label;
  if (group !== 'base' && slot) {
    const groupLabel = getWindowGroupLabel(group, slot, w.label || '');
    if (groupLabel && (!explicitLabel || normalizeWindowToken(explicitLabel) === slot)) {
      return {
        label: `${slot}${groupLabel.charAt(0).toUpperCase()}`,
        title: `${slot} ${groupLabel}`,
      };
    }
  }
  return {
    label: fallbackLabel,
    title: w.label || fallbackLabel,
  };
}

function buildWindowRows(items: AccountUsageWindow[]): UsageWindowRow[] {
  const groups: Array<{ id: string; five?: AccountUsageWindow; seven?: AccountUsageWindow; other: AccountUsageWindow[] }> = [];
  const groupMap = new Map<string, { id: string; five?: AccountUsageWindow; seven?: AccountUsageWindow; other: AccountUsageWindow[] }>();

  for (const item of items) {
    const { group, slot } = getWindowSlot(item);
    let bucket = groupMap.get(group);
    if (!bucket) {
      bucket = { id: group, other: [] };
      groupMap.set(group, bucket);
      groups.push(bucket);
    }
    if (slot === '7d') bucket.seven = item;
    else if (slot === '5h') bucket.five = item;
    else bucket.other.push(item);
  }

  return groups.flatMap((group) => {
    const rows: UsageWindowRow[] = [];
    if (group.five) {
      rows.push({ id: `${group.id}:5h`, window: group.five });
    }
    if (group.seven) {
      rows.push({ id: `${group.id}:7d`, window: group.seven });
    }
    for (const window of group.other) {
      const { slot } = getWindowSlot(window);
      rows.push({ id: `${group.id}:${window.key || slot}:${rows.length}`, window });
    }
    return rows;
  });
}

function prepareUsageView(row: AccountResp, usage: AccountUsageInfo | undefined, resetNow: number): PreparedUsageView {
  const missing = !usage;
  const windows: AccountUsageWindow[] = Array.isArray(usage?.windows) ? usage.windows : [];
  const credits: AccountUsageCredits | null = usage?.credits || null;
  const todayStatsRaw = usage?.today_stats || null;
  const todayStats: AccountUsageTodayStats | null = todayStatsRaw
    ? {
        requests: toFiniteNumber(todayStatsRaw.requests),
        tokens: toFiniteNumber(todayStatsRaw.tokens),
        account_cost: toFiniteNumber(todayStatsRaw.account_cost),
        user_cost: toFiniteNumber(todayStatsRaw.user_cost),
      }
    : null;
  const windowRows = buildWindowRows(windows).map((item) => {
    const percent = Math.round(item.window.used_percent);
    const display = getWindowDisplay(item.window);
    const color = usageColor(item.window.used_percent);
    return {
      barPercent: Math.max(0, Math.min(100, percent)),
      color,
      id: item.id,
      label: display.label,
      percent,
      resetText: formatReset(getResetSeconds(item.window, resetNow)),
      title: display.title,
    };
  });
  const hasTodayStats = todayStats != null;
  const showImageCount = row.platform === 'openai';
  const todayImageCount = showImageCount ? (row.today_image_count ?? 0) : 0;
  const accessRequestsText = formatCompact(todayStats?.requests ?? 0, false);
  const accessImageText = formatCompact(todayImageCount, false);
  const accessText = showImageCount ? `${accessRequestsText}/${accessImageText}` : accessRequestsText;
  const todayTokensText = todayStats ? formatCompact(todayStats.tokens) : '0';
  const todayUserCostText = todayStats ? todayStats.user_cost.toFixed(2) : '0.00';
  const todayAccountCostText = todayStats ? todayStats.account_cost.toFixed(2) : '0.00';
  return {
    accessImageText,
    accessRequestsText,
    accessText,
    canRefresh: row.type !== 'apikey',
    credits,
    hasContent: windowRows.length > 0 || Boolean(credits) || hasTodayStats,
    hasTodayStats,
    hideAccessLabel: showImageCount && accessText.length > '100/100'.length,
    missing,
    showImageCount,
    todayAccountCostText,
    todayStats,
    todayTokensText,
    todayUserCostText,
    windowRows,
    windowsClassName: windowRows.length > 2
      ? 'ag-account-usage-windows ag-account-usage-windows--expanded'
      : 'ag-account-usage-windows',
  };
}

function prepareLastUsed(lastUsedAt: string | undefined, now: number, t: (key: string, options?: Record<string, unknown>) => string) {
  if (!lastUsedAt) return { relative: '', title: '' };
  const parsed = new Date(lastUsedAt);
  const diff = now - parsed.getTime();
  const seconds = Math.floor(diff / 1000);
  const minutes = Math.floor(seconds / 60);
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  let relative: string;
  if (seconds < 60) relative = t('accounts.just_now');
  else if (minutes < 60) relative = t('accounts.minutes_ago', { n: minutes });
  else if (hours < 24) relative = t('accounts.hours_ago', { n: hours });
  else relative = t('accounts.days_ago', { n: days });
  return {
    relative,
    title: parsed.toLocaleString(),
  };
}

type UseAccountTableColumnsArgs = {
  applyQuotaRefreshResult: (id: number, result: QuotaRefreshResult) => void;
  groupMap: Map<number, string>;
  onClearRateLimitMarkers: (id: number) => void;
  onDeleteAccount: (row: AccountResp) => void;
  onEditAccount: (row: AccountResp) => void;
  onRefreshQuota: (id: number) => void;
  onStatsAccount: (id: number) => void;
  onTestAccount: (row: AccountResp) => void;
  onToggleScheduling: (id: number) => void;
  platformFilter: string;
  platformName: (platform: string) => string;
  platformsKey: string;
  rows: AccountResp[];
  usageData: AccountUsageData | undefined;
};

export function useAccountTableColumns({
  applyQuotaRefreshResult,
  groupMap,
  onClearRateLimitMarkers,
  onDeleteAccount,
  onEditAccount,
  onRefreshQuota,
  onStatsAccount,
  onTestAccount,
  onToggleScheduling,
  platformFilter,
  platformName,
  platformsKey,
  rows,
  usageData,
}: UseAccountTableColumnsArgs): AccountTableColumn[] {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const { toast } = useToast();
  const resetNow = useUsageResetClock(Boolean(usageData?.accounts));

  const rowMetaById = useMemo(() => {
    const now = Date.now();
    const usageAccounts: Record<string, AccountUsageInfo> = usageData?.accounts ?? {};
    const nextMeta = new Map<number, AccountRowRenderMeta>();

    for (const row of rows) {
      const groupNames = (row.group_ids ?? []).map((gid) => groupMap.get(gid) ?? `#${gid}`);
      const visibleGroups = groupNames.length > 3 ? groupNames.slice(0, 2) : groupNames.slice(0, 3);
      const lastUsed = prepareLastUsed(row.last_used_at, now, t);

      nextMeta.set(row.id, {
        groupNames,
        hiddenGroupCount: Math.max(0, groupNames.length - visibleGroups.length),
        lastUsedRelative: lastUsed.relative,
        lastUsedTitle: lastUsed.title,
        usage: prepareUsageView(row, usageAccounts[String(row.id)], resetNow),
        visibleGroups,
      });
    }

    return nextMeta;
  }, [groupMap, resetNow, rows, t, usageData?.accounts]);

  const accountActionLabels = useMemo(() => ({
    actions: t('common.actions'),
    clearCooldowns: t('accounts.clear_family_cooldowns'),
    delete: t('common.delete'),
    edit: t('common.edit'),
    editShort: t('common.edit'),
    more: t('common.more'),
    refreshQuota: t('accounts.refresh_quota'),
    stats: t('accounts.view_stats'),
    statsShort: t('accounts.stats_short', '统计'),
    test: t('accounts.test_connection'),
    testShort: t('common.test'),
  }), [t]);

  const accountUsageLabels = useMemo<AccountUsageMetricLabels>(() => ({
    accountCostShort: t('accounts.account_cost_short', '成本'),
    imageCountInlineLabel: t('accounts.image_count_inline_label', '图').trim(),
    imageCountTooltip: t('accounts.image_count_tooltip', '今日生图请求数（gpt-image 系列）'),
    refreshUsage: t('accounts.refresh_usage', '点击刷新用量'),
    refreshUsageFailed: t('accounts.refresh_usage_failed', '用量刷新失败'),
    refreshUsageSuccess: t('accounts.refresh_usage_success', '用量刷新成功'),
    todayAccessCount: t('accounts.today_access_count', '访问'),
    todayStatsTooltip: t('accounts.today_stats_tooltip', '今日账号消耗（本地时区自然日）'),
    userCostShort: t('accounts.user_cost_short', '消费'),
    windowAccountCost: t('accounts.window_account_cost', '账号成本（上游计费）'),
    windowUserCost: t('accounts.window_user_cost', '用户消耗（平台计费）'),
  }), [t]);

  return useMemo<AccountTableColumn[]>(() => [
    {
      key: 'name',
      title: t('common.name'),
      width: '132px',
      mobileWidth: '112px',
      render: (row) => {
        const email = row.credentials?.email;
        return (
          <div className="flex w-full min-w-0 flex-col items-center text-center">
            <span style={{ color: 'var(--ag-text)' }} className="max-w-full truncate font-medium" title={row.name}>
              {row.name}
            </span>
            {email && (
              <span className="max-w-full truncate text-[11px]" style={{ color: 'var(--ag-text)' }} title={email}>
                {email}
              </span>
            )}
          </div>
        );
      },
    },
    {
      key: 'platform',
      title: t('accounts.platform_type'),
      width: '96px',
      mobileWidth: '84px',
      render: (row) => {
        const PluginAccountIdentity = getPluginAccountIdentity(row.platform);
        return (
          <div className="flex w-full min-w-0 flex-col items-center gap-1 text-center">
            <span className="inline-flex max-w-full min-w-0 items-center justify-center">
              <span className="min-w-0 truncate">{platformName(row.platform)}</span>
            </span>
            {PluginAccountIdentity ? (
              <PluginAccountIdentity
                accountId={row.id}
                accountType={row.type}
                context={{ account: row, credentials: row.credentials }}
              />
            ) : (
              <div className="flex max-w-full items-center justify-center gap-1">
                {row.type && (
                  <span className="truncate rounded px-1 py-0 text-[10px]" style={{ background: 'var(--ag-bg-surface)', border: '1px solid var(--ag-glass-border)', color: 'var(--ag-text-secondary)' }}>
                    {{ oauth: 'OAuth', session_key: 'Session Key', apikey: 'API Key' }[row.type] ?? row.type}
                  </span>
                )}
              </div>
            )}
          </div>
        );
      },
    },
    {
      key: 'groups',
      title: t('accounts.groups'),
      width: '92px',
      mobileWidth: '80px',
      align: 'center',
      render: (row) => {
        const meta = rowMetaById.get(row.id);
        if (!meta || meta.groupNames.length === 0) {
          return <span style={{ color: 'var(--ag-text-tertiary)' }}>-</span>;
        }
        return (
          <div className="ag-account-group-list" title={meta.groupNames.join('\n')}>
            {meta.visibleGroups.map((name, index) => (
              <span
                key={`${name}:${index}`}
                className="ag-account-group-chip"
              >
                {name}
              </span>
            ))}
            {meta.hiddenGroupCount > 0 ? (
              <span
                className="ag-account-group-chip ag-account-group-chip--more"
                style={ACCOUNT_GROUP_CARD_STYLE}
              >
                +{meta.hiddenGroupCount}
              </span>
            ) : null}
          </div>
        );
      },
    },
    {
      key: 'capacity',
      title: t('accounts.capacity'),
      width: '84px',
      mobileWidth: '68px',
      align: 'center',
      render: (row) => {
        const current = row.current_concurrency || 0;
        const max = row.max_concurrency;
        return <AccountCapacityChip current={current} max={max} />;
      },
    },
    {
      key: 'status',
      title: t('common.status'),
      width: '84px',
      mobileWidth: '76px',
      align: 'center',
      render: (row) => <AccountStatusCell row={row} />,
    },
    {
      key: 'scheduling',
      title: t('accounts.scheduling'),
      width: '80px',
      mobileWidth: '72px',
      align: 'center',
      render: (row) => (
        <AccountSchedulingSwitch
          ariaLabel={t('accounts.scheduling')}
          isSelected={row.state !== 'disabled'}
          rowId={row.id}
          onToggle={onToggleScheduling}
        />
      ),
    },
    {
      key: 'rate_multiplier',
      title: t('accounts.rate_multiplier'),
      width: '80px',
      mobileWidth: '72px',
      align: 'center',
      render: (row) => (
        <span className="font-mono" style={{ color: 'var(--ag-primary)' }}>
          {row.rate_multiplier}x
        </span>
      ),
    },
    {
      key: 'usage_window',
      title: t('accounts.usage_window'),
      width: '396px',
      mobileWidth: '364px',
      maxWidth: '396px',
      align: 'center',
      render: (row: AccountResp) => {
        const prepared = rowMetaById.get(row.id)?.usage;
        if (!prepared) {
          return <span style={ACCOUNT_USAGE_EMPTY_TEXT_STYLE}>-</span>;
        }

        const handleRefreshClick = async (event: MouseEvent<HTMLElement>) => {
          event.stopPropagation();
          const target = event.currentTarget as HTMLElement;
          target.style.opacity = '0.5';
          target.style.pointerEvents = 'none';
          try {
            const result = await accountsApi.refreshQuota(row.id);
            applyQuotaRefreshResult(row.id, result);
            queryClient.invalidateQueries({ queryKey: queryKeys.accounts() });
            queryClient.invalidateQueries({ queryKey: queryKeys.accountUsage(platformFilter) });
            toast('success', accountUsageLabels.refreshUsageSuccess);
          } catch (err) {
            const message = err instanceof Error && err.message ? err.message : accountUsageLabels.refreshUsageFailed;
            toast('error', message);
          }
          target.style.opacity = '1';
          target.style.pointerEvents = '';
        };

        if (prepared.missing) {
          return (
            <div
              className={
                prepared.canRefresh
                  ? 'flex items-center gap-1 cursor-pointer rounded px-1 py-0.5 transition-colors hover:bg-[var(--ag-glass-border)]'
                  : 'flex items-center gap-1 rounded px-1 py-0.5'
              }
              title={prepared.canRefresh ? accountUsageLabels.refreshUsage : undefined}
              onClick={prepared.canRefresh ? handleRefreshClick : undefined}
            >
              <span style={ACCOUNT_USAGE_EMPTY_TEXT_STYLE}>-</span>
              {prepared.canRefresh && <RefreshCw size={11} style={ACCOUNT_USAGE_REFRESH_ICON_STYLE} />}
            </div>
          );
        }

        if (!prepared.hasContent) {
          return (
            <div
              className={
                prepared.canRefresh
                  ? 'flex items-center gap-1 cursor-pointer rounded px-1 py-0.5 transition-colors hover:bg-[var(--ag-glass-border)]'
                  : 'flex items-center gap-1 rounded px-1 py-0.5'
              }
              title={prepared.canRefresh ? accountUsageLabels.refreshUsage : undefined}
              onClick={prepared.canRefresh ? handleRefreshClick : undefined}
            >
              <span style={ACCOUNT_USAGE_EMPTY_TEXT_STYLE}>-</span>
              {prepared.canRefresh && <RefreshCw size={11} style={ACCOUNT_USAGE_REFRESH_ICON_STYLE} />}
            </div>
          );
        }

        const hasTodayMetricChips = prepared.hasTodayStats;

        return (
          <div
            className={
              prepared.canRefresh
                ? 'ag-account-usage-cell ag-account-usage-cell--refreshable'
                : 'ag-account-usage-cell'
            }
            style={ACCOUNT_USAGE_CELL_STYLE}
            title={prepared.canRefresh ? accountUsageLabels.refreshUsage : undefined}
            onClick={prepared.canRefresh ? handleRefreshClick : undefined}
          >
            <div className={hasTodayMetricChips ? 'ag-account-usage-layout' : 'ag-account-usage-layout ag-account-usage-layout--centered'}>
              <div className={prepared.windowsClassName}>
                {prepared.windowRows.map((item) => (
                  <div key={item.id} className="ag-account-usage-window-row">
                    <span className="ag-account-usage-window-label text-text-secondary" style={ACCOUNT_USAGE_BADGE_STYLE} title={item.title}>
                      {item.label}
                    </span>
                    <div className="ag-account-usage-bar" style={{ background: 'var(--ag-glass-border)' }}>
                      <div
                        className="h-full rounded-full"
                        style={{ width: `${item.barPercent}%`, background: item.color }}
                      />
                    </div>
                    <span className="ag-account-usage-percent" style={{ color: item.color }}>
                      {item.percent}%
                    </span>
                    <span className="ag-account-usage-reset" title={item.resetText}>
                      {item.resetText}
                    </span>
                  </div>
                ))}
                {prepared.credits && (
                  <div className="flex h-5 items-center gap-1">
                    <span className="inline-flex items-center justify-center px-1 py-0 rounded text-[10px] font-medium" style={ACCOUNT_USAGE_BADGE_STYLE}>
                      $
                    </span>
                    <span style={{ color: prepared.credits.unlimited ? 'var(--ag-success)' : prepared.credits.balance > 0 ? 'var(--ag-text)' : 'var(--ag-danger)' }}>
                      {prepared.credits.unlimited ? '∞' : `$${Number(prepared.credits.balance).toFixed(2)}`}
                    </span>
                  </div>
                )}
              </div>
              {hasTodayMetricChips && (
                <>
                  <span aria-hidden="true" />
                  <AccountUsageTodayMetricChips
                    accessImageText={prepared.accessImageText}
                    accessRequestsText={prepared.accessRequestsText}
                    accessText={prepared.accessText}
                    accountCostText={prepared.todayAccountCostText}
                    hideAccessLabel={prepared.hideAccessLabel}
                    labels={accountUsageLabels}
                    showImageCount={prepared.showImageCount}
                    tokensText={prepared.todayTokensText}
                    userCostText={prepared.todayUserCostText}
                  />
                </>
              )}
            </div>
          </div>
        );
      },
    },
    {
      key: 'last_used_at',
      title: t('accounts.last_used'),
      width: '88px',
      mobileWidth: '88px',
      align: 'center',
      render: (row) => {
        const meta = rowMetaById.get(row.id);
        if (!meta?.lastUsedRelative) {
          return <span style={{ color: 'var(--ag-text-tertiary)' }}>-</span>;
        }
        return (
          <span className="text-xs" style={{ color: 'var(--ag-text-secondary)' }} title={meta.lastUsedTitle}>
            {meta.lastUsedRelative}
          </span>
        );
      },
    },
    {
      key: 'actions',
      title: t('common.actions'),
      width: '136px',
      mobileWidth: '124px',
      align: 'center',
      render: (row) => (
        <AccountRowActions
          row={row}
          labels={accountActionLabels}
          onEdit={onEditAccount}
          onDelete={onDeleteAccount}
          onTest={onTestAccount}
          onStats={onStatsAccount}
          onRefreshQuota={onRefreshQuota}
          onClearCooldowns={onClearRateLimitMarkers}
        />
      ),
    },
  ], [
    accountActionLabels,
    accountUsageLabels,
    applyQuotaRefreshResult,
    onClearRateLimitMarkers,
    onDeleteAccount,
    onEditAccount,
    onRefreshQuota,
    onStatsAccount,
    onTestAccount,
    onToggleScheduling,
    platformFilter,
    platformName,
    platformsKey,
    queryClient,
    rowMetaById,
    t,
    toast,
  ]);
}
