import type { UsageColumnConfig } from '../../../shared/columns/usageColumns';
import type { UsageLogResp } from '../../../shared/types';

type TimingRow = Pick<UsageLogResp, 'output_tokens' | 'duration_ms' | 'first_token_ms'>;

/** Output tokens already include reasoning tokens; never add them a second time. */
export function usageTokensPerSecond(row: TimingRow): number | null {
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
    return [{
      ...column,
      key: bottomKey,
      title,
      width: top && bottom ? '96px' : '78px',
      render: (row: UsageLogResp) => (
        <div className="flex flex-col items-center gap-1 text-center tabular-nums">
          {[top, bottom].filter((item): item is UsageColumnConfig<UsageLogResp> => Boolean(item)).map((item) => (
            <div key={item.key} title={typeof item.title === 'string' ? item.title : undefined}>
              {item.render(row)}
            </div>
          ))}
        </div>
      ),
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

