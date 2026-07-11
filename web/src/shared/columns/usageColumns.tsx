import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type CSSProperties, type ReactNode } from 'react';
import { createPortal } from 'react-dom';
import {
  getPluginUsageCostDetail,
  getPluginUsageMetricDetail,
  getPluginUsageModelMeta,
  getUsageCostDetailVersion,
  getUsageMetricDetailVersion,
  getUsageModelMetaVersion,
  subscribeUsageCostDetailChange,
  subscribeUsageMetricDetailChange,
  subscribeUsageModelMetaChange,
} from '../../app/plugin-frontend-registry';
import type { UsageLogResp, CustomerUsageLogResp } from '../types';
import { USAGE_TOKEN_COLORS } from '../constants';
import { CostValue } from '../components/CostValue';
import { formatRateMultiplier } from '../utils/rateMultiplier';

/**
 * 列定义统一使用一个宽松的行类型：管理端拿到的是 UsageLogResp，
 * 而 end customer（API Key 登录）拿到的是 CustomerUsageLogResp（无 input_cost / actual_cost 等字段）。
 * customerScope 列不会读取那些缺失字段。
 */
export type UsageRow = UsageLogResp | CustomerUsageLogResp;

export interface UsageColumnConfig<T extends UsageRow = UsageRow> {
  key: string;
  title: ReactNode;
  width?: string;
  hideOnMobile?: boolean;
  render: (row: T) => ReactNode;
}

const RICH_TOOLTIP_TRIGGER_CLASS = 'flex h-full w-full cursor-default items-center justify-center rounded-[var(--radius)] px-1.5 py-0 text-center transition-colors hover:bg-bg-hover';
const RICH_TOOLTIP_OFFSET_PX = 8;
const RICH_TOOLTIP_VIEWPORT_PADDING_PX = 8;
const RICH_TOOLTIP_WIDTH_PX = 336;
const RICH_TOOLTIP_ESTIMATED_HALF_HEIGHT_PX = 160;

type RichTooltipPlacement = 'left' | 'right';
type RichTooltipPosition = { left: number; top: number; width: number };

function getRichTooltipPosition(trigger: HTMLElement, placement: RichTooltipPlacement): RichTooltipPosition {
  const rect = trigger.getBoundingClientRect();
  const viewportWidth = window.innerWidth;
  const viewportHeight = window.innerHeight;
  const width = Math.min(RICH_TOOLTIP_WIDTH_PX, Math.max(160, viewportWidth - RICH_TOOLTIP_VIEWPORT_PADDING_PX * 4));
  const maxLeft = Math.max(RICH_TOOLTIP_VIEWPORT_PADDING_PX, viewportWidth - width - RICH_TOOLTIP_VIEWPORT_PADDING_PX);
  const preferredLeft = placement === 'left'
    ? rect.left - width - RICH_TOOLTIP_OFFSET_PX
    : rect.right + RICH_TOOLTIP_OFFSET_PX;
  const left = Math.min(Math.max(RICH_TOOLTIP_VIEWPORT_PADDING_PX, preferredLeft), maxLeft);
  const verticalInset = Math.min(
    RICH_TOOLTIP_VIEWPORT_PADDING_PX + RICH_TOOLTIP_ESTIMATED_HALF_HEIGHT_PX,
    Math.max(RICH_TOOLTIP_VIEWPORT_PADDING_PX, viewportHeight / 2),
  );
  const top = Math.min(
    Math.max(verticalInset, rect.top + rect.height / 2),
    Math.max(verticalInset, viewportHeight - verticalInset),
  );

  return { left, top, width };
}

function RichTooltip({
  children,
  content,
  placement = 'right',
}: {
  children: ReactNode;
  content: () => ReactNode;
  placement?: RichTooltipPlacement;
}) {
  const [isOpen, setIsOpen] = useState(false);
  const [position, setPosition] = useState<RichTooltipPosition | null>(null);
  const triggerRef = useRef<HTMLSpanElement>(null);

  const updatePosition = useCallback(() => {
    const trigger = triggerRef.current;
    if (!trigger || typeof window === 'undefined') return;
    setPosition(getRichTooltipPosition(trigger, placement));
  }, [placement]);

  const openTooltip = useCallback(() => {
    updatePosition();
    setIsOpen(true);
  }, [updatePosition]);

  const closeTooltip = useCallback(() => {
    setIsOpen(false);
    setPosition(null);
  }, []);

  useEffect(() => {
    if (!isOpen) return undefined;
    updatePosition();
    window.addEventListener('resize', updatePosition);
    window.addEventListener('scroll', updatePosition, true);
    return () => {
      window.removeEventListener('resize', updatePosition);
      window.removeEventListener('scroll', updatePosition, true);
    };
  }, [isOpen, updatePosition]);

  return (
    <>
      <span
        ref={triggerRef}
        className={RICH_TOOLTIP_TRIGGER_CLASS}
        onMouseEnter={openTooltip}
        onMouseLeave={closeTooltip}
      >
        {children}
      </span>
      {isOpen && position && typeof document !== 'undefined'
        ? createPortal(
          <div
            className="ag-usage-rich-tooltip-content"
            data-placement={placement}
            role="tooltip"
            style={{
              left: position.left,
              top: position.top,
              width: position.width,
            }}
          >
            {content()}
          </div>,
          document.body,
        )
        : null}
    </>
  );
}

