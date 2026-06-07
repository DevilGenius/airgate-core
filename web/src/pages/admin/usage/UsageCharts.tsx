import { useMemo, useState, type PointerEvent } from 'react';
import { fmtNum } from '../../../shared/columns/usageColumns';
import { CostValue } from '../../../shared/components/CostValue';
import { PIE_CHART_COLORS, USAGE_TOKEN_COLORS } from '../../../shared/constants';
import type { UsageTrendBucket } from '../../../shared/types';

type TokenTrendLineKey = 'input' | 'output' | 'cacheCreation' | 'cacheRead' | 'cacheRatio' | 'cacheCumulativeRatio';
type TokenTrendPoint = Record<TokenTrendLineKey, number> & {
  actualCost: number;
  rawTime: string;
  standardCost: number;
  time: string;
};

const TOKEN_TREND_LINE_ORDER: TokenTrendLineKey[] = ['input', 'output', 'cacheCreation', 'cacheRead', 'cacheRatio', 'cacheCumulativeRatio'];
const TOKEN_TREND_RATIO_KEYS = new Set<TokenTrendLineKey>(['cacheRatio', 'cacheCumulativeRatio']);
const TOKEN_TREND_WIDTH = 800;
const TOKEN_TREND_HEIGHT = 220;
const TOKEN_TREND_MARGIN = {
  bottom: 28,
  left: 48,
  right: 42,
  top: 12,
};
const TOKEN_TREND_PLOT_WIDTH = TOKEN_TREND_WIDTH - TOKEN_TREND_MARGIN.left - TOKEN_TREND_MARGIN.right;
const TOKEN_TREND_PLOT_HEIGHT = TOKEN_TREND_HEIGHT - TOKEN_TREND_MARGIN.top - TOKEN_TREND_MARGIN.bottom;
const PIE_CHART_SIZE = 176;
const PIE_CHART_CENTER = PIE_CHART_SIZE / 2;
const PIE_CHART_INNER_RADIUS = 42;
const PIE_CHART_OUTER_RADIUS = 68;

export interface UsagePieChartItem {
  name: string;
  value: number;
}

function polarToCartesian(cx: number, cy: number, radius: number, angle: number) {
  return {
    x: cx + radius * Math.cos(angle),
    y: cy + radius * Math.sin(angle),
  };
}

function buildDonutSlicePath(startAngle: number, endAngle: number) {
  const outerStart = polarToCartesian(PIE_CHART_CENTER, PIE_CHART_CENTER, PIE_CHART_OUTER_RADIUS, startAngle);
  const outerEnd = polarToCartesian(PIE_CHART_CENTER, PIE_CHART_CENTER, PIE_CHART_OUTER_RADIUS, endAngle);
  const innerEnd = polarToCartesian(PIE_CHART_CENTER, PIE_CHART_CENTER, PIE_CHART_INNER_RADIUS, endAngle);
  const innerStart = polarToCartesian(PIE_CHART_CENTER, PIE_CHART_CENTER, PIE_CHART_INNER_RADIUS, startAngle);
  const largeArc = endAngle - startAngle > Math.PI ? 1 : 0;

  return [
    `M ${outerStart.x.toFixed(3)} ${outerStart.y.toFixed(3)}`,
    `A ${PIE_CHART_OUTER_RADIUS} ${PIE_CHART_OUTER_RADIUS} 0 ${largeArc} 1 ${outerEnd.x.toFixed(3)} ${outerEnd.y.toFixed(3)}`,
    `L ${innerEnd.x.toFixed(3)} ${innerEnd.y.toFixed(3)}`,
    `A ${PIE_CHART_INNER_RADIUS} ${PIE_CHART_INNER_RADIUS} 0 ${largeArc} 0 ${innerStart.x.toFixed(3)} ${innerStart.y.toFixed(3)}`,
    'Z',
  ].join(' ');
}

