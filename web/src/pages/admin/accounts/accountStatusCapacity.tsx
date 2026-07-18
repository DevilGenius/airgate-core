import { memo, useCallback, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore, type ReactElement } from 'react';
import { useTranslation } from 'react-i18next';
import type { AccountResp } from '../../../shared/types';
import { AccountCapacityStore } from './accountRuntimeStores';
import { NativeSoftChip } from './accountNativeChip';

function StatusPill({
  label,
  status,
  tooltip,
}: {
  label: string;
  status: 'active' | 'disabled';
  tooltip?: string;
}) {
  return (
    <NativeSoftChip
      className="ag-account-status-pill"
      title={tooltip}
      tone={status === 'active' ? 'success' : 'default'}
    >
      {label}
    </NativeSoftChip>
  );
}

// formatCountdown 把剩余毫秒格式化成 "Xd Yh"/"Xh Ym"/"Ym" 样式，
// 与 sub2api 的"限流中 10h 16m 自动恢复"徽标一致。
function formatCountdown(ms: number): string {
  if (ms <= 0) return '';
  const s = Math.floor(ms / 1000);
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m`;
  return `${sec}s`;
}

function accountHasLiveCooldown(row: AccountResp, now: number): boolean {
  const stateUntil = row.state_until ? Date.parse(row.state_until) : 0;
  if (stateUntil > now) return true;
  return (row.family_cooldowns || []).some((fc) => Date.parse(fc.until) > now);
}

let cooldownClockNow = Date.now();
let cooldownClockTimer: number | null = null;
const cooldownClockListeners = new Set<() => void>();

function subscribeCooldownClock(listener: () => void) {
  cooldownClockNow = Date.now();
  cooldownClockListeners.add(listener);
  if (cooldownClockTimer == null) {
    cooldownClockTimer = window.setInterval(() => {
      cooldownClockNow = Date.now();
      cooldownClockListeners.forEach((notify) => notify());
    }, 1000);
  }

  return () => {
    cooldownClockListeners.delete(listener);
    if (cooldownClockListeners.size === 0 && cooldownClockTimer != null) {
      window.clearInterval(cooldownClockTimer);
      cooldownClockTimer = null;
    }
  };
}

function subscribeIdleClock() {
  return () => {};
}

function getCooldownClockSnapshot() {
  return cooldownClockNow;
}

function useCooldownClock(enabled: boolean): number {
  return useSyncExternalStore(
    enabled ? subscribeCooldownClock : subscribeIdleClock,
    getCooldownClockSnapshot,
    getCooldownClockSnapshot,
  );
}
/**
 * AccountStatusCell 渲染账号状态徽标，按 state + state_until 动态展示：
 *   active       → 绿色 "活跃"
 *   rate_limited → 橙色 "限流中 Xh Ym"（state_until 倒计时）
 *   degraded     → 黄色 "降级 Xm"（上游退避，倒计时）
 *   disabled     → 红色 "已禁用"（tooltip 显示 error_msg）
 * 到期的 rate_limited / degraded 视作 active（后端 lazy 回收，前端可先显示 active）。
 *
 * 同一行还会叠加家族级冷却（family_cooldowns）：账号 state 可能仍是 active，
 * 但某个 family（如 gpt-image）在 Redis 上仍处冷却中。用一个橙色小 pill
 * 标出"限流家族数"，hover tooltip 列出每个家族剩余时间。
 */
export function AccountStatusCell({ row }: { row: AccountResp }) {
  const { t } = useTranslation();
  const hasLiveCooldown = accountHasLiveCooldown(row, Date.now());
  const [isCooldownHovered, setIsCooldownHovered] = useState(false);
  const hoverNowRef = useRef<number | null>(null);
  const tickingNow = useCooldownClock(hasLiveCooldown && !isCooldownHovered);
  const liveNow = hasLiveCooldown ? tickingNow : Date.now();
  const now = isCooldownHovered && hoverNowRef.current != null ? hoverNowRef.current : liveNow;
  const untilMs = row.state_until ? Date.parse(row.state_until) : 0;
  const remainingMs = untilMs - now;
  const hasCountdown = untilMs > 0 && remainingMs > 0;

  // 过滤出仍生效的家族冷却（后端可能返回刚到期的）。
  const liveFamilyCooldowns = (row.family_cooldowns || []).filter(
    (fc) => Date.parse(fc.until) > now,
  );

  const pill = (label: string, bg: string, fg: string, tooltip?: string) => (
    <span
      className="inline-flex items-center gap-1 px-2.5 py-1 rounded-full text-[11px] font-semibold border whitespace-nowrap"
      style={{ background: bg, color: fg, borderColor: bg }}
      title={tooltip}
    >
      <span className="w-1.5 h-1.5 rounded-full" style={{ background: fg }} />
      {label}
    </span>
  );

  const freezeCooldownHoverProps = hasLiveCooldown
    ? {
      onMouseEnter: () => {
        hoverNowRef.current = liveNow;
        setIsCooldownHovered(true);
      },
      onMouseLeave: () => {
        hoverNowRef.current = null;
        setIsCooldownHovered(false);
      },
    }
    : undefined;

  // 主 state 徽标
  let mainBadge: ReactElement;
  if (row.state === 'rate_limited' && hasCountdown) {
    mainBadge = pill(
      `${t('accounts.rate_limited_label', '限流中')} ${formatCountdown(remainingMs)}`,
      'var(--ag-warning-subtle)',
      'var(--ag-warning)',
      t('accounts.rate_limited_tooltip', '上游限流，到期自动恢复，不影响调度开关'),
    );
  } else if (row.state === 'degraded' && hasCountdown) {
    mainBadge = pill(
      `${t('accounts.degraded_label', '降级')} ${formatCountdown(remainingMs)}`,
      'var(--ag-warning-subtle)',
      'var(--ag-warning)',
      t('accounts.degraded_tooltip', '退避中，暂停调度，到期自动恢复'),
    );
  } else if (row.state === 'disabled') {
    const reason = row.error_msg?.trim() === '管理员手动关闭调度' ? '手动关闭' : row.error_msg?.trim();
    mainBadge = (
      <div className="inline-flex min-w-0 max-w-full flex-col items-center gap-0.5">
        <StatusPill label={t('status.disabled')} status="disabled" tooltip={reason || undefined} />
        {reason && (
          <span className="block max-w-[5.75rem] truncate text-center text-[10px] leading-none text-[var(--ag-muted)]" title={reason}>
            {reason}
          </span>
        )}
      </div>
    );
  } else {
    // active，或 rate_limited/degraded 已到期（lazy 恢复）
    mainBadge = <StatusPill label={t('status.active')} status="active" />;
  }

  if (liveFamilyCooldowns.length === 0) {
    if (!freezeCooldownHoverProps) return mainBadge;
    return (
      <span className="inline-flex max-w-full" {...freezeCooldownHoverProps}>
        {mainBadge}
      </span>
    );
  }

  // tooltip 多行：每个家族 + 剩余时间，rate-limit 原因截断到 80 字符避免过宽
  const familyTooltip = liveFamilyCooldowns
    .map((fc) => {
      const ms = Date.parse(fc.until) - now;
      const reason = fc.reason ? ` — ${fc.reason.slice(0, 80)}` : '';
      return `${fc.family} ${formatCountdown(ms)}${reason}`;
    })
    .join('\n');

  const familyLabel = t(
    'accounts.family_cooldown_label',
    '{{count}} 家族限流',
    { count: liveFamilyCooldowns.length },
  );

  return (
    <div
      className="flex w-full max-w-full flex-wrap items-center justify-center gap-1 text-center"
      {...freezeCooldownHoverProps}
    >
      {mainBadge}
      {pill(
        familyLabel,
        'var(--ag-warning-subtle)',
        'var(--ag-warning)',
        familyTooltip,
      )}
    </div>
  );
}

const CAPACITY_ROLL_DURATION = 200;
const CAPACITY_ROLL_EASING = 'cubic-bezier(0.22, 1, 0.36, 1)';

type AccountCapacityDisplay = {
  text: string;
  fit: 'default' | 'compact' | 'compressed';
};

function formatAccountCapacityDisplay(value: number): AccountCapacityDisplay {
  const normalized = Number.isFinite(value) ? Math.trunc(value) : 0;
  const sign = normalized < 0 ? '-' : '';
  const abs = Math.abs(normalized);
  if (abs < 1000) {
    return { text: String(normalized), fit: 'default' };
  }

  const compactUnit = abs >= 1000000000
    ? { value: 1000000000, suffix: 'b' }
    : abs >= 1000000
      ? { value: 1000000, suffix: 'm' }
      : { value: 1000, suffix: 'k' };
  const compactValue = `${sign}${Math.floor(abs / compactUnit.value)}${compactUnit.suffix}`;
  return {
    text: compactValue,
    fit: compactValue.length > 3 ? 'compressed' : 'compact',
  };
}

// 滚动数字动画每次触发都会查询一次；缓存 MQL 避免重复创建 matchMedia 对象。
let reducedMotionMediaQueryList: MediaQueryList | null = null;
function prefersReducedMotion() {
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return false;
  reducedMotionMediaQueryList ??= window.matchMedia('(prefers-reduced-motion: reduce)');
  return reducedMotionMediaQueryList.matches;
}

/**
 * AccountCapacityNumber 渲染容量当前值，并在数值变化时做"滚动数字"动画：
 *   - 增加：新值自上而下滚入，旧值向下滑出底部（被裁剪）。
 *   - 减少：旧值向上滑出顶部，新值自下而上滚入。
 *
 * DOM 结构稳定、永不重挂载：incoming 层始终由 React 渲染当前值（文字稳定、不闪错值）；
 * outgoing 层在动画期命令式写入旧值并经 WAAPI 滑出，fill:'none' 结束后回到 CSS 隐藏态（不滞留）。
 * 动画走 WAAPI（仅 transform/opacity，GPU 合成层），可被 cancel() 干净中断，无每行定时器/状态，
 * 100 行高频更新下保持高性能。首次挂载、document.hidden、prefers-reduced-motion 时不触发动画。
 */
function AccountCapacityNumber({ value }: { value: number }) {
  const incomingRef = useRef<HTMLSpanElement | null>(null);
  const outgoingRef = useRef<HTMLSpanElement | null>(null);
  const previousRef = useRef(value);
  const animationsRef = useRef<Animation[]>([]);
  const display = formatAccountCapacityDisplay(value);

  useLayoutEffect(() => {
    const previous = previousRef.current;
    if (previous === value) return;
    previousRef.current = value;

    const incoming = incomingRef.current;
    const outgoing = outgoingRef.current;
    if (!incoming || !outgoing || typeof incoming.animate !== 'function') return;
    if (prefersReducedMotion()) return;
    if (typeof document !== 'undefined' && document.hidden) return;

    // 中断上一轮滚动：cancel() 后两层瞬回 CSS 静止态（incoming 显示新值、outgoing 隐藏），
    // 因 incoming 文本始终是当前值，绝不会出现内容硬切/闪错值。
    for (const animation of animationsRef.current) animation.cancel();
    const previousDisplay = formatAccountCapacityDisplay(previous);
    outgoing.textContent = previousDisplay.text;
    outgoing.dataset.fit = previousDisplay.fit;

    const increasing = value > previous;
    const incomingFrom = increasing ? '-100%' : '100%';
    const outgoingTo = increasing ? '100%' : '-100%';
    const options: KeyframeAnimationOptions = {
      duration: CAPACITY_ROLL_DURATION,
      easing: CAPACITY_ROLL_EASING,
      fill: 'none',
    };

    animationsRef.current = [
      incoming.animate(
        [
          { transform: `translate3d(0, ${incomingFrom}, 0)`, opacity: 0.4 },
          { transform: 'translate3d(0, 0, 0)', opacity: 1 },
        ],
        options,
      ),
      outgoing.animate(
        [
          { transform: 'translate3d(0, 0, 0)', opacity: 1 },
          { transform: `translate3d(0, ${outgoingTo}, 0)`, opacity: 0.4 },
        ],
        options,
      ),
    ];
  }, [value]);

  useEffect(() => () => {
    for (const animation of animationsRef.current) animation.cancel();
    animationsRef.current = [];
  }, []);

  return (
    <>
      <span ref={outgoingRef} aria-hidden="true" className="ag-account-capacity-number ag-account-capacity-number--out" />
      <span
        ref={incomingRef}
        className="ag-account-capacity-number ag-account-capacity-number--in"
        data-fit={display.fit}
      >
        {display.text}
      </span>
    </>
  );
}

export const AccountCapacityChip = memo(function AccountCapacityChip({ current, max }: { current: number; max: number }) {
  const state = current <= 0 ? 'idle' : current >= max ? 'full' : 'active';
  const maxDisplay = formatAccountCapacityDisplay(max);

  return (
    <span
      className="ag-account-capacity"
      data-state={state}
      title={`${current} / ${max}`}
      aria-label={`${current} / ${max}`}
    >
      <span className="ag-account-capacity-current">
        <AccountCapacityNumber value={current} />
      </span>
      <span className="ag-account-capacity-divider">/</span>
      <span className="ag-account-capacity-max" data-fit={maxDisplay.fit}>{maxDisplay.text}</span>
    </span>
  );
});

export const AccountCapacityLiveChip = memo(function AccountCapacityLiveChip({
  current,
  max,
  rowId,
  store,
}: {
  current: number;
  max: number;
  rowId: number;
  store: AccountCapacityStore;
}) {
  const liveCurrent = useSyncExternalStore(
    useCallback((listener) => store.subscribe(rowId, listener), [rowId, store]),
    useCallback(() => store.getCurrent(rowId, current), [current, rowId, store]),
    () => current,
  );

  return <AccountCapacityChip current={liveCurrent} max={max} />;
});