function TooltipPanel({
  children,
  subtitle,
  title,
}: {
  children: ReactNode;
  subtitle?: ReactNode;
  title: ReactNode;
}) {
  return (
    <div className="ag-usage-tooltip-panel">
      <div className="ag-usage-tooltip-header">
        <div className="ag-usage-tooltip-title">{title}</div>
        {subtitle ? <div className="ag-usage-tooltip-subtitle">{subtitle}</div> : null}
      </div>
      <div className="ag-usage-tooltip-body">{children}</div>
    </div>
  );
}

function TooltipRow({
  color,
  label,
  strong,
  tone,
  value,
}: {
  color?: string;
  label: ReactNode;
  strong?: boolean;
  tone?: 'accent' | 'info' | 'strong' | 'success' | 'warning';
  value: ReactNode;
}) {
  const toneClass = tone === 'success'
    ? 'text-success'
    : tone === 'warning'
      ? 'text-warning'
      : tone === 'info'
        ? 'text-info'
        : tone === 'accent'
          ? 'text-primary'
          : tone === 'strong'
            ? 'text-text'
            : 'text-text-secondary';

  return (
    <div className="ag-usage-tooltip-row">
      <span className="ag-usage-tooltip-row-label">{label}</span>
      <span
        className={`ag-usage-tooltip-row-value ${strong ? 'font-semibold' : 'font-medium'} ${toneClass}`}
        style={color ? { color } : undefined}
      >
        {value}
      </span>
    </div>
  );
}

function TooltipDivider() {
  return <div className="ag-usage-tooltip-divider" />;
}

const MODEL_META_IMAGE_COLOR = 'rgb(148,163,184)';
const META_CHIP_LOW_COLOR = 'rgb(34,197,94)';
const META_CHIP_MEDIUM_COLOR = 'rgb(59,130,246)';
const META_CHIP_HIGH_COLOR = 'rgb(249,115,22)';
const META_CHIP_XHIGH_COLOR = 'rgb(239,68,68)';
const META_CHIP_MAX_COLOR = 'rgb(148,163,184)';
const META_CHIP_ULTRA_COLOR = 'var(--ag-text)';
const META_CHIP_FALLBACK_COLOR = 'var(--ag-text-secondary)';
const META_CHIP_SERVICE_TIER_COLOR = 'rgb(168,85,247)';
const IMAGE_TIER_1K_MAX_PIXELS = 1536 * 1024;
const IMAGE_TIER_2K_MAX_PIXELS = 2048 * 2048;

const META_CHIP_EFFORT_COLORS: Record<string, string> = {
  low: META_CHIP_LOW_COLOR,
  medium: META_CHIP_MEDIUM_COLOR,
  high: META_CHIP_HIGH_COLOR,
  xhigh: META_CHIP_XHIGH_COLOR,
  max: META_CHIP_MAX_COLOR,
  ultra: META_CHIP_ULTRA_COLOR,
};

const MODEL_META_SLOT_WIDTH_CLASS = 'w-[5.5rem]';

function usagePlatformKey(platform: string): string {
  return platform.trim().toLowerCase();
}

function isClaudeUsagePlatform(platform: string): boolean {
  return usagePlatformKey(platform).includes('claude');
}

function reasoningEffortMetaColor(reasoningEffort: string): string {
  const key = reasoningEffort.trim().toLowerCase().replace(/[\s_-]+/g, '');
  return META_CHIP_EFFORT_COLORS[key] ?? META_CHIP_FALLBACK_COLOR;
}

