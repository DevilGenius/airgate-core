import { memo, useCallback, useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore, type ReactElement, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { Ban } from 'lucide-react';
import type { AccountResp, FamilyCooldownDTO } from '../../../shared/types';
import { AccountCapacityStore } from './accountRuntimeStores';
import { NativeSoftChip } from './accountNativeChip';

function StatusPill({
  icon,
  label,
  status,
  tooltip,
}: {
  icon?: ReactNode;
  label: string;
  status: 'active' | 'disabled';
  tooltip?: string;
}) {
  return (
    <NativeSoftChip
      className={icon ? 'ag-account-status-pill flex-row gap-0.5 whitespace-nowrap' : 'ag-account-status-pill'}
      title={tooltip}
      tone={status === 'active' ? 'success' : 'default'}
    >
      {icon}
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

const OAUTH_MODEL_UNSUPPORTED_REASON = 'not supported when using codex with a chatgpt account';

function isModelUnsupportedCooldown(cooldown: FamilyCooldownDTO): boolean {
  return cooldown.reason?.toLowerCase().includes(OAUTH_MODEL_UNSUPPORTED_REASON) ?? false;
}

const TRANSIENT_FAMILY_COOLDOWN_REASON_MARKERS = [
  'overload',
  '过载',
  '超载',
  'bad gateway',
  'gateway timeout',
  'service unavailable',
  'timeout',
  'timed out',
  'deadline exceeded',
];

function isTransientFamilyCooldown(cooldown: FamilyCooldownDTO): boolean {
  const reason = cooldown.reason?.trim().toLowerCase() ?? '';
  if (isModelUnsupportedCooldown(cooldown)) return true;
  if (/\b5\d{2}\b/.test(reason)) return true;
  return TRANSIENT_FAMILY_COOLDOWN_REASON_MARKERS.some((marker) => reason.includes(marker));
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
 * AccountStatusCell 将状态拆成最多三行：
 *   1. 主状态（活跃 / 限流 / 账号级退避 / 已禁用）
 *   2. 模型状态（家族限流 / 模型降级）
 *   3. 瞬时状态（家族退避等）
 * 到期的 rate_limited / degraded 视作 active（后端 lazy 回收，前端可先显示 active）。
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
  const transientFamilyCooldowns = liveFamilyCooldowns.filter(isTransientFamilyCooldown);
  const rateLimitedFamilyCooldowns = liveFamilyCooldowns.filter(
    (cooldown) => !isTransientFamilyCooldown(cooldown),
  );
  const cooldownTooltip = (cooldowns: FamilyCooldownDTO[]) => cooldowns
    .map((fc) => {
      const ms = Date.parse(fc.until) - now;
      const reason = fc.reason ? ` — ${fc.reason.slice(0, 80)}` : '';
      return `${fc.family} ${formatCountdown(ms)}${reason}`;
    })
    .join('\n');
  const familyTooltip = cooldownTooltip(rateLimitedFamilyCooldowns);
  const transientFamilyTooltip = cooldownTooltip(transientFamilyCooldowns);

  const pill = (label: string, bg: string, fg: string, tooltip?: string) => (
    <span
      className="inline-flex h-[0.875rem] items-center gap-1 px-1.5 rounded-full text-[10px] font-semibold border whitespace-nowrap"
      style={{ background: bg, color: fg, borderColor: `color-mix(in oklab, ${fg} 70%, transparent)` }}
      title={tooltip}
    >
      <span className="w-1 h-1 rounded-full" style={{ background: fg }} />
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

  // 主状态始终占据第一行；家族冷却与模型降级不能抢占主状态位置。
  let mainBadge: ReactElement;
  if (row.state === 'rate_limited' && hasCountdown) {
    mainBadge = pill(
      `${t('accounts.rate_limited_label', '限流中')} ${formatCountdown(remainingMs)}`,
      'var(--ag-danger-subtle)',
      'var(--ag-danger)',
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
    const trimmedError = row.error_msg?.trim();
    const isManualDisabled = trimmedError === '手动关闭' || trimmedError === '管理员手动关闭调度';
    const reason = trimmedError === '管理员手动关闭调度' ? '手动关闭' : trimmedError;
    mainBadge = (
      <div className="inline-flex min-w-0 max-w-full flex-col items-center gap-0.5">
        <StatusPill
          icon={isManualDisabled ? undefined : <Ban aria-hidden="true" className="relative -top-[0.5px] h-2.5 w-2.5 shrink-0 text-danger" />}
          label={t('status.disabled')}
          status="disabled"
          tooltip={reason || undefined}
        />
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

  // 模型状态独立占据第二行，家族限流与模型降级在同一行展示。
  const modelStatusBadges: Array<{ key: string; badge: ReactElement }> = [];
  if (rateLimitedFamilyCooldowns.length > 0) {
    modelStatusBadges.push({
      key: 'rate_limited',
      badge: pill(
        t('accounts.family_limited_status_label', '家族限流'),
        'var(--ag-warning-subtle)',
        'var(--ag-warning)',
        familyTooltip,
      ),
    });
  }

  // 模型降级（当前 30 分钟桶成功率低于阈值），紫色徽标，tooltip 逐模型列出成功率。
  const modelDemotions = row.model_demotions ?? [];
  if (modelDemotions.length > 0) {
    modelStatusBadges.push({
      key: 'model_demoted',
      badge: pill(
        t('accounts.model_demoted_status_label', '降级 ×{{count}}', { count: modelDemotions.length }),
        'color-mix(in srgb, #a855f7 14%, transparent)',
        '#a855f7',
        modelDemotions
          .map((item) => `${item.model} ${(item.success_rate * 100).toFixed(1)}% (${item.valid_requests})`)
          .join('\n'),
      ),
    });
  }

  // 瞬时状态独立占据第三行，避免与主状态或模型状态混排。
  const transientStatusBadges: Array<{ key: string; badge: ReactElement }> = [];
  if (transientFamilyCooldowns.length > 0) {
    transientStatusBadges.push({
      key: 'family_transient',
      badge: pill(
        t('accounts.family_transient_status_label', '家族退避中'),
        'color-mix(in srgb, #06b6d4 14%, transparent)',
        '#06b6d4',
        transientFamilyTooltip,
      ),
    });
  }

  const statusRows = [
    { key: 'main', badges: [{ key: 'main', badge: mainBadge }] },
    { key: 'model', badges: modelStatusBadges },
    { key: 'transient', badges: transientStatusBadges },
  ].filter((row) => row.badges.length > 0);

  if (statusRows.length === 1) {
    if (!freezeCooldownHoverProps) return mainBadge;
    return (
      <div className="flex w-full max-w-full flex-col items-center gap-0.5 text-center" {...freezeCooldownHoverProps}>
        <div className="flex max-w-full flex-nowrap items-center justify-center gap-0.5">
          {mainBadge}
        </div>
      </div>
    );
  }

  return (
    <div
      className="flex w-full max-w-full flex-col items-center gap-0.5 text-center"
      {...freezeCooldownHoverProps}
    >
      {statusRows.map((row) => (
        <div key={row.key} className="flex max-w-full flex-nowrap items-center justify-center gap-0.5">
          {row.badges.map((item) => (
            <span key={item.key} className="inline-flex">{item.badge}</span>
          ))}
        </div>
      ))}
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

  // 首次用实时值替换列表 fallback 时通过 remount 直接跳变，不播放滚动动画：
  // 页面切回时列表缓存值与首个容量快照之间几乎必然存在差值，
  // 没有这一步会把"最后一次容量变化"在每次切换页面时重复播放一遍。
  const isLive = store.has(rowId);
  return <AccountCapacityChip key={isLive ? 'live' : 'seed'} current={liveCurrent} max={max} />;
});