export function UsagePieChart({ data }: { data: UsagePieChartItem[] }) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);
  const slices = useMemo(() => {
    const positiveItems = data.filter((item) => item.value > 0);
    const total = positiveItems.reduce((sum, item) => sum + item.value, 0);
    let cursor = -Math.PI / 2;

    return positiveItems.map((item) => {
      const angle = total > 0 ? (item.value / total) * Math.PI * 2 : 0;
      const startAngle = cursor;
      const endAngle = Math.min(cursor + angle, Math.PI * 1.5 - 0.0001);
      cursor += angle;
      return {
        ...item,
        d: buildDonutSlicePath(startAngle, endAngle),
        originalIndex: data.indexOf(item),
      };
    });
  }, [data]);
  const hoveredSlice = hoveredIndex == null ? null : slices[hoveredIndex] ?? null;

  return (
    <div className="relative h-[176px] w-[176px]">
      <svg
        aria-label="Usage distribution"
        className="block h-full w-full"
        role="img"
        viewBox={`0 0 ${PIE_CHART_SIZE} ${PIE_CHART_SIZE}`}
        onPointerLeave={() => setHoveredIndex(null)}
      >
        <circle
          cx={PIE_CHART_CENTER}
          cy={PIE_CHART_CENTER}
          fill="none"
          r={(PIE_CHART_INNER_RADIUS + PIE_CHART_OUTER_RADIUS) / 2}
          stroke="var(--ag-border-subtle)"
          strokeWidth={PIE_CHART_OUTER_RADIUS - PIE_CHART_INNER_RADIUS}
        />
        {slices.map((slice, index) => (
          <path
            key={`${slice.name}:${index}`}
            d={slice.d}
            fill={PIE_CHART_COLORS[slice.originalIndex % PIE_CHART_COLORS.length]}
            stroke="var(--ag-surface)"
            strokeWidth={2}
            onPointerEnter={() => setHoveredIndex(index)}
          />
        ))}
      </svg>
      {hoveredSlice && (
        <div className="pointer-events-none absolute left-1/2 top-1/2 max-w-56 -translate-x-1/2 -translate-y-1/2 truncate rounded-[var(--radius)] border border-border bg-surface px-2.5 py-1.5 text-xs font-medium text-text shadow-lg">
          {hoveredSlice.name}
        </div>
      )}
    </div>
  );
}

function fmtTime(timeStr: string): string {
  if (timeStr.includes(' ')) {
    return timeStr.split(' ')[1] ?? timeStr;
  }
  const parts = timeStr.split('-');
  return `${parts[1] ?? ''}/${parts[2] ?? ''}`;
}

function formatTrendValue(key: TokenTrendLineKey, value: number) {
  return TOKEN_TREND_RATIO_KEYS.has(key) ? `${value.toFixed(1)}%` : fmtNum(value);
}

function getTokenTrendX(index: number, length: number) {
  if (length <= 1) return TOKEN_TREND_MARGIN.left + TOKEN_TREND_PLOT_WIDTH / 2;
  return TOKEN_TREND_MARGIN.left + (index / (length - 1)) * TOKEN_TREND_PLOT_WIDTH;
}

function getTokenTrendY(value: number, max: number) {
  return TOKEN_TREND_MARGIN.top + TOKEN_TREND_PLOT_HEIGHT - (value / max) * TOKEN_TREND_PLOT_HEIGHT;
}

function buildTokenTrendPath(points: TokenTrendPoint[], key: TokenTrendLineKey, tokenMax: number) {
  return points
    .map((point, index) => {
      const x = getTokenTrendX(index, points.length);
      const y = getTokenTrendY(point[key], TOKEN_TREND_RATIO_KEYS.has(key) ? 100 : tokenMax);
      return `${index === 0 ? 'M' : 'L'}${x.toFixed(1)},${y.toFixed(1)}`;
    })
    .join(' ');
}

function getTokenTrendXTicks(points: TokenTrendPoint[]) {
  if (points.length <= 6) return points.map((point, index) => ({ index, label: point.time }));
  const step = Math.ceil((points.length - 1) / 5);
  const indexes = new Set<number>();
  for (let index = 0; index < points.length; index += step) {
    indexes.add(index);
  }
  indexes.add(points.length - 1);
  return Array.from(indexes)
    .sort((a, b) => a - b)
    .map((index) => ({ index, label: points[index]?.time ?? '' }));
}

