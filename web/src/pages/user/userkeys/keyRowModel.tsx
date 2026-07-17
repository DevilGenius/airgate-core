import type { TFunction } from 'i18next';
import type { APIKeyResp, GroupResp } from '../../../shared/types';
import type { MetricChipItem } from '../../../shared/components/MetricChips';
import { formatAPIKeyHint } from '../../../shared/utils/format';
import {
  formatRateMultiplier,
  isValidRateMultiplierValue,
  isValidSellRateValue,
} from '../../../shared/utils/rateMultiplier';

export const API_KEY_AMOUNT_DECIMALS = 3;

export function formatRateValue(rate: number | null | undefined) {
  return formatRateMultiplier(rate);
}

export type UserKeyRowModel = {
  displayStatus: string;
  effectiveRate: number | undefined;
  groupName: string;
  hasGroupRate: boolean;
  hasSellRate: boolean;
  isGroupUnbound: boolean;
  keyHint: string;
  normalizedGroupRate: number | undefined;
  profit: number;
  sellRate: number;
};

// 汇总单行密钥的派生数据，桌面表格与移动端卡片共用，保证两端展示一致。
export function getUserKeyRowModel(
  row: APIKeyResp,
  groupMap: Map<number, GroupResp>,
  userGroupRates: Record<number, number> | undefined,
  t: TFunction,
): UserKeyRowModel {
  const group = row.group_id == null ? null : groupMap.get(row.group_id);
  const isGroupUnbound = row.group_id == null;
  const groupName = isGroupUnbound
    ? t('user_keys.group_unbound')
    : group?.name || `#${row.group_id}`;
  const sellRate = isValidSellRateValue(row.sell_rate ?? null) && row.sell_rate != null ? row.sell_rate : 1;
  const hasSellRate = sellRate !== 1;
  const userOverride = row.group_id == null ? undefined : userGroupRates?.[row.group_id];
  const hasUserOverride = isValidRateMultiplierValue(userOverride ?? null);
  const responseGroupRate = isValidRateMultiplierValue(row.group_rate ?? null)
    ? row.group_rate
    : undefined;
  const groupRate = responseGroupRate ?? (hasUserOverride ? userOverride : group?.rate_multiplier);
  const normalizedGroupRate = isValidRateMultiplierValue(groupRate ?? null) ? groupRate : undefined;
  const hasGroupRate = normalizedGroupRate != null;
  const effectiveRate = hasGroupRate ? normalizedGroupRate * sellRate : undefined;
  const profit = (row.used_quota || 0) - (row.used_quota_actual || 0);
  const isExpired = Boolean(row.expires_at) && new Date(row.expires_at as string) < new Date();
  const displayStatus = isExpired ? 'expired' : row.status;
  const keyHint = formatAPIKeyHint(row.key_prefix);

  return {
    displayStatus,
    effectiveRate,
    groupName,
    hasGroupRate,
    hasSellRate,
    isGroupUnbound,
    keyHint,
    normalizedGroupRate,
    profit,
    sellRate,
  };
}

// 分组列：组名 + 综合倍率 + 分组倍率x销售倍率。
export function KeyGroupRateStack({ model, t }: { model: UserKeyRowModel; t: TFunction }) {
  return (
    <div className="ag-api-key-group-stack">
      <div className="ag-api-key-group-line">
        <span
          className="ag-api-key-group-name-chip inline-flex h-6 min-w-0 max-w-full items-center justify-center gap-1 rounded-[var(--radius)] px-1.5 text-[13px] font-medium leading-none text-text-secondary"
          data-tone={model.isGroupUnbound ? 'warning' : 'default'}
          title={model.groupName}
        >
          {model.groupName}
        </span>
        {model.hasGroupRate ? (
          <span
            className="ag-api-key-effective-rate-chip"
            title={`${t('user_keys.effective_rate_short', '综合倍率')} ${formatRateValue(model.effectiveRate)}`}
          >
            {formatRateValue(model.effectiveRate)}
          </span>
        ) : null}
      </div>
      {(model.hasGroupRate || model.hasSellRate) && (
        <div
          className="ag-api-key-rate-row"
          title={`${t('user_keys.group_sell_rate_short', '分组倍率x销售倍率')} ${formatRateValue(model.normalizedGroupRate)}x${formatRateValue(model.sellRate)}`}
        >
          <span className="ag-api-key-rate-label">
            {t('user_keys.group_sell_rate_short', '分组倍率x销售倍率')}
          </span>
          <span className="ag-api-key-rate-value">
            {formatRateValue(model.normalizedGroupRate)}x{formatRateValue(model.sellRate)}
          </span>
        </div>
      )}
    </div>
  );
}

export function buildQuotaChipItems(row: APIKeyResp, t: TFunction): MetricChipItem[] {
  return [
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
  ];
}

export function buildMarkupChipItems(row: APIKeyResp, model: UserKeyRowModel, t: TFunction): MetricChipItem[] {
  return [
    {
      amount: row.used_quota_actual || 0,
      color: 'default',
      decimals: API_KEY_AMOUNT_DECIMALS,
      dollarTone: 'warning',
      label: t('user_keys.cost_actual', '成本'),
    },
    {
      amount: model.profit,
      color: 'default',
      decimals: API_KEY_AMOUNT_DECIMALS,
      dollarTone: 'success',
      label: t('user_keys.profit', '利润'),
    },
  ];
}

export function buildUsageChipItems(row: APIKeyResp, t: TFunction): MetricChipItem[] {
  return [
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
  ];
}
