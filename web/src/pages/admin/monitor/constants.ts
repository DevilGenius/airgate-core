import type { MonitorTimeRangePreset } from './types';

export const DEFAULT_PAGE_SIZE = 20;

export const MONITOR_COLUMN_WIDTHS = {
  time: '100px',
  severity: '116px',
  event: '300px',
  source: '220px',
  subject: '200px',
  detail: '240px',
  status: '116px',
  actions: '96px',
  statusActions: '212px',
};

export const SEVERITY_CLASSES: Record<string, string> = {
  critical: 'bg-black text-white ring-black dark:bg-black dark:text-white dark:ring-zinc-600',
  error: 'bg-rose-100 text-rose-700 ring-rose-200 dark:bg-rose-400/15 dark:text-rose-300 dark:ring-rose-400/25',
  warning: 'bg-amber-100 text-amber-700 ring-amber-200 dark:bg-amber-400/15 dark:text-amber-300 dark:ring-amber-400/25',
  info: 'bg-sky-100 text-sky-700 ring-sky-200 dark:bg-sky-400/15 dark:text-sky-300 dark:ring-sky-400/25',
};

export const STATUS_CLASSES: Record<string, string> = {
  active: 'bg-amber-100 text-amber-700 ring-amber-200 dark:bg-amber-400/15 dark:text-amber-300 dark:ring-amber-400/25',
  ignored: 'bg-zinc-100 text-zinc-700 ring-zinc-200 dark:bg-zinc-400/15 dark:text-zinc-300 dark:ring-zinc-400/25',
  resolved: 'bg-emerald-100 text-emerald-700 ring-emerald-200 dark:bg-emerald-400/15 dark:text-emerald-300 dark:ring-emerald-400/25',
};

export const MONITOR_TIME_RANGE_PRESETS: Array<{ id: MonitorTimeRangePreset; minutes?: number; labelKey: string }> = [
  { id: 'all', labelKey: 'monitor.time_range_all' },
  { id: '5m', minutes: 5, labelKey: 'monitor.time_range_5m' },
  { id: '30m', minutes: 30, labelKey: 'monitor.time_range_30m' },
  { id: '1h', minutes: 60, labelKey: 'monitor.time_range_1h' },
  { id: '6h', minutes: 360, labelKey: 'monitor.time_range_6h' },
  { id: '24h', minutes: 1440, labelKey: 'monitor.time_range_24h' },
  { id: 'custom', labelKey: 'monitor.time_range_custom' },
];
