import { memo, useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { Button, Dropdown } from '@heroui/react';
import { Check, ChevronDown, RefreshCw } from 'lucide-react';
import { normalizeAutoRefresh, type AutoRefreshOptions } from '../hooks/usePersistentAutoRefresh';

interface AutoRefreshControlProps {
  value: number;
  options: AutoRefreshOptions;
  label: string;
  offLabel: string;
  fastLabel?: string;
  beforeRefresh?: ReactNode;
  afterRefresh?: ReactNode;
  triggerClassName?: string;
  ariaLabel: string;
  refreshAriaLabel: string;
  onChange: (value: number) => void;
  onAutoRefresh?: () => void | Promise<unknown>;
  onRefresh: () => void | Promise<unknown>;
  isRefreshing?: boolean;
  isAutoRefreshing?: boolean;
  isDisabled?: boolean;
}

function useAutoRefreshCountdown({
  active,
  isRefreshing,
  onRefresh,
  resetKey,
  seconds,
}: {
  active: boolean;
  isRefreshing: boolean;
  onRefresh: () => void | Promise<unknown>;
  resetKey: number;
  seconds: number;
}) {
  const [remainingSeconds, setRemainingSeconds] = useState(seconds);
  const onRefreshRef = useRef(onRefresh);
  const isRefreshingRef = useRef(isRefreshing);

  useEffect(() => {
    onRefreshRef.current = onRefresh;
  }, [onRefresh]);

  useEffect(() => {
    isRefreshingRef.current = isRefreshing;
  }, [isRefreshing]);

  useEffect(() => {
    if (!active || seconds <= 0 || typeof window === 'undefined') {
      setRemainingSeconds(seconds);
      return undefined;
    }

    const intervalMs = seconds * 1000;
    let disposed = false;
    let timeoutId: number | undefined;
    let nextRefreshAt = Date.now() + intervalMs;

    const clearTimer = () => {
      if (timeoutId !== undefined) {
        window.clearTimeout(timeoutId);
        timeoutId = undefined;
      }
    };

    const documentHidden = () => typeof document !== 'undefined' && document.visibilityState === 'hidden';
    const tickMs = seconds < 1 ? 100 : 1000;
    const updateRemaining = (msLeft: number) => {
      if (seconds < 1) {
        setRemainingSeconds(seconds);
      } else {
        setRemainingSeconds(Math.max(1, Math.ceil(msLeft / 1000)));
      }
    };

    const scheduleNextTick = () => {
      if (disposed) return;
      clearTimer();

      if (documentHidden()) {
        setRemainingSeconds(seconds);
        return;
      }

      const msLeft = Math.max(0, nextRefreshAt - Date.now());
      updateRemaining(msLeft);
      timeoutId = window.setTimeout(runTick, Math.min(tickMs, msLeft));
    };

    const runTick = () => {
      if (disposed) return;

      const now = Date.now();
      if (now >= nextRefreshAt) {
        if (isRefreshingRef.current) {
          nextRefreshAt = now + intervalMs;
        } else {
          void onRefreshRef.current();
          nextRefreshAt = Date.now() + intervalMs;
        }
      }

      scheduleNextTick();
    };

    const handleVisibilityChange = () => {
      if (documentHidden()) {
        clearTimer();
        setRemainingSeconds(seconds);
        return;
      }
      nextRefreshAt = Date.now() + intervalMs;
      scheduleNextTick();
    };

    setRemainingSeconds(seconds);
    scheduleNextTick();
    document.addEventListener('visibilitychange', handleVisibilityChange);

    return () => {
      disposed = true;
      clearTimer();
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, [active, resetKey, seconds]);

  return remainingSeconds;
}

function formatAutoRefreshSeconds(seconds: number) {
  if (Number.isInteger(seconds)) {
    return `${seconds}s`;
  }
  return `${seconds.toFixed(1).replace(/\.0$/, '')}s`;
}

function formatAutoRefreshOption(label: string, seconds: number, fastLabel?: string) {
  if (seconds > 0 && seconds < 1) {
    return `${label}${fastLabel ?? formatAutoRefreshSeconds(seconds)}`;
  }
  return `${label}${formatAutoRefreshSeconds(seconds)}`;
}

export const AutoRefreshControl = memo(function AutoRefreshControl({
  value,
  options,
  label,
  offLabel,
  fastLabel,
  beforeRefresh,
  afterRefresh,
  triggerClassName,
  ariaLabel,
  refreshAriaLabel,
  onChange,
  onAutoRefresh,
  onRefresh,
  isAutoRefreshing,
  isRefreshing = false,
  isDisabled = false,
}: AutoRefreshControlProps) {
  const enabled = value > 0;
  const [manualRefreshVersion, setManualRefreshVersion] = useState(0);
  const autoRefreshHandler = onAutoRefresh ?? onRefresh;
  const remainingSeconds = useAutoRefreshCountdown({
    active: enabled && !isDisabled,
    isRefreshing: isAutoRefreshing ?? isRefreshing,
    onRefresh: autoRefreshHandler,
    resetKey: manualRefreshVersion,
    seconds: value,
  });
  const selectedKeys = useMemo(() => new Set([`auto_${value}`]), [value]);
  const currentLabel = enabled
    ? formatAutoRefreshOption(label, value < 1 ? value : remainingSeconds, fastLabel)
    : offLabel;
  const optionLabel = (seconds: number) => (seconds === 0 ? offLabel : formatAutoRefreshOption(label, seconds, fastLabel));
  const handleRefresh = useCallback(() => {
    void onRefresh();
    if (enabled) {
      setManualRefreshVersion((version) => version + 1);
    }
  }, [enabled, onRefresh]);

  return (
    <>
      {beforeRefresh}
      <Button
        isIconOnly
        aria-label={refreshAriaLabel}
        isDisabled={isDisabled || isRefreshing}
        size="sm"
        variant="ghost"
        className="h-8 w-8 min-w-8"
        onPress={handleRefresh}
      >
        <RefreshCw className={`h-4 w-4 ${isRefreshing ? 'animate-spin' : ''}`} />
      </Button>
      {afterRefresh}
      <Dropdown>
        <Dropdown.Trigger
          className={[
            'ag-auto-refresh-trigger button button--sm h-8 min-w-[7.5rem] whitespace-nowrap px-3',
            enabled ? 'button--secondary' : 'button--ghost',
            triggerClassName,
          ].filter(Boolean).join(' ')}
        >
          <span>{currentLabel}</span>
          <ChevronDown className="h-3 w-3 shrink-0" />
        </Dropdown.Trigger>
        <Dropdown.Popover placement="bottom end">
          <Dropdown.Menu
            aria-label={ariaLabel}
            selectedKeys={selectedKeys}
            selectionMode="single"
            onAction={(key) => {
              onChange(normalizeAutoRefresh(String(key).replace('auto_', ''), options));
            }}
          >
            {options.map((seconds) => {
              const itemLabel = optionLabel(seconds);
              return (
                <Dropdown.Item key={`auto_${seconds}`} id={`auto_${seconds}`} textValue={itemLabel}>
                  <span className="flex items-center justify-between gap-6">
                    <span>{itemLabel}</span>
                    {value === seconds ? <Check className="h-3.5 w-3.5 text-primary" /> : null}
                  </span>
                </Dropdown.Item>
              );
            })}
          </Dropdown.Menu>
        </Dropdown.Popover>
      </Dropdown>
    </>
  );
});
