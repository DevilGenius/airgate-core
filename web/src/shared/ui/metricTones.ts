export type MetricTone =
  | 'blue'
  | 'gray'
  | 'violet'
  | 'emerald'
  | 'teal'
  | 'amber'
  | 'indigo'
  | 'purple'
  | 'rose'
  | 'stream';

/** 流式请求趋势线使用的强调色，独立于主题成功色，避免与 emerald 混淆。 */
export const STREAM_BLUE = 'oklch(62.04% 0.1950 253.83)';

/**
 * 指标卡片图标徽章的 Tailwind 类名。emerald / stream 不在这里给类名：
 * 它们由 design-token 派生的内联样式着色（见 METRIC_TONE_STYLES），保证在
 * 明暗主题下都与品牌色一致。
 */
export const METRIC_TONE_CLASSES: Record<MetricTone, string> = {
  amber: 'bg-amber-100 text-amber-600 ring-amber-200 dark:bg-amber-400/15 dark:text-amber-300 dark:ring-amber-400/25',
  blue: 'bg-blue-100 text-blue-600 ring-blue-200 dark:bg-blue-400/15 dark:text-blue-300 dark:ring-blue-400/25',
  emerald: '',
  gray: 'bg-zinc-100 text-zinc-600 ring-zinc-200 dark:bg-zinc-400/15 dark:text-zinc-300 dark:ring-zinc-400/25',
  indigo: 'bg-indigo-100 text-indigo-600 ring-indigo-200 dark:bg-indigo-400/15 dark:text-indigo-300 dark:ring-indigo-400/25',
  purple: 'bg-purple-100 text-purple-600 ring-purple-200 dark:bg-purple-400/15 dark:text-purple-300 dark:ring-purple-400/25',
  rose: 'bg-rose-100 text-rose-600 ring-rose-200 dark:bg-rose-400/15 dark:text-rose-300 dark:ring-rose-400/25',
  stream: '',
  teal: 'bg-teal-100 text-teal-600 ring-teal-200 dark:bg-teal-400/15 dark:text-teal-300 dark:ring-teal-400/25',
  violet: 'bg-violet-100 text-violet-600 ring-violet-200 dark:bg-violet-400/15 dark:text-violet-300 dark:ring-violet-400/25',
};

/**
 * emerald / stream 两种"语义色"徽章的内联样式，从 CSS 变量派生而非写死
 * Tailwind 色阶，确保随主题切换正确。
 */
export const METRIC_TONE_STYLES: Partial<Record<MetricTone, import('react').CSSProperties>> = {
  emerald: {
    background: 'color-mix(in srgb, var(--ag-success) 18%, var(--ag-surface))',
    boxShadow: '0 0 0 1px color-mix(in srgb, var(--ag-success) 34%, var(--ag-border)), var(--shadow-sm)',
    color: 'var(--ag-success)',
  },
  stream: {
    background: `color-mix(in srgb, ${STREAM_BLUE} 18%, transparent)`,
    boxShadow: `0 0 0 1px color-mix(in srgb, ${STREAM_BLUE} 34%, transparent), var(--shadow-sm)`,
    color: STREAM_BLUE,
  },
};
