import { useEffect, useRef } from 'react';
import { LineChart } from 'echarts/charts';
import { AriaComponent, GridComponent, LegendScrollComponent, TooltipComponent } from 'echarts/components';
import { init, use, type EChartsType } from 'echarts/core';
import { CanvasRenderer } from 'echarts/renderers';
import type { EChartsOption } from 'echarts';

use([LineChart, GridComponent, LegendScrollComponent, TooltipComponent, AriaComponent, CanvasRenderer]);

export interface ChartTheme {
  text: string;
  muted: string;
  surface: string;
  grid: string;
  pointer: string;
  font: string;
  reducedMotion: boolean;
  color: (value: string) => string;
}

// Canvas 不解析 CSS 变量，先通过浏览器解析主题色，再将颜色交给 ECharts。
function readChartTheme(element: HTMLElement): ChartTheme {
  const probe = document.createElement('span');
  probe.style.display = 'none';
  const canvas = document.createElement('canvas');
  canvas.width = canvas.height = 1;
  const context = canvas.getContext('2d', { willReadFrequently: true });
  const colors = new Map<string, string>();
  const color = (value: string) => {
    const cached = colors.get(value);
    if (cached) return cached;
    probe.style.color = value;
    element.appendChild(probe);
    let resolved = getComputedStyle(probe).color;
    probe.remove();
    // 主题包含 OKLCH；转成 RGBA，保证 ZRender 的渐变和强调色计算也能解析。
    if (context) {
      context.clearRect(0, 0, 1, 1);
      context.fillStyle = resolved;
      context.fillRect(0, 0, 1, 1);
      const [red, green, blue, alpha] = context.getImageData(0, 0, 1, 1).data;
      resolved = `rgba(${red}, ${green}, ${blue}, ${(alpha ?? 255) / 255})`;
    }
    colors.set(value, resolved);
    return resolved;
  };
  const style = getComputedStyle(element);
  return {
    text: color('var(--ag-text)'),
    muted: color('var(--ag-text-tertiary)'),
    surface: color('var(--ag-surface)'),
    grid: color('var(--ag-border-subtle)'),
    pointer: color('var(--ag-border)'),
    font: style.fontFamily,
    reducedMotion: window.matchMedia('(prefers-reduced-motion: reduce)').matches,
    color,
  };
}

export function BaseChart({ option, label, className = '' }: {
  option: (theme: ChartTheme) => EChartsOption;
  label: string;
  className?: string;
}) {
  const elementRef = useRef<HTMLDivElement>(null);
  const optionRef = useRef(option);
  const refreshRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    optionRef.current = option;
    refreshRef.current?.();
  }, [option]);

  useEffect(() => {
    const element = elementRef.current;
    if (!element) return;
    let chart: EChartsType | undefined;
    let frame = 0;
    let disposed = false;
    let selected: Record<string, boolean> = {};
    const render = () => {
      frame = 0;
      if (disposed || element.clientWidth === 0 || element.clientHeight === 0) return;
      if (!chart) {
        chart = init(element, undefined, { renderer: 'canvas' });
        chart.on('legendselectchanged', (event: unknown) => {
          if (event && typeof event === 'object' && 'selected' in event) {
            selected = { ...event.selected as Record<string, boolean> };
          }
        });
      }
      const next = optionRef.current(readChartTheme(element));
      const legend = next.legend;
      if (legend && !Array.isArray(legend)) {
        // 自动刷新保留图例开关；移除已消失序列，防止筛选切换后状态无限增长。
        const names = new Set((Array.isArray(next.series) ? next.series : []).map(series => String(series.name)));
        selected = Object.fromEntries(Object.entries(selected).filter(([name]) => names.has(name)));
        next.legend = { ...legend, selected };
      }
      chart.resize();
      chart.setOption(next, { notMerge: true, lazyUpdate: true });
    };
    const schedule = () => {
      if (!disposed && !frame) frame = requestAnimationFrame(render);
    };
    refreshRef.current = schedule;
    const resizeObserver = new ResizeObserver(schedule);
    resizeObserver.observe(element);
    const themeObserver = new MutationObserver(schedule);
    themeObserver.observe(document.documentElement, { attributes: true, attributeFilter: ['class', 'style', 'data-theme'] });
    const themeStyle = document.getElementById('ag-theme-vars');
    if (themeStyle) themeObserver.observe(themeStyle, { childList: true, characterData: true, subtree: true });
    const motion = window.matchMedia('(prefers-reduced-motion: reduce)');
    motion.addEventListener('change', schedule);
    document.fonts?.ready.then(schedule);
    schedule();
    return () => {
      disposed = true;
      refreshRef.current = null;
      cancelAnimationFrame(frame);
      resizeObserver.disconnect();
      themeObserver.disconnect();
      motion.removeEventListener('change', schedule);
      chart?.dispose();
    };
  }, []);

  return <div ref={elementRef} role="img" aria-label={label} className={`h-full min-h-0 w-full min-w-0 ${className}`} />;
}
