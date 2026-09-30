import { decorativePalette } from '@devilgenius/airgate-theme';
import type { DashboardAPIKeyTrend } from '../types';
import type { TimeSeriesModel } from './timeSeries';

export function topAPIKeyTotalTokens(apiKey: DashboardAPIKeyTrend): number {
  return apiKey.trend.reduce((total, point) => total + point.tokens, 0);
}

export function sortTopAPIKeysByTokens(keys: DashboardAPIKeyTrend[]): DashboardAPIKeyTrend[] {
  return keys.map(key => ({ key, tokens: topAPIKeyTotalTokens(key) }))
    .sort((a, b) => b.tokens - a.tokens || a.key.api_key_id - b.key.api_key_id)
    .map(item => item.key);
}

export function buildAPIKeyTrendModel(keys: DashboardAPIKeyTrend[], pluginLabel: string): TimeSeriesModel {
  const sorted = sortTopAPIKeysByTokens(keys);
  const times = [...new Set(sorted.flatMap(key => key.trend.map(point => point.time)))].sort();
  return {
    times, sortTooltip: true,
    series: sorted.map((key, index) => {
      const points = new Map(key.trend.map(point => [point.time, point]));
      return {
        key: `api-key-${key.api_key_id}`,
        label: key.name || (key.api_key_id > 0 ? `#${key.api_key_id}` : pluginLabel),
        color: decorativePalette[index % decorativePalette.length] ?? 'var(--ag-primary)',
        values: times.map(time => points.get(time)?.tokens ?? 0),
        costs: times.map(time => points.get(time)?.billed_cost ?? 0),
      };
    }),
  };
}
