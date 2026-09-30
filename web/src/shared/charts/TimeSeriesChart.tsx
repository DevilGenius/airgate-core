import { useCallback } from 'react';
import { BaseChart, type ChartTheme } from './BaseChart';
import { timeSeriesOption, type TimeSeriesModel } from './timeSeries';

export function TimeSeriesChart({ model, label }: { model: TimeSeriesModel; label: string }) {
  const option = useCallback((theme: ChartTheme) => timeSeriesOption(model, theme), [model]);
  return <BaseChart option={option} label={label} />;
}