function MetaChip({
  color,
  dotColor,
  imageTier,
  label,
}: {
  color: string;
  dotColor?: string;
  imageTier?: 'high' | 'low' | 'medium';
  label: string;
}) {
  const style = {
    '--ag-usage-meta-chip-color': color,
    '--ag-usage-meta-chip-dot-color': dotColor ?? color,
    background: `color-mix(in srgb, ${color} 18%, transparent)`,
    boxShadow: `inset 0 0 0 1px color-mix(in srgb, ${color} 34%, transparent)`,
    color,
  } as CSSProperties;

  return (
    <span
      className={[
        'ag-usage-meta-chip',
        dotColor && 'ag-usage-meta-chip--image',
        imageTier && `ag-usage-meta-chip--image-${imageTier}`,
        MODEL_META_SLOT_WIDTH_CLASS,
        'inline-flex h-4 shrink-0 items-center justify-center truncate rounded px-1.5 text-[12px] font-semibold leading-none whitespace-nowrap',
      ].filter(Boolean).join(' ')}
      style={style}
      title={label}
    >
      {label}
    </span>
  );
}

function getImageSizeTier(imageSize: string): 'high' | 'low' | 'medium' {
  const normalized = imageSize.trim().toLowerCase();
  if (normalized.includes('4k')) return 'high';
  if (normalized.includes('2k')) return 'medium';
  if (normalized.includes('1k')) return 'low';

  const dimensions = normalized.match(/\d+(?:\.\d+)?/g)?.map(Number).filter(Number.isFinite) ?? [];
  const [width, height] = dimensions;
  if (width && height) {
    const pixels = width * height;
    if (pixels > IMAGE_TIER_2K_MAX_PIXELS) return 'high';
    if (pixels > IMAGE_TIER_1K_MAX_PIXELS) return 'medium';
  }
  return 'low';
}

function getImageSizeDotColor(imageSize: string): string {
  const tier = getImageSizeTier(imageSize);
  if (tier === 'high') return META_CHIP_HIGH_COLOR;
  if (tier === 'medium') return META_CHIP_MEDIUM_COLOR;
  return META_CHIP_LOW_COLOR;
}

function serviceTierMetaLabel(serviceTier: string): string {
  const normalized = serviceTier.trim().toLowerCase();
  if (normalized === 'fast' || normalized === 'priority' || normalized === 'scale') return 'fast';
  return serviceTier;
}

const HEROUI_BLUE = 'oklch(62.04% 0.1950 253.83)';

const STREAM_CHIP_STYLE: CSSProperties = {
  background: `color-mix(in srgb, ${HEROUI_BLUE} 18%, transparent)`,
  boxShadow: `inset 0 0 0 1px color-mix(in srgb, ${HEROUI_BLUE} 34%, transparent)`,
  color: HEROUI_BLUE,
};
const USAGE_TIME_FORMATTER = new Intl.DateTimeFormat('zh-CN', {
  hour: '2-digit',
  hour12: false,
  minute: '2-digit',
  second: '2-digit',
});
const USAGE_DATE_FORMATTER = new Intl.DateTimeFormat('zh-CN');

/** 单行 token 数据行：固定宽度图标 + 右对齐等宽数字 */
function TokenRow({
  color,
  marker,
  value,
}: {
  color: string;
  marker: 'input' | 'output' | 'cache-read' | 'cache-create';
  value: string;
}) {
  return (
    <div
      className={`ag-usage-token-row ag-usage-token-row--${marker} grid grid-cols-[1rem_minmax(0,1fr)] items-center gap-1`}
      style={{ '--ag-usage-token-color': color } as CSSProperties}
    >
      <span
        className="w-[3.5rem] justify-self-center truncate text-center font-mono text-xs font-semibold tabular-nums leading-none"
        style={{ color }}
      >
        {value}
      </span>
    </div>
  );
}

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

function normalizeUsageKey(value?: string): string {
  return (value || '').trim().toLowerCase().replace(/[\s-]+/g, '_');
}

