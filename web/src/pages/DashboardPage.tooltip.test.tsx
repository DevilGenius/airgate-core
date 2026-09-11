import { render, waitFor } from '@testing-library/react';
import { describe, expect, it } from 'vitest';
import { ChartTooltip, TokenTrendTooltip, apiKeyBilledCostDataKey } from './DashboardPage';

/** 构造 Key Top 12 提示框的一行数据（recharts 传入的 payload item 形状）。 */
function tooltipRow(index: number, tokens: number) {
  const dataKey = `api_key_${index + 1}`;
  const data: Record<string, unknown> = { time: '2026-08-16' };
  data[apiKeyBilledCostDataKey(dataKey)] = tokens / 1000;
  return { color: '#000000', dataKey, name: `key-${index + 1}`, payload: data, value: tokens };
}

function tooltipGrid(container: HTMLElement): HTMLElement {
  const grid = container.querySelector<HTMLElement>('div.grid');
  if (!grid) throw new Error('提示框未使用网格布局');
  return grid;
}

describe('dashboard key top 12 tooltip layout', () => {
  it('lays rows out as a four column grid so numbers line up vertically', () => {
    const payload = [10_350_000_000, 64_940_000, 28_490_000].map((tokens, index) => tooltipRow(index, tokens));
    const { container } = render(
      <ChartTooltip active label="2026-08-16" payload={payload} seriesOrder={payload.map((item) => item.dataKey)} />,
    );

    const grid = tooltipGrid(container);
    expect(grid.className).toContain('grid-cols-[0.5rem_minmax(0,1fr)_auto_auto]');
    // 每个 Key 占 4 个网格单元（色点 / 名称 / Token / 金额），列数一致才能竖向对齐。
    const cells = Array.from(grid.children);
    expect(cells).toHaveLength(12);

    const tokens = cells.filter((_, index) => index % 4 === 2);
    const costs = cells.filter((_, index) => index % 4 === 3);
    expect(tokens.map((cell) => cell.textContent)).toEqual(['10.35B', '64.94M', '28.49M']);
    for (const cell of [...tokens, ...costs]) {
      expect(cell.className).toContain('text-right');
      expect(cell.className).toContain('tabular-nums');
      // 数值列带固定最小宽度，切换时间桶（数值长短变化）时提示框尺寸保持稳定。
      expect(cell.className).toMatch(/min-w-\[/);
    }
    // 金额单元格用 CostValue 渲染，带 $ 前缀并与 token 同列对齐。
    for (const cell of costs) {
      expect(cell.textContent?.startsWith('$')).toBe(true);
    }
  });

  it('renders rows ordered by the hovered bucket token usage', () => {
    const payload = [4_140_000, 10_350_000_000, 0, 64_940_000].map((tokens, index) => tooltipRow(index, tokens));
    const { container } = render(
      <ChartTooltip active label="2026-08-16" payload={payload} seriesOrder={payload.map((item) => item.dataKey)} />,
    );

    const tokens = Array.from(tooltipGrid(container).children).filter((_, index) => index % 4 === 2);
    expect(tokens.map((cell) => cell.textContent)).toEqual(['10.35B', '64.94M', '4.14M', '0']);
  });

  it('renders no rows when the tooltip is inactive', () => {
    const payload = [10_350_000_000].map((tokens, index) => tooltipRow(index, tokens));
    const { container } = render(<ChartTooltip active={false} label="2026-08-16" payload={payload} />);

    expect(container).toBeEmptyDOMElement();
  });
});

describe('dashboard token trend tooltip layout', () => {
  const datum = { actualCost: 12.5, standardCost: 20, timeLabel: { primary: '08/16', tooltip: '08/16' } };
  // 数值刻意不是降序：图例顺序为 input → output → cacheRatio，用来验证行序不按数值重排。
  const trendPayload = [
    { color: '#111111', dataKey: 'input', payload: datum, value: 345_600 },
    { color: '#222222', dataKey: 'output', payload: datum, value: 1_200_000 },
    { color: '#333333', dataKey: 'cacheRatio', payload: datum, value: 45.32 },
  ];

  function metricCells(grid: HTMLElement, column: number) {
    return Array.from(grid.children).slice(0, 9).filter((_, index) => index % 3 === column);
  }

  it('lays metric rows out as a three column grid with right aligned numbers', () => {
    const { container } = render(<TokenTrendTooltip active label="2026-08-16" payload={trendPayload} />);

    const grid = tooltipGrid(container);
    expect(grid.className).toContain('grid-cols-[0.5rem_minmax(0,1fr)_auto]');
    // 指标行：每个指标占 3 个网格单元（色点 / 指标 / 数值）。
    expect(Array.from(grid.children).slice(0, 9)).toHaveLength(9);

    const values = metricCells(grid, 2);
    expect(values.map((cell) => cell.textContent)).toEqual(['345.60K', '1.20M', '45.3%']);
    for (const cell of values) {
      expect(cell.className).toContain('text-right');
      expect(cell.className).toContain('tabular-nums');
      // 数值列带固定最小宽度，切换时间桶（数值长短变化）时提示框尺寸保持稳定。
      expect(cell.className).toMatch(/min-w-\[/);
    }
  });

  it('keeps the legend metric order and aligns the cost footer inside the same grid', async () => {
    const { container } = render(<TokenTrendTooltip active label="2026-08-16" payload={[...trendPayload].reverse()} />);

    const grid = tooltipGrid(container);
    // 行序固定为图例顺序，不随数值大小变化。
    expect(metricCells(grid, 2).map((cell) => cell.textContent)).toEqual(['345.60K', '1.20M', '45.3%']);

    // 分隔线 + 实际/标准两行，费用数字与指标数值共用同一列，不再单独撑宽提示框。
    const footer = () => Array.from(grid.children).slice(9);
    expect(footer()).toHaveLength(5);
    for (const cell of [footer()[2], footer()[4]]) {
      expect(cell?.textContent?.startsWith('$')).toBe(true);
      expect(cell?.className).toContain('min-w-[4.25rem]');
      expect(cell?.className).toContain('text-right');
    }

    await waitFor(() => {
      expect(metricCells(grid, 1).map((cell) => cell.textContent)).toEqual(['Input', 'Output', '缓存比例']);
      expect(footer()[1]?.textContent).toBe('实际');
      expect(footer()[3]?.textContent).toBe('标准');
    });
  });

  it('renders nothing when the token trend tooltip is inactive', () => {
    const { container } = render(<TokenTrendTooltip active={false} label="2026-08-16" payload={trendPayload} />);

    expect(container).toBeEmptyDOMElement();
  });
});
