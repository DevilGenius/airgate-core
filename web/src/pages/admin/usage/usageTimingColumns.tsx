import { usageLatencyTone, type UsageColumnConfig, type UsageLatencyTone } from '../../../shared/columns/usageColumns';
import type { UsageLogResp } from '../../../shared/types';

type TimingRow = Pick<UsageLogResp, 'output_tokens' | 'duration_ms' | 'first_token_ms' | 'stream' | 'account_type'>;

const LATENCY_BAR_COLORS: Record<UsageLatencyTone, string> = {
  normal: 'var(--ag-success)',
  slow: 'var(--ag-warning)',
  critical: 'var(--ag-danger)',
};

function latencyBarBackground(row: UsageLogResp): string {
  const firstTokenTone = usageLatencyTone(row.first_token_ms, 5_000, 15_000);
  const durationTone = usageLatencyTone(row.duration_ms, 30_000, 120_000);
  const top = firstTokenTone ? LATENCY_BAR_COLORS[firstTokenTone] : 'var(--ag-border)';
  const bottom = durationTone ? LATENCY_BAR_COLORS[durationTone] : 'var(--ag-border)';
  return `linear-gradient(to bottom, ${top} 0%, ${top} 42%, ${bottom} 58%, ${bottom} 100%)`;
}

/** Output tokens already include reasoning tokens; never add them a second time. */
export function usageTokensPerSecond(row: TimingRow): number | null {
  // A synchronous API Key response arrives as a complete body, so subtracting
  // its first-token timestamp leaves parsing time, not token generation time.
  const accountType = row.account_type?.trim().toLowerCase();
  if (row.stream === false && (accountType === 'apikey' || accountType === 'api_key')) return null;
  const { output_tokens: tokens, duration_ms: duration, first_token_ms: firstToken } = row;
  if (!Number.isFinite(tokens) || tokens <= 0 || !Number.isFinite(duration) || duration <= 0) return null;
  const elapsed = Number.isFinite(firstToken) && firstToken > 0 ? duration - firstToken : duration;
  if (elapsed <= 0) return null;
  const rate = tokens * 1000 / elapsed;
  return Number.isFinite(rate) ? rate : null;
}

/** Keep each metric's visibility preference while displaying two stacked columns. */
export function combineUsageTimingColumns(
  columns: UsageColumnConfig<UsageLogResp>[],
): UsageColumnConfig<UsageLogResp>[] {
  const pairs = [
    ['ws_dial_ms', 'first_event_ms'],
    ['first_token_ms', 'duration_ms'],
  ] as const;
  const byKey = new Map(columns.map((column) => [column.key, column]));
  const emitted = new Set<string>();
  return columns.flatMap((column) => {
    const pair = pairs.find(([top, bottom]) => column.key === top || column.key === bottom);
    if (!pair) return [column];
    const [topKey, bottomKey] = pair;
    if (emitted.has(bottomKey)) return [];
    emitted.add(bottomKey);
    const top = byKey.get(topKey);
    const bottom = byKey.get(bottomKey);
    const title = top && bottom
      ? typeof top.title === 'string' && typeof bottom.title === 'string'
        ? `${top.title}/${bottom.title}`
        : <>{top.title}/{bottom.title}</>
      : (top ?? bottom)!.title;
    const isLatencyPair = topKey === 'first_token_ms' && bottomKey === 'duration_ms';
    return [{
      ...column,
      key: bottomKey,
      title,
      width: top && bottom ? '96px' : '78px',
      render: (row: UsageLogResp) => {
        const values = (
          <div className="flex flex-col items-center gap-1 text-center tabular-nums">
            {[top, bottom].filter((item): item is UsageColumnConfig<UsageLogResp> => Boolean(item)).map((item) => (
              <div key={item.key} title={typeof item.title === 'string' ? item.title : undefined}>
                {item.render(row)}
              </div>
            ))}
          </div>
        );
        if (!isLatencyPair) return values;
        return (
          <div className="flex items-stretch justify-center gap-px">
            <span
              aria-hidden="true"
              className="my-0.5 w-[3px] shrink-0 rounded-full"
              style={{ background: latencyBarBackground(row) }}
            />
            {values}
          </div>
        );
      },
    }];
  });
}

/** Upgrade legacy preferences once; later hiding TPS must remain persistent. */
export function readUsageColumnSelection(value: unknown, defaults: readonly string[]): Set<string> {
  const legacy = Array.isArray(value);
  const source = legacy
    ? value
    : value && typeof value === 'object' && 'version' in value && value.version === 2 && 'keys' in value
      ? value.keys
      : null;
  if (!Array.isArray(source)) return new Set(defaults);
  const keys = source.filter((key): key is string => typeof key === 'string' && key.length > 0);
  if (keys.length === 0) return new Set(defaults);
  if (legacy && !keys.includes('tps')) keys.push('tps');
  return new Set(keys);
}

