import { useTranslation } from 'react-i18next';
import { Badge } from '../components/Badge';
import { HoverCard } from '../components/HoverCard';
import type { Column } from '../components/Table';
import type { UsageLogResp } from '../types';

/** 大数字友好显示：33518599 -> "33.52M"，1234 -> "1,234" */
export function fmtNum(n: number): string {
  if (n >= 1_000_000_000) return `${(n / 1_000_000_000).toFixed(2)}B`;
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(2)}M`;
  if (n >= 10_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}

/** 格式化费用 */
export function fmtCost(n: number): string {
  if (n >= 1000) return `$${(n / 1000).toFixed(2)}K`;
  return `$${n.toFixed(2)}`;
}

/**
 * 使用记录表格的共享列定义。
 * 管理端和用户端共用，管理端额外在前面插入 user / api_key / account 列。
 */
export function useUsageColumns(): Column<UsageLogResp>[] {
  const { t } = useTranslation();

  return [
    {
      key: 'created_at',
      title: t('usage.time'),
      width: '168px',
      render: (row) => (
        <span className="text-text-secondary">
          {new Date(row.created_at).toLocaleString('zh-CN')}
        </span>
      ),
    },
    {
      key: 'model',
      title: t('usage.model'),
      width: '220px',
      render: (row) => (
        <span className="block max-w-full truncate text-text" title={row.model}>
          {row.model}
        </span>
      ),
    },
    {
      key: 'tokens',
      title: 'TOKEN',
      width: '160px',
      render: (row) => {
        const total = row.input_tokens + row.output_tokens + row.cached_input_tokens;
        return (
          <HoverCard
            content={
              <>
                <div className="text-xs font-semibold text-text mb-2">Token {t('usage.detail')}</div>
                <div className="space-y-1 text-xs font-mono">
                  <div className="flex justify-between gap-6">
                    <span className="text-text-tertiary">{t('usage.input_tokens')}</span>
                    <span className="text-text-secondary">{row.input_tokens.toLocaleString()}</span>
                  </div>
                  <div className="flex justify-between gap-6">
                    <span className="text-text-tertiary">{t('usage.output_tokens')}</span>
                    <span className="text-text-secondary">{row.output_tokens.toLocaleString()}</span>
                  </div>
                  {row.cached_input_tokens > 0 && (
                    <div className="flex justify-between gap-6">
                      <span className="text-text-tertiary">{t('usage.cached_input_tokens')}</span>
                      <span className="text-text-secondary">{row.cached_input_tokens.toLocaleString()}</span>
                    </div>
                  )}
                  <div className="flex justify-between gap-6 pt-1 border-t border-glass-border">
                    <span className="text-text-tertiary">{t('usage.total_tokens')}</span>
                    <span className="text-primary font-semibold">{total.toLocaleString()}</span>
                  </div>
                </div>
              </>
            }
          >
            <div className="font-mono text-xs flex items-center gap-1.5">
              <span className="text-emerald-400">↓ {row.input_tokens.toLocaleString()}</span>
              <span className="text-sky-400">↑ {row.output_tokens.toLocaleString()}</span>
            </div>
            {row.cached_input_tokens > 0 && (
              <div className="text-[11px] font-mono text-text-tertiary">
                ⊕ {fmtNum(row.cached_input_tokens)}
              </div>
            )}
          </HoverCard>
        );
      },
    },
    {
      key: 'cost',
      title: t('usage.cost'),
      width: '140px',
      render: (row) => {
        return (
          <HoverCard
            content={
              <>
                <div className="text-xs font-semibold text-text mb-2">{t('usage.cost_detail')}</div>
                <div className="space-y-1 text-xs font-mono">
                  <div className="flex justify-between gap-6">
                    <span className="text-text-tertiary">{t('usage.input_cost')}</span>
                    <span className="text-text-secondary">${row.input_cost.toFixed(6)}</span>
                  </div>
                  <div className="flex justify-between gap-6">
                    <span className="text-text-tertiary">{t('usage.output_cost')}</span>
                    <span className="text-text-secondary">${row.output_cost.toFixed(6)}</span>
                  </div>
                  {row.input_price > 0 && (
                    <div className="flex justify-between gap-6">
                      <span className="text-text-tertiary">{t('usage.input_unit_price')}</span>
                      <span className="text-text-secondary">${row.input_price.toFixed(4)} / 1M Token</span>
                    </div>
                  )}
                  {row.output_price > 0 && (
                    <div className="flex justify-between gap-6">
                      <span className="text-text-tertiary">{t('usage.output_unit_price')}</span>
                      <span className="text-text-secondary">${row.output_price.toFixed(4)} / 1M Token</span>
                    </div>
                  )}
                  {row.cached_input_cost > 0 && (
                    <div className="flex justify-between gap-6">
                      <span className="text-text-tertiary">{t('usage.cached_input_cost')}</span>
                      <span className="text-text-secondary">${row.cached_input_cost.toFixed(6)}</span>
                    </div>
                  )}
                  <div className="my-1 border-t border-glass-border" />
                  {row.service_tier && (
                    <div className="flex justify-between gap-6">
                      <span className="text-text-tertiary">{t('usage.service_tier')}</span>
                      <span className="text-text-secondary capitalize">{row.service_tier}</span>
                    </div>
                  )}
                  <div className="flex justify-between gap-6">
                    <span className="text-text-tertiary">{t('usage.rate_multiplier')}</span>
                    <span className="text-text-secondary">{row.rate_multiplier.toFixed(2)}x</span>
                  </div>
                  <div className="flex justify-between gap-6">
                    <span className="text-text-tertiary">{t('usage.account_rate_multiplier')}</span>
                    <span className="text-text-secondary">{row.account_rate_multiplier.toFixed(2)}x</span>
                  </div>
                  <div className="my-1 border-t border-glass-border" />
                  <div className="flex justify-between gap-6">
                    <span className="text-text-tertiary">{t('usage.original_cost')}</span>
                    <span className="text-text-secondary">${row.total_cost.toFixed(6)}</span>
                  </div>
                  <div className="flex justify-between gap-6">
                    <span className="text-text-tertiary">{t('usage.user_charged')}</span>
                    <span className="text-text-secondary">${row.actual_cost.toFixed(6)}</span>
                  </div>
                  <div className="flex justify-between gap-6">
                    <span className="text-text-tertiary">{t('usage.account_billed')}</span>
                    <span className="text-primary font-semibold">${row.total_cost.toFixed(6)}</span>
                  </div>
                </div>
              </>
            }
          >
            <div className="font-mono text-xs text-right">
              <div className="text-text">${row.actual_cost.toFixed(6)}</div>
              {row.actual_cost !== row.total_cost && (
                <div className="text-text-tertiary">A ${row.total_cost.toFixed(6)}</div>
              )}
            </div>
          </HoverCard>
        );
      },
    },
    {
      key: 'stream',
      title: t('usage.type'),
      width: '84px',
      hideOnMobile: true,
      render: (row) => (
        <Badge variant={row.stream ? 'info' : 'default'}>
          {row.stream ? t('usage.type_stream') : t('usage.type_sync')}
        </Badge>
      ),
    },
    {
      key: 'first_token_ms',
      title: t('usage.first_token'),
      width: '96px',
      hideOnMobile: true,
      render: (row) => (
        <span className="font-mono text-xs text-text-secondary">
          {row.first_token_ms > 0 ? (row.first_token_ms >= 1000 ? `${(row.first_token_ms / 1000).toFixed(2)}s` : `${row.first_token_ms}ms`) : '-'}
        </span>
      ),
    },
    {
      key: 'duration_ms',
      title: t('usage.duration'),
      width: '96px',
      hideOnMobile: true,
      render: (row) => (
        <span className="font-mono text-xs">
          {row.duration_ms >= 1000 ? `${(row.duration_ms / 1000).toFixed(2)}s` : `${row.duration_ms}ms`}
        </span>
      ),
    },
  ];
}
