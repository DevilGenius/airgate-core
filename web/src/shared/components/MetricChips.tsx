import type { ClipboardEvent } from 'react';

type MetricChipColor = 'default' | 'warning' | 'success' | 'accent' | 'danger';

export type MetricChipItem = {
  amount?: number;
  color: MetricChipColor;
  decimals?: number;
  dollarTone?: MetricChipColor;
  highlightDollar?: boolean;
  label: string;
  mutedWhenZero?: boolean;
  value?: string;
};

function formatMoneyAmount(value: number, decimals = 4) {
  return (Number.isFinite(value) ? value : 0).toFixed(decimals);
}

function formatMetricTitleValue(item: MetricChipItem) {
  if (item.amount != null) return `$${formatMoneyAmount(item.amount, item.decimals)}`;
  return item.value ?? '';
}

type MetricChipProps = MetricChipItem & {
  copyText: string;
};

function getSelectedCopyItems(root: HTMLElement) {
  const selection = window.getSelection();
  if (!selection || selection.isCollapsed) return [];

  return Array.from(root.querySelectorAll<HTMLElement>('[data-metric-copy-text]'))
    .filter((element) => {
      for (let index = 0; index < selection.rangeCount; index += 1) {
        if (selection.getRangeAt(index).intersectsNode(element)) return true;
      }
      return false;
    })
    .map((element) => element.dataset.metricCopyText)
    .filter((text): text is string => Boolean(text));
}

function MetricChip({ amount, color, copyText, decimals, dollarTone, highlightDollar, label, mutedWhenZero, value }: MetricChipProps) {
  const amountText = amount == null ? null : formatMoneyAmount(amount, decimals);
  const isMutedZero = mutedWhenZero && amount === 0;
  const chipClassName = [
    'ag-metric-chip',
    isMutedZero ? 'ag-metric-chip--zero' : '',
  ].filter(Boolean).join(' ');
  const effectiveDollarTone = dollarTone ?? (highlightDollar ? 'warning' : undefined);
  const dollarClassName = [
    'ag-metric-dollar',
    effectiveDollarTone ? `ag-metric-dollar--${effectiveDollarTone}` : '',
  ].filter(Boolean).join(' ');

  return (
    <span className={chipClassName} data-tone={isMutedZero ? 'default' : color}>
      <span className="ag-metric-chip-content" data-metric-copy-text={copyText}>
        <span className="ag-metric-chip-label">{label}</span>
        <span className="ag-metric-chip-value">
          {amountText == null ? (
            value === '∞' ? <span className="ag-metric-infinity">{value}</span> : value
          ) : (
            <>
              <span className={dollarClassName}>$</span>
              <span>{amountText}</span>
            </>
          )}
        </span>
      </span>
    </span>
  );
}

export function MetricChips({
  className,
  items,
}: {
  className?: string;
  items: MetricChipItem[];
}) {
  const title = items
    .map((item) => `${item.label} ${formatMetricTitleValue(item)}`)
    .join(' / ');
  const handleCopy = (event: ClipboardEvent<HTMLDivElement>) => {
    const copyItems = getSelectedCopyItems(event.currentTarget);
    if (copyItems.length === 0) return;

    event.clipboardData.setData('text/plain', copyItems.join(' / '));
    event.preventDefault();
  };

  return (
    <div className={`ag-metric-chips ${className ?? ''}`} onCopy={handleCopy} title={title}>
      {items.map((item, idx) => (
        <MetricChip key={`${idx}-${item.label}`} {...item} copyText={`${item.label} ${formatMetricTitleValue(item)}`} />
      ))}
    </div>
  );
}