export function UsageTokenTrendChart({
  data,
  lineLabels,
}: {
  data: UsageTrendBucket[];
  lineLabels: Record<string, string>;
}) {
  const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);
  const chartData = useMemo(() => {
    let cumulativeCache = 0;
    let cumulativeTotal = 0;

    return data.map((d) => {
      const cacheTokens = d.cache_creation + d.cache_read;
      const totalTokens = d.input_tokens + d.output_tokens + cacheTokens;
      cumulativeCache += cacheTokens;
      cumulativeTotal += totalTokens;
      const cacheRatio = totalTokens > 0
        ? Math.min(100, Math.max(0, (cacheTokens / totalTokens) * 100))
        : 0;
      const cacheCumulativeRatio = cumulativeTotal > 0
        ? Math.min(100, Math.max(0, (cumulativeCache / cumulativeTotal) * 100))
        : 0;

      return {
        time: fmtTime(d.time),
        rawTime: d.time,
        input: d.input_tokens,
        output: d.output_tokens,
        cacheCreation: d.cache_creation,
        cacheRead: d.cache_read,
        cacheRatio,
        cacheCumulativeRatio,
        actualCost: d.actual_cost,
        standardCost: d.standard_cost,
      };
    });
  }, [data]);
  const chartModel = useMemo(() => {
    const tokenMax = Math.max(
      1,
      ...chartData.flatMap((point) => [point.input, point.output, point.cacheCreation, point.cacheRead]),
    );
    const niceTokenMax = tokenMax <= 10 ? 10 : Math.ceil(tokenMax / 4) * 4;
    const tokenTicks = [0, niceTokenMax / 2, niceTokenMax];
    const ratioTicks = [0, 50, 100];
    const xTicks = getTokenTrendXTicks(chartData);
    const paths = TOKEN_TREND_LINE_ORDER.map((key) => ({
      color: USAGE_TOKEN_COLORS[key],
      dash: TOKEN_TREND_RATIO_KEYS.has(key) ? '5 5' : undefined,
      key,
      path: buildTokenTrendPath(chartData, key, TOKEN_TREND_RATIO_KEYS.has(key) ? 100 : niceTokenMax),
    }));

    return {
      paths,
      ratioTicks,
      tokenTicks,
      xTicks,
    };
  }, [chartData]);
  const hoveredPoint = hoveredIndex == null ? null : chartData[hoveredIndex] ?? null;
  const hoveredX = hoveredIndex == null ? null : getTokenTrendX(hoveredIndex, chartData.length);
  const hoveredLeft = chartData.length > 1 && hoveredIndex != null
    ? `${Math.max(0, Math.min(100, (hoveredIndex / (chartData.length - 1)) * 100))}%`
    : '50%';
  const handlePointerMove = (event: PointerEvent<SVGSVGElement>) => {
    if (chartData.length === 0) return;
    const bounds = event.currentTarget.getBoundingClientRect();
    const ratio = Math.max(0, Math.min(1, (event.clientX - bounds.left) / Math.max(1, bounds.width)));
    setHoveredIndex(Math.round(ratio * (chartData.length - 1)));
  };

  return (
    <div className="relative flex h-full min-h-0 w-full flex-col">
      <svg
        className="min-h-0 flex-1 overflow-visible"
        role="img"
        viewBox={`0 0 ${TOKEN_TREND_WIDTH} ${TOKEN_TREND_HEIGHT}`}
        preserveAspectRatio="none"
        onPointerLeave={() => setHoveredIndex(null)}
        onPointerMove={handlePointerMove}
      >
        {chartModel.tokenTicks.map((tick) => {
          const y = getTokenTrendY(tick, chartModel.tokenTicks[2] || 1);
          return (
            <g key={`token-${tick}`}>
              <line x1={TOKEN_TREND_MARGIN.left} x2={TOKEN_TREND_WIDTH - TOKEN_TREND_MARGIN.right} y1={y} y2={y} stroke="var(--ag-border-subtle)" strokeWidth={1} vectorEffect="non-scaling-stroke" />
              <text x={TOKEN_TREND_MARGIN.left - 8} y={y + 4} fill="var(--ag-text-tertiary)" fontSize={11} textAnchor="end">{fmtNum(tick)}</text>
            </g>
          );
        })}
        {chartModel.ratioTicks.map((tick) => {
          const y = getTokenTrendY(tick, 100);
          return (
            <text key={`ratio-${tick}`} x={TOKEN_TREND_WIDTH - TOKEN_TREND_MARGIN.right + 8} y={y + 4} fill="var(--ag-text-tertiary)" fontSize={11}>{Math.round(tick)}%</text>
          );
        })}
        {chartModel.xTicks.map((tick) => (
          <text
            key={`x-${tick.index}`}
            x={getTokenTrendX(tick.index, chartData.length)}
            y={TOKEN_TREND_HEIGHT - 8}
            fill="var(--ag-text-tertiary)"
            fontSize={11}
            textAnchor="middle"
          >
            {tick.label}
          </text>
        ))}
        {chartModel.paths.map((line) => (
          <path
            key={line.key}
            d={line.path}
            fill="none"
            stroke={line.color}
            strokeDasharray={line.dash}
            strokeLinecap="round"
            strokeLinejoin="round"
            strokeWidth={2}
            vectorEffect="non-scaling-stroke"
          />
        ))}
        {hoveredX != null && (
          <line
            x1={hoveredX}
            x2={hoveredX}
            y1={TOKEN_TREND_MARGIN.top}
            y2={TOKEN_TREND_HEIGHT - TOKEN_TREND_MARGIN.bottom}
            stroke="var(--ag-text-tertiary)"
            strokeDasharray="3 3"
            strokeWidth={1}
            vectorEffect="non-scaling-stroke"
          />
        )}
      </svg>
      <div className="flex flex-wrap items-center justify-center gap-x-4 gap-y-1 pt-1 text-[11px] text-text-tertiary">
        {TOKEN_TREND_LINE_ORDER.map((key) => (
          <span key={key} className="inline-flex items-center gap-1.5">
            {TOKEN_TREND_RATIO_KEYS.has(key) ? (
              <span className="h-0 w-4 border-t-2 border-dashed" style={{ borderColor: USAGE_TOKEN_COLORS[key] }} />
            ) : (
              <span className="h-2 w-2 rounded-full" style={{ background: USAGE_TOKEN_COLORS[key] }} />
            )}
            <span>{lineLabels[key]}</span>
          </span>
        ))}
      </div>
      {hoveredPoint && (
        <div
          className="pointer-events-none absolute top-2 min-w-48 rounded-lg border border-border bg-surface p-3 text-xs text-text shadow-lg"
          style={{
            left: hoveredLeft,
            transform: hoveredIndex != null && hoveredIndex > chartData.length / 2 ? 'translateX(-100%)' : 'translateX(0)',
          }}
        >
          <div className="mb-2 font-semibold text-text">{hoveredPoint.rawTime}</div>
          {TOKEN_TREND_LINE_ORDER.map((key) => (
            <div key={key} className="flex items-center gap-2 py-0.5">
              <span className="h-2.5 w-2.5 rounded-sm" style={{ background: USAGE_TOKEN_COLORS[key] }} />
              <span className="text-text-secondary">{lineLabels[key]}:</span>
              <span className="ml-auto font-mono text-text">{formatTrendValue(key, hoveredPoint[key])}</span>
            </div>
          ))}
          <div className="mt-2 border-t border-border-subtle pt-2 text-text-secondary">
            Actual: <CostValue className="font-mono" value={hoveredPoint.actualCost} tone="actual" />
            {' | '}
            Standard: <CostValue className="font-mono" value={hoveredPoint.standardCost} tone="standard" />
          </div>
        </div>
      )}
    </div>
  );
}
