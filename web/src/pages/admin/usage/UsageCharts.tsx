import { useMemo } from 'react';
import { TokenTrendChart } from '../../../shared/charts/TokenTrendChart';
import type { UsageTrendBucket } from '../../../shared/types';

export function UsageTokenTrendChart({ data, lineLabels }: { data: UsageTrendBucket[]; lineLabels: Record<string, string> }) {
  const points = useMemo(() => data.map(point => ({
    time: point.time, input: point.input_tokens, output: point.output_tokens,
    cacheRead: point.cache_read, cacheCreation: point.cache_creation,
    actualCost: point.actual_cost, standardCost: point.standard_cost,
  })), [data]);
  return <TokenTrendChart data={points} lineLabels={lineLabels} />;
}
