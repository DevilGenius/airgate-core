import { MONITOR_TIME_RANGE_PRESETS } from './constants';
import type { MonitorTimeRangePreset } from './types';

const MONITOR_RANGE_TIME_FORMATTER = new Intl.DateTimeFormat('zh-CN', {
  month: '2-digit',
  day: '2-digit',
  hour: '2-digit',
  hour12: false,
  minute: '2-digit',
});

function pad2(value: number): string {
  return String(value).padStart(2, '0');
}

export function formatDateTimeInputValue(value?: string): string {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  return [
    date.getFullYear(),
    '-',
    pad2(date.getMonth() + 1),
    '-',
    pad2(date.getDate()),
    'T',
    pad2(date.getHours()),
    ':',
    pad2(date.getMinutes()),
    ':',
    pad2(date.getSeconds()),
  ].join('');
}

export function localDateTimeInputToISOString(value: string): string | undefined {
  const trimmed = value.trim();
  if (!trimmed) return undefined;
  const date = new Date(trimmed);
  if (Number.isNaN(date.getTime())) return undefined;
  return dateToMonitorISOString(date);
}

export function dateToMonitorISOString(date: Date): string {
  return date.toISOString().replace(/\.\d{3}Z$/, 'Z');
}

export function presetTimeRange(preset: MonitorTimeRangePreset): { from?: string; to?: string } {
  const config = MONITOR_TIME_RANGE_PRESETS.find((item) => item.id === preset);
  if (!config?.minutes) return {};
  const to = new Date();
  const from = new Date(to.getTime() - config.minutes * 60_000);
  return {
    from: dateToMonitorISOString(from),
    to: dateToMonitorISOString(to),
  };
}

export function monitorRangeLabel(from?: string, to?: string): string {
  const labels = [from, to].map((value) => {
    if (!value) return '';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? '' : MONITOR_RANGE_TIME_FORMATTER.format(date);
  });
  return labels.filter(Boolean).join(' - ');
}
