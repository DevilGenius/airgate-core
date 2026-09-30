import { useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { TimeSeriesChart } from './TimeSeriesChart';
import { buildTokenTrendModel, TOKEN_METRICS, type TokenMetric, type TokenTrendInput } from './tokenTrend';

export function TokenTrendChart({ data, lineLabels, metrics = TOKEN_METRICS }: {
  data: TokenTrendInput[];
  lineLabels?: Record<string, string>;
  metrics?: readonly TokenMetric[];
}) {
  const { t } = useTranslation();
  const model = useMemo(() => buildTokenTrendModel(data, {
    input: t('dashboard.input'), output: t('dashboard.output'),
    cacheCreation: t('dashboard.cache_creation'), cacheRead: t('dashboard.cache_read'),
    cacheRatio: t('dashboard.cache_ratio'), cacheCumulativeRatio: t('dashboard.cache_cumulative_ratio'),
    actual: t('dashboard.actual'), standard: t('dashboard.standard'),
    ...lineLabels,
  }, metrics), [data, lineLabels, metrics, t]);
  return <TimeSeriesChart model={model} label={t('dashboard.token_trend')} />;
}
