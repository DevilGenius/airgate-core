import { USAGE_TOKEN_COLORS } from '../constants';
import type { TimeSeriesModel, TooltipDetail, TrendSeries } from './timeSeries';

export const TOKEN_METRICS = ['input', 'output', 'cacheCreation', 'cacheRead', 'cacheRatio', 'cacheCumulativeRatio'] as const;
export type TokenMetric = typeof TOKEN_METRICS[number];
export interface TokenTrendInput {
  time: string;
  input: number;
  output: number;
  cacheRead: number;
  cacheCreation?: number;
  actualCost?: number;
  standardCost?: number;
}

export function buildTokenTrendModel(
  data: TokenTrendInput[],
  labels: Record<string, string>,
  metrics: readonly TokenMetric[] = TOKEN_METRICS,
): TimeSeriesModel {
  let cumulativeCache = 0;
  let cumulativeTotal = 0;
  const points = data.map(point => {
    const cacheCreation = point.cacheCreation ?? 0;
    const cache = point.cacheRead + cacheCreation;
    const total = point.input + point.output + cache;
    cumulativeCache += cache;
    cumulativeTotal += total;
    return { ...point, cacheCreation,
      cacheRatio: total > 0 ? Math.min(100, Math.max(0, cache / total * 100)) : 0,
      cacheCumulativeRatio: cumulativeTotal > 0 ? Math.min(100, Math.max(0, cumulativeCache / cumulativeTotal * 100)) : 0,
    };
  });
  const percentage = (value: number) => `${value.toFixed(1)}%`;
  const series: TrendSeries[] = metrics.map(key => {
    const ratio = key === 'cacheRatio' || key === 'cacheCumulativeRatio';
    return { key, label: labels[key] ?? key, color: USAGE_TOKEN_COLORS[key],
      values: points.map(point => point[key]), axis: ratio ? 1 : 0,
      dashed: ratio || key === 'cacheRead', area: !ratio && key !== 'output',
      format: ratio ? percentage : undefined,
    };
  });
  return {
    times: data.map(point => point.time),
    series,
    axes: metrics.some(key => key === 'cacheRatio' || key === 'cacheCumulativeRatio') ? [{}, { max: 100, format: percentage }] : [{}],
    details: index => {
      const point = data[index];
      const details: TooltipDetail[] = [];
      if (point?.actualCost != null) details.push({ label: labels.actual ?? 'Actual', value: `$${point.actualCost.toFixed(2)}`, costTone: 'actual' });
      if (point?.standardCost != null) details.push({ label: labels.standard ?? 'Standard', value: `$${point.standardCost.toFixed(2)}`, costTone: 'standard' });
      return details;
    },
  };
}
