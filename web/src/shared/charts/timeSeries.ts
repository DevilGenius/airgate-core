import type { EChartsOption, LineSeriesOption } from 'echarts';
import type { ChartTheme } from './BaseChart';

export type ChartValue = number | null;
export interface TrendSeries {
  key: string;
  label: string;
  color: string;
  values: ChartValue[];
  axis?: number;
  dashed?: boolean;
  area?: boolean;
  format?: (value: number) => string;
  costs?: ChartValue[];
}
export interface TrendAxis {
  max?: number;
  format?: (value: number) => string;
}
type TooltipCostTone = 'actual' | 'standard';
export interface TooltipDetail { label: string; value: string; costTone?: TooltipCostTone }
export interface TimeSeriesModel {
  times: string[];
  series: TrendSeries[];
  axes?: TrendAxis[];
  sortTooltip?: boolean;
  details?: (index: number) => TooltipDetail[];
}

export function compactNumber(value: number): string {
  for (const [divisor, suffix] of [[1e9, 'B'], [1e6, 'M'], [1e3, 'K']] as const) {
    if (Math.abs(value) >= divisor) return `${(value / divisor).toFixed(2)}${suffix}`;
  }
  return value.toLocaleString();
}

// 时间桶已经由服务端转换到调用方时区，不能再次按 UTC 解析。
export function chartTimeLabel(value: string): string {
  const [date, time] = value.split(' ');
  const parts = date?.split('-') ?? [];
  const day = parts.length === 3 ? `${parts[1]}/${parts[2]}` : date ?? value;
  return time ? `${day}\n${time.slice(0, 5)}` : day;
}

function escapeHTML(value: string): string {
  return value.replace(/[&<>"']/g, char => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' })[char]!);
}

// 与 CostValue 保持一致：只给美元符号着色，金额数字继承提示框文字颜色。
function tooltipValue(value: string, tone?: TooltipCostTone): string {
  if (!tone || !value.startsWith('$')) return escapeHTML(value);
  const colorClass = tone === 'actual' ? 'text-warning' : 'text-success';
  return `<span class="${colorClass}">$</span>${escapeHTML(value.slice(1))}`;
}

export function tooltipIndex(params: unknown): number {
  const first = Array.isArray(params) ? params[0] : params;
  return first && typeof first === 'object' && 'dataIndex' in first && typeof first.dataIndex === 'number' ? first.dataIndex : -1;
}

export function formatTimeSeriesTooltip(model: TimeSeriesModel, index: number, visible?: Set<string>): string {
  if (index < 0 || index >= model.times.length) return '';
  const rows = model.series.filter(series => !visible || visible.has(series.key));
  if (model.sortTooltip) rows.sort((a, b) => (b.values[index] ?? 0) - (a.values[index] ?? 0));
  const moneyColumn = rows.some(series => series.costs);
  const numberStyle = 'text-align:right;min-width:4.25rem;font-variant-numeric:tabular-nums;white-space:nowrap';
  const cells = rows.map(series => {
    const value = series.values[index];
    const amount = value == null ? '-' : (series.format ?? compactNumber)(value);
    const cost = series.costs?.[index];
    const costCell = moneyColumn ? `<span style="${numberStyle}">${cost == null ? '-' : tooltipValue('$' + cost.toFixed(2), 'actual')}</span>` : '';
    return `<span style="width:7px;height:7px;border-radius:50%;background:${escapeHTML(series.color)}"></span><span>${escapeHTML(series.label)}</span><span style="${numberStyle}">${escapeHTML(amount)}</span>${costCell}`;
  }).join('');
  const details = model.details?.(index) ?? [];
  const footer = details.length ? `<div style="border-top:1px solid var(--ag-border);margin-top:6px;padding-top:6px;display:grid;grid-template-columns:1fr auto;gap:4px 12px">${details.map(detail => `<span>${escapeHTML(detail.label)}</span><span style="${numberStyle}">${tooltipValue(detail.value, detail.costTone)}</span>`).join('')}</div>` : '';
  return `<div style="font-weight:600;margin-bottom:6px">${escapeHTML(model.times[index]!)}</div><div style="display:grid;grid-template-columns:7px minmax(0,1fr) auto${moneyColumn ? ' auto' : ''};align-items:center;gap:5px 10px">${cells}</div>${footer}`;
}

export function timeSeriesOption(model: TimeSeriesModel, theme: ChartTheme): EChartsOption {
  const series = model.series.map(item => ({ ...item, color: theme.color(item.color) }));
  const resolved = { ...model, series };
  const axes = model.axes ?? [{}];
  const labels = new Map(series.map(item => [item.key, item.label]));
  const lines: LineSeriesOption[] = series.map(item => ({
    id: item.key,
    name: item.key,
    type: 'line',
    data: item.values,
    yAxisIndex: item.axis ?? 0,
    smooth: 0.25,
    smoothMonotone: 'x',
    connectNulls: false,
    showSymbol: item.values.filter(value => value != null).length <= 12,
    symbol: 'circle',
    symbolSize: model.times.length === 1 ? 7 : 5,
    lineStyle: { width: item.dashed ? 1.6 : 2.2, type: item.dashed ? 'dashed' : 'solid', color: item.color },
    itemStyle: { color: item.color },
    emphasis: { focus: 'series' },
    areaStyle: item.area ? { color: { type: 'linear', x: 0, y: 0, x2: 0, y2: 1, colorStops: [{ offset: 0, color: item.color }, { offset: 1, color: 'transparent' }] }, opacity: 0.16 } : undefined,
  }));
  return {
    animation: !theme.reducedMotion,
    animationDuration: 240,
    animationDurationUpdate: 180,
    textStyle: { fontFamily: theme.font },
    aria: { enabled: true },
    grid: { top: 40, left: 4, right: 4, bottom: 4, outerBoundsMode: 'same', outerBoundsContain: 'axisLabel' },
    legend: { type: 'scroll', top: 0, left: 0, right: 0, data: series.map(item => item.key), formatter: name => labels.get(name) ?? name, textStyle: { color: theme.muted, fontSize: 11 }, pageTextStyle: { color: theme.muted }, pageIconColor: theme.text, itemWidth: 16, itemHeight: 8, itemGap: 16 },
    tooltip: {
      trigger: 'axis', confine: true, backgroundColor: theme.surface, borderWidth: 0, padding: [10, 14],
      textStyle: { color: theme.text, fontFamily: theme.font, fontSize: 12 },
      extraCssText: 'border-radius:12px;box-shadow:0 6px 24px rgb(0 0 0 / 15%);max-width:100%;',
      axisPointer: { type: 'line', lineStyle: { color: theme.pointer, type: 'dashed', width: 1 } },
      formatter: params => {
        const values = Array.isArray(params) ? params : [params];
        return formatTimeSeriesTooltip(resolved, tooltipIndex(params), new Set(values.map(item => String(item.seriesName))));
      },
    },
    xAxis: { type: 'category', data: model.times, boundaryGap: false, axisLine: { show: false }, axisTick: { show: false }, axisLabel: { color: theme.muted, fontSize: 10, hideOverlap: true, lineHeight: 14, formatter: chartTimeLabel }, axisPointer: { label: { show: false } } },
    yAxis: axes.map((axis, index) => ({ type: 'value', min: 0, max: axis.max, position: index === 0 ? 'left' : 'right', splitNumber: 3, axisLine: { show: false }, axisTick: { show: false }, axisLabel: { color: theme.muted, fontSize: 10, formatter: axis.format ?? compactNumber }, splitLine: { show: index === 0, lineStyle: { color: theme.grid, type: 'dashed' } } })),
    series: lines,
  };
}