function metricNumber(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

function firstText(...values: unknown[]): string | undefined {
  for (const value of values) {
    if (typeof value !== 'string') continue;
    const text = value.trim();
    if (text) return text;
  }
  return undefined;
}

function usageMetadataValue(metadata: Record<string, string>, keys: string[]): string | undefined {
  const normalizedKeys = new Set(keys.map(normalizeUsageKey));
  for (const [key, value] of Object.entries(metadata)) {
    if (!normalizedKeys.has(normalizeUsageKey(key))) continue;
    const text = firstText(value);
    if (text) return text;
  }
  return undefined;
}

function usageMetadataNumber(metadata: Record<string, string>, keys: string[]): number {
  const value = usageMetadataValue(metadata, keys);
  if (!value) return 0;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

interface MetricDisplay {
  key: string;
  label: string;
  kind: 'token' | 'image';
  unit: string;
  value: number;
}

function isTotalMetric(metric: MetricDisplay) {
  return metric.key === 'total_tokens';
}

function isReasoningMetric(metric: MetricDisplay) {
  return metric.key === 'reasoning_output_tokens';
}

function isOutputMetric(metric: MetricDisplay) {
  return metric.key === 'output_tokens';
}

function isCacheCreationMetric(metric: MetricDisplay) {
  return metric.key === 'cache_creation_tokens';
}

function isTokenUnit(unit?: string) {
  const normalized = normalizeUsageKey(unit);
  return normalized === 'token' || normalized === 'tokens';
}

function formatMetricNumber(value: number): string {
  return Number.isInteger(value)
    ? value.toLocaleString()
    : value.toLocaleString(undefined, { maximumFractionDigits: 4 });
}

function formatMetricValue(metric: MetricDisplay): string {
  const value = metricNumber(metric.value);
  const formatted = formatMetricNumber(value);
  const unit = metric.unit?.trim();
  if (!unit || isTokenUnit(unit)) return formatted;
  return `${formatted} ${unit}`;
}

function metricColor(metric: MetricDisplay, index: number): string | undefined {
  const key = normalizeUsageKey(metric.key);
  if (key.includes('reasoning')) return USAGE_TOKEN_COLORS.reasoning;
  if (key.includes('input') && !key.includes('cached')) return USAGE_TOKEN_COLORS.input;
  if (key.includes('output')) return USAGE_TOKEN_COLORS.output;
  if (key.includes('cache_read') || key.includes('cached_input')) return USAGE_TOKEN_COLORS.cacheRead;
  if (key.includes('cache_creation')) return USAGE_TOKEN_COLORS.cacheCreation;
  if (metric.kind === 'image') return 'var(--ag-text)';
  return [USAGE_TOKEN_COLORS.input, USAGE_TOKEN_COLORS.output, USAGE_TOKEN_COLORS.cacheRead, USAGE_TOKEN_COLORS.cacheCreation][index % 4];
}

function cacheCreationBreakdownValue(row: UsageRow, total: number): ReactNode {
  const metadata = row.usage_metadata ?? {};
  const cacheCreation5m = usageMetadataNumber(metadata, ['claude.cache_creation_5m_tokens']);
  const cacheCreation1h = usageMetadataNumber(metadata, ['claude.cache_creation_1h_tokens']);
  const parts: Array<[string, number]> = [];
  if (cacheCreation5m > 0) parts.push(['5m', cacheCreation5m]);
  if (cacheCreation1h > 0) parts.push(['1h', cacheCreation1h]);

  if (parts.length === 0) return formatMetricNumber(total);

  return (
    <span className="inline-flex min-w-0 max-w-full items-baseline justify-end gap-1">
      <span className="min-w-0 truncate" style={{ color: USAGE_TOKEN_COLORS.cacheRead }}>
        ({parts.map(([label, value]) => `${label} ${formatMetricNumber(value)}`).join(',')})
      </span>
      <span className="shrink-0">{formatMetricNumber(total)}</span>
    </span>
  );
}

function metricTooltipValue(row: UsageRow, metric: MetricDisplay, reasoningTokens: number): ReactNode {
  if (isCacheCreationMetric(metric)) return cacheCreationBreakdownValue(row, metricNumber(metric.value));

  if (isOutputMetric(metric) && reasoningTokens > 0) {
    return (
      <span className="inline-flex min-w-0 max-w-full items-baseline justify-end gap-1">
        <span className="min-w-0 truncate" style={{ color: USAGE_TOKEN_COLORS.reasoning }}>(推理 {formatMetricNumber(reasoningTokens)})</span>
        <span className="shrink-0">{formatMetricValue(metric)}</span>
      </span>
    );
  }

  return formatMetricValue(metric);
}

function rowMetrics(row: UsageRow): MetricDisplay[] {
  const cacheCreation = (row as UsageLogResp).cache_creation_tokens ?? 0;
  const metrics: MetricDisplay[] = [
    { key: 'input_tokens', label: '输入 Token', kind: 'token', unit: 'token', value: row.input_tokens },
    { key: 'output_tokens', label: '输出 Token', kind: 'token', unit: 'token', value: row.output_tokens },
    { key: 'cached_input_tokens', label: '缓存读取 Token', kind: 'token', unit: 'token', value: row.cached_input_tokens },
    { key: 'cache_creation_tokens', label: '缓存创建 Token', kind: 'token', unit: 'token', value: cacheCreation },
  ];
  const imageCount = usageMetadataNumber(row.usage_metadata ?? {}, ['openai.image.count']);
  if (imageCount > 0) {
    metrics.push({ key: 'images', label: '图片数量', kind: 'image', unit: 'image', value: imageCount });
  }
  return metrics.filter((metric) => metric.value > 0 || metric.key === 'input_tokens' || metric.key === 'output_tokens');
}

function buildUsageRecordContext(row: UsageRow, customerScope: boolean) {
  const usageMetadata = row.usage_metadata ?? {};
  const serviceTier = firstText(row.service_tier);
  const reasoningEffort = firstText((row as Partial<UsageLogResp>).reasoning_effort);
  const reasoningTokens = (row as Partial<UsageLogResp>).reasoning_output_tokens;

  const ctx: Record<string, unknown> = {
    record: row,
    customerScope,
    usage_metadata: usageMetadata,
    // 常用的行级别字段做扁平化，方便插件扩展渲染器直接取值。
    model: row.model,
    platform: row.platform,
    service_tier: serviceTier,
    endpoint: row.endpoint,
    stream: row.stream,
    created_at: row.created_at,
  };

  if (reasoningEffort) ctx.reasoning_effort = reasoningEffort;
  if (typeof reasoningTokens === 'number' && reasoningTokens > 0) {
    ctx.reasoning_output_tokens = reasoningTokens;
  }

  return ctx;
}

function buildCostDetailContext(row: UsageRow, adminView: boolean, customerScope = false) {
  const ctx = buildUsageRecordContext(row, customerScope);
  ctx.adminView = adminView;
  return ctx;
}

function GenericMetricDetail({ row, t }: { row: UsageRow; t: TFunction }) {
  const allMetrics = rowMetrics(row);
  const reasoningTokens = (row as Partial<UsageLogResp>).reasoning_output_tokens ?? 0;
  const metrics = allMetrics.filter((metric) => (
    !isTotalMetric(metric)
    && !isReasoningMetric(metric)
    && (metricNumber(metric.value) > 0 || metric.key === 'input_tokens' || metric.key === 'output_tokens' || (isOutputMetric(metric) && reasoningTokens > 0))
  ));
  const tokenTotal =
    row.input_tokens + row.output_tokens + row.cached_input_tokens + ((row as UsageLogResp).cache_creation_tokens ?? 0);
  const shouldShowTokenTotal = tokenTotal > 0 || metrics.some((metric) => metric.kind === 'token');

  return (
    <TooltipPanel title={t('usage.metric_detail', '计量明细')} subtitle={row.model}>
      {metrics.map((metric, index) => (
        <TooltipRow
          key={metric.key || `${metric.label}:${index}`}
          label={metric.label || metric.key || t('usage.metric', '计量')}
          value={metricTooltipValue(row, metric, reasoningTokens)}
          color={metricColor(metric, index)}
        />
      ))}
      {shouldShowTokenTotal && (
        <>
          <TooltipDivider />
          <TooltipRow label={t('usage.total_tokens')} value={Number(tokenTotal).toLocaleString()} tone="strong" strong />
        </>
      )}
    </TooltipPanel>
  );
}

function userVisibleCost(row: UsageRow) {
  return 'cost' in row ? row.cost : row.billed_cost;
}

function FallbackCostDetail({ row, t, adminView }: { row: UsageRow; t: TFunction; adminView: boolean }) {
  const cost = userVisibleCost(row);
  const effectiveRate = Number.isFinite(row.effective_rate) ? row.effective_rate : undefined;
  const fullRow = 'actual_cost' in row ? row : undefined;
  const hasRateInfo = !!row.service_tier || (adminView ? !!fullRow : effectiveRate !== undefined);

  return (
    <TooltipPanel title={t('usage.cost_detail')} subtitle={row.model}>
      <TooltipRow label={t('usage.input_cost')} value={`$${row.input_cost.toFixed(6)}`} />
      {row.cached_input_cost > 0 && (
        <TooltipRow label={t('usage.cached_input_cost')} value={`$${row.cached_input_cost.toFixed(6)}`} />
      )}
      {row.cache_creation_cost > 0 && (
        <TooltipRow label={t('usage.cache_creation_cost', '缓存创建成本')} value={`$${row.cache_creation_cost.toFixed(6)}`} />
      )}
      <TooltipRow label={t('usage.output_cost')} value={`$${row.output_cost.toFixed(6)}`} />
      {row.input_price > 0 && (
        <TooltipRow label={t('usage.input_unit_price')} value={`$${row.input_price.toFixed(4)} / 1M Token`} />
      )}
      {row.cached_input_price > 0 && (
        <TooltipRow label={t('usage.cached_input_unit_price', '缓存读取单价')} value={`$${row.cached_input_price.toFixed(4)} / 1M Token`} />
      )}
      {row.cache_creation_price > 0 && (
        <TooltipRow label={t('usage.cache_creation_unit_price', '缓存创建单价')} value={`$${row.cache_creation_price.toFixed(4)} / 1M Token`} />
      )}
      {row.output_price > 0 && (
        <TooltipRow label={t('usage.output_unit_price')} value={`$${row.output_price.toFixed(4)} / 1M Token`} />
      )}
      <TooltipDivider />
      {row.service_tier && (
        <TooltipRow label={t('usage.service_tier')} value={<span className="capitalize">{row.service_tier}</span>} />
      )}
      {adminView && fullRow && (
        <TooltipRow label={t('usage.rate_multiplier')} value={`${formatRateMultiplier(fullRow.rate_multiplier)}x`} />
      )}
      {adminView && fullRow && Number.isFinite(fullRow.account_rate_multiplier) && (
        <TooltipRow label={t('usage.account_rate', '上游倍率')} value={`${formatRateMultiplier(fullRow.account_rate_multiplier)}x`} />
      )}
      {adminView && fullRow && Number.isFinite(fullRow.sell_rate) && fullRow.sell_rate !== 1 && (
        <TooltipRow label={t('usage.sell_rate', '销售倍率')} value={`${formatRateMultiplier(fullRow.sell_rate)}x`} />
      )}
      {!adminView && effectiveRate !== undefined && (
        <TooltipRow label={t('usage.effective_rate', '倍率')} value={`${formatRateMultiplier(effectiveRate)}x`} />
      )}
      {hasRateInfo && <TooltipDivider />}
      <TooltipRow label={t('usage.original_cost')} value={<CostValue value={row.total_cost} decimals={6} tone="standard" />} />
      {adminView && fullRow && (
        <TooltipRow label={t('usage.account_cost', '上游计费')} value={<CostValue value={fullRow.account_cost} decimals={6} />} />
      )}
      {adminView && fullRow && fullRow.billed_cost !== fullRow.actual_cost && (
        <>
          <TooltipRow label={t('usage.user_charged', '余额扣费')} value={<CostValue value={fullRow.actual_cost} decimals={6} />} />
          <TooltipRow label={t('usage.profit', '利润')} value={<CostValue value={fullRow.billed_cost - fullRow.actual_cost} decimals={6} tone="success" />} />
        </>
      )}
      <TooltipRow label={t('usage.billed_cost', '密钥计费')} value={<CostValue value={cost} decimals={6} tone="actual" />} tone="strong" />
    </TooltipPanel>
  );
}

/** 管理端保留完整成本分析；普通用户使用详细明细并仅展示最终倍率。 */
function buildResellerCostColumn(t: TFunction, adminView: boolean): UsageColumnConfig<UsageRow> {
  return {
    key: 'cost',
    title: t('usage.cost'),
    width: '140px',
    render: (raw) => {
      const row = raw as UsageLogResp;
      const PluginUsageCostDetail = getPluginUsageCostDetail(row.platform);
      return (
        <RichTooltip
          placement="right"
          content={() => (
            PluginUsageCostDetail ? (
              <PluginUsageCostDetail
                recordId={row.id}
                context={buildCostDetailContext(row, adminView)}
              />
            ) : (
              <FallbackCostDetail row={row} t={t} adminView={adminView} />
            )
          )}
        >
          <div className="flex w-full flex-col items-center font-mono text-center text-xs">
            {row.billed_cost !== row.actual_cost ? (
              <div className="text-[15px] font-semibold leading-none text-text">
                <CostValue value={row.billed_cost} decimals={6} tone="warning" />
              </div>
            ) : (
              <div className="text-[15px] font-semibold leading-none text-text">
                <CostValue value={row.actual_cost} decimals={6} tone="warning" />
              </div>
            )}
          </div>
        </RichTooltip>
      );
    },
  };
}

/** End customer 复用普通用户的详细费用组件，仅传递最终倍率。 */
function buildCustomerCostColumn(t: TFunction): UsageColumnConfig<UsageRow> {
  return {
    key: 'cost',
    title: t('usage.cost'),
    width: '140px',
    render: (raw) => {
      const cost = userVisibleCost(raw);
      const PluginUsageCostDetail = getPluginUsageCostDetail(raw.platform);
      return (
        <RichTooltip
          placement="right"
          content={() => (
            PluginUsageCostDetail ? (
              <PluginUsageCostDetail
                recordId={raw.id}
                context={buildCostDetailContext(raw, false, true)}
              />
            ) : (
              <FallbackCostDetail row={raw} t={t} adminView={false} />
            )
          )}
        >
          <div className="flex w-full flex-col items-center font-mono text-center text-xs">
            <div className="text-[15px] font-semibold leading-none text-text">
              <CostValue value={cost} decimals={6} tone="warning" />
            </div>
          </div>
        </RichTooltip>
      );
    },
  };
}

/**
 * 使用记录表格的共享列定义。
 * 管理端和用户端共用，管理端额外在前面插入 user / api_key / account 列。
 *
 * customerScope=true 只切换 end customer 视角的成本列，避免读取后端剥离过的字段。
 * token 计量列和 tooltip 使用同一套可展示字段渲染，保留调用方传入的 scope 语义。
 */
export function useUsageColumns(opts?: { customerScope?: boolean; adminView?: boolean }): UsageColumnConfig<UsageRow>[] {
  const { t } = useTranslation();
  const customerScope = opts?.customerScope ?? false;
  const adminView = opts?.adminView ?? true;
  const metricDetailVersion = useSyncExternalStore(subscribeUsageMetricDetailChange, getUsageMetricDetailVersion);
  const costDetailVersion = useSyncExternalStore(subscribeUsageCostDetailChange, getUsageCostDetailVersion);
  const modelMetaVersion = useSyncExternalStore(subscribeUsageModelMetaChange, getUsageModelMetaVersion);

  return useMemo(() => {
    const costColumn = customerScope ? buildCustomerCostColumn(t) : buildResellerCostColumn(t, adminView);

    return [
    {
      key: 'created_at',
      title: t('usage.time'),
      width: '142px',
      render: (row) => {
        const date = new Date(row.created_at);
        const timeLabel = USAGE_TIME_FORMATTER.format(date);
        const dateLabel = USAGE_DATE_FORMATTER.format(date);
        const fullLabel = `${dateLabel} ${timeLabel}`;

        return (
          <div className="flex min-w-0 items-center gap-1.5 font-mono text-xs" title={fullLabel}>
            <span className="shrink-0 font-mono text-[13px] font-medium text-text">
              {timeLabel}
            </span>
            <span className="hidden shrink-0 font-light text-text-tertiary xl:inline">
              {dateLabel}
            </span>
          </div>
        );
      },
    },
    {
      key: 'model',
      title: t('usage.model'),
      width: '220px',
      render: (row) => {
        const PluginUsageModelMeta = getPluginUsageModelMeta(row.platform);
        const metaContext = buildUsageRecordContext(row, customerScope);
        const fallbackMeta = (() => {
          const reasoningEffort = typeof metaContext.reasoning_effort === 'string' ? metaContext.reasoning_effort.trim() : '';
          const reasoningEffortMeta = reasoningEffort ? (
            <MetaChip
              color={reasoningEffortMetaColor(reasoningEffort)}
              label={reasoningEffort}
            />
          ) : null;

          if (PluginUsageModelMeta) {
            return isClaudeUsagePlatform(row.platform) ? reasoningEffortMeta : null;
          }

          const imageSize = usageMetadataValue(row.usage_metadata ?? {}, ['openai.image.size']) ?? '';
          if (imageSize) {
            const imageTier = getImageSizeTier(imageSize);
            return (
              <MetaChip
                color={MODEL_META_IMAGE_COLOR}
                dotColor={getImageSizeDotColor(imageSize)}
                imageTier={imageTier}
                label={imageSize}
              />
            );
          }

          if (reasoningEffortMeta) {
            return reasoningEffortMeta;
          }

          const serviceTier = typeof metaContext.service_tier === 'string' ? metaContext.service_tier : '';
          if (!serviceTier) return null;
          return (
            <MetaChip
              color={META_CHIP_SERVICE_TIER_COLOR}
              label={serviceTierMetaLabel(serviceTier)}
            />
          );
        })();

        return (
          <div className="ag-usage-model-cell grid w-full min-w-0 grid-cols-[5.5rem_minmax(0,1fr)] items-center gap-2 text-left">
            <div className={`ag-usage-model-meta-slot ${MODEL_META_SLOT_WIDTH_CLASS} flex h-4 shrink-0 items-center justify-center overflow-hidden`}>
              {fallbackMeta ?? (PluginUsageModelMeta ? (
                <PluginUsageModelMeta
                  recordId={row.id}
                  context={metaContext}
                />
              ) : null)}
            </div>
            <span className="min-w-0 truncate text-sm font-medium leading-none text-text" title={row.model}>
              {row.model}
            </span>
          </div>
        );
      },
    },
    {
      key: 'tokens',
      title: t('usage.metrics', '计量'),
      width: '220px',
      render: (row) => {
        const PluginUsageMetricDetail = getPluginUsageMetricDetail(row.platform);
        const metrics = rowMetrics(row);
        const inputTokens = row.input_tokens;
        const outputTokens = row.output_tokens;
        const cacheReadTokens = row.cached_input_tokens;
        const cacheCreationTokens = (row as UsageLogResp).cache_creation_tokens ?? 0;
        const total = inputTokens + outputTokens + cacheReadTokens + cacheCreationTokens;
        const hasCacheRead = cacheReadTokens > 0;
        const hasCacheWrite = cacheCreationTokens > 0;
        const tokenSummaryVisible = inputTokens > 0 || outputTokens > 0 || hasCacheRead || hasCacheWrite || total > 0;
        const primaryMetric = metrics.find((metric) => metricNumber(metric.value) > 0 && !isTotalMetric(metric));
        return (
          <RichTooltip
            placement="left"
            content={() => (
              PluginUsageMetricDetail ? (
                <PluginUsageMetricDetail
                  recordId={row.id}
                  context={buildUsageRecordContext(row, customerScope)}
                />
              ) : (
                <GenericMetricDetail row={row} t={t} />
              )
            )}
          >
            {tokenSummaryVisible ? (
              <div className="ag-usage-token-summary mx-auto grid h-full max-h-[var(--ag-usage-table-row-height)] grid-cols-[minmax(0,8.75rem)_4.75rem] items-center justify-center gap-2 overflow-visible px-1">
                <div className="grid min-w-0 grid-cols-2 gap-x-2 gap-y-px">
                  <TokenRow
                    color={USAGE_TOKEN_COLORS.input}
                    marker="input"
                    value={fmtNum(inputTokens)}
                  />
                  <TokenRow
                    color={USAGE_TOKEN_COLORS.output}
                    marker="output"
                    value={fmtNum(outputTokens)}
                  />
                  {(hasCacheRead || hasCacheWrite) ? (
                    <>
                      {hasCacheRead ? (
                        <TokenRow
                          color={USAGE_TOKEN_COLORS.cacheRead}
                          marker="cache-read"
                          value={fmtNum(cacheReadTokens)}
                        />
                      ) : <div />}
                      {hasCacheWrite ? (
                        <TokenRow
                          color={USAGE_TOKEN_COLORS.cacheCreation}
                          marker="cache-create"
                          value={fmtNum(cacheCreationTokens)}
                        />
                      ) : <div />}
                    </>
                  ) : null}
                </div>
                <div className="w-[4.75rem] text-center font-mono text-base font-semibold tabular-nums leading-none text-text">
                  {fmtNum(total)}
                </div>
              </div>
            ) : (
              <div className="flex h-full min-w-0 flex-col items-center justify-center px-2 text-center">
                <span className="max-w-full truncate text-[11px] leading-none text-text-tertiary" title={primaryMetric?.label || primaryMetric?.key}>
                  {primaryMetric?.label || primaryMetric?.key || '-'}
                </span>
                <span className="mt-1 max-w-full truncate font-mono text-sm font-semibold leading-none text-text">
                  {primaryMetric ? formatMetricValue(primaryMetric) : '-'}
                </span>
              </div>
            )}
          </RichTooltip>
        );
      },
    },
    costColumn,
    {
      key: 'stream',
      title: t('usage.type'),
      width: '72px',
      hideOnMobile: true,
      render: (row) => (
        <span
          className="inline-flex h-6 min-w-0 items-center justify-center rounded-[var(--radius)] px-1.5 text-[13px] font-medium leading-none text-text-secondary"
          style={row.stream ? STREAM_CHIP_STYLE : undefined}
        >
          {row.stream ? t('usage.type_stream') : t('usage.type_sync')}
        </span>
      ),
    },
    {
      key: 'first_token_ms',
      title: t('usage.first_token'),
      width: '78px',
      hideOnMobile: true,
      render: (row) => (
        <span className="block text-center font-mono text-[13px] text-text-secondary">
          {row.first_token_ms > 0 ? (row.first_token_ms >= 1000 ? `${(row.first_token_ms / 1000).toFixed(2)}s` : `${row.first_token_ms}ms`) : '-'}
        </span>
      ),
    },
    {
      key: 'duration_ms',
      title: t('usage.duration'),
      width: '76px',
      hideOnMobile: true,
      render: (row) => (
        <span className="block text-center font-mono text-[13px] text-text-secondary">
          {row.duration_ms >= 1000 ? `${(row.duration_ms / 1000).toFixed(2)}s` : `${row.duration_ms}ms`}
        </span>
      ),
    },
    ];
  }, [adminView, costDetailVersion, customerScope, metricDetailVersion, modelMetaVersion, t]);
}
