import { memo, useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { Button } from '@heroui/react';
import { RefreshCw } from 'lucide-react';
import { normalizeAutoRefresh, type AutoRefreshOptions } from '../hooks/usePersistentAutoRefresh';
import { ToolbarMenu, ToolbarMenuItem } from './ToolbarMenu';

interface AutoRefreshControlProps {
  value: number;
  options: AutoRefreshOptions;
  label: string;
  offLabel: string;
  fastLabel?: string;
  beforeRefresh?: ReactNode;
  afterRefresh?: ReactNode;
  refreshButtonClassName?: string;
  triggerClassName?: string;
  ariaLabel: string;
  refreshAriaLabel: string;
  onChange: (value: number) => void;
  onAutoRefresh?: () => void | Promise<unknown>;
  onMenuOpenChange?: (isOpen: boolean) => void;
  onRefresh: () => void | Promise<unknown>;
  isRefreshing?: boolean;
  isAutoRefreshing?: boolean;
  isAutoRefreshDisabled?: boolean;
  isDisabled?: boolean;
}

function useAutoRefreshTimer({
  active,
  isRefreshing,
  onDisplaySecondsChange,
  onRefresh,
  resetKey,
  seconds,
}: {
  active: boolean;
  isRefreshing: boolean;
  onDisplaySecondsChange?: (seconds: number) => void;
  onRefresh: () => void | Promise<unknown>;
  resetKey: number;
  seconds: number;
}) {
  const onDisplaySecondsChangeRef = useRef(onDisplaySecondsChange);
  const onRefreshRef = useRef(onRefresh);
  const isRefreshingRef = useRef(isRefreshing);

  useEffect(() => {
    onDisplaySecondsChangeRef.current = onDisplaySecondsChange;
  }, [onDisplaySecondsChange]);

  useEffect(() => {
    onRefreshRef.current = onRefresh;
  }, [onRefresh]);

  useEffect(() => {
    isRefreshingRef.current = isRefreshing;
  }, [isRefreshing]);

  useEffect(() => {
    if (!active || seconds <= 0 || typeof window === 'undefined') {
      return undefined;
    }

    const intervalMs = seconds * 1000;
    let disposed = false;
    let timeoutId: number | undefined;
    let nextRefreshAt = Date.now() + intervalMs;
    const tickMs = seconds < 1 ? 100 : 1000;

    const clearTimer = () => {
      if (timeoutId !== undefined) {
        window.clearTimeout(timeoutId);
        timeoutId = undefined;
      }
    };

    const documentHidden = () => typeof document !== 'undefined' && document.visibilityState === 'hidden';
    const updateDisplay = (msLeft: number) => {
      const handler = onDisplaySecondsChangeRef.current;
      if (!handler) return;
      handler(seconds < 1 ? seconds : Math.max(1, Math.ceil(msLeft / 1000)));
    };

    const scheduleNextRefresh = () => {
      if (disposed) return;
      clearTimer();

      if (documentHidden()) {
        updateDisplay(intervalMs);
        return;
      }

      const msLeft = Math.max(0, nextRefreshAt - Date.now());
      updateDisplay(msLeft);
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

      scheduleNextRefresh();
    };

    const handleVisibilityChange = () => {
      if (documentHidden()) {
        clearTimer();
        return;
      }
      nextRefreshAt = Date.now() + intervalMs;
      scheduleNextRefresh();
    };

    scheduleNextRefresh();
    document.addEventListener('visibilitychange', handleVisibilityChange);

    return () => {
      disposed = true;
      clearTimer();
      document.removeEventListener('visibilitychange', handleVisibilityChange);
    };
  }, [active, resetKey, seconds]);
}

function formatAutoRefreshSeconds(seconds: number) {
  if (Number.isInteger(seconds)) {
    return `${seconds}s`;
  }
  return `${seconds.toFixed(1).replace(/\.0$/, '')}s`;
}

function formatAutoRefreshTitle(label: string) {
  return label.trimEnd();
}

function formatAutoRefreshValue(seconds: number, fastLabel?: string) {
  if (seconds > 0 && seconds < 1) {
    return fastLabel ?? formatAutoRefreshSeconds(seconds);
  }
  return formatAutoRefreshSeconds(seconds);
}

function formatAutoRefreshOption(label: string, seconds: number, fastLabel?: string) {
  return `${formatAutoRefreshTitle(label)} ${formatAutoRefreshValue(seconds, fastLabel)}`;
}

export const AutoRefreshControl = memo(function AutoRefreshControl({
  value,
  options,
  label,
  offLabel,
  fastLabel,
  beforeRefresh,
  afterRefresh,
  refreshButtonClassName,
  triggerClassName,
  ariaLabel,
  refreshAriaLabel,
  onChange,
  onAutoRefresh,
  onMenuOpenChange,
  onRefresh,
  isAutoRefreshing,
  isAutoRefreshDisabled = false,
  isRefreshing = false,
  isDisabled = false,
}: AutoRefreshControlProps) {
  const enabled = value > 0;
  const autoRefreshEnabled = enabled && !isAutoRefreshDisabled;
  const [manualRefreshVersion, setManualRefreshVersion] = useState(0);
  const autoRefreshHandler = onAutoRefresh ?? onRefresh;
  const labelTitleRef = useRef<HTMLSpanElement | null>(null);
  const labelValueRef = useRef<HTMLSpanElement | null>(null);
  const currentLabelTitle = formatAutoRefreshTitle(label);
  const currentLabelValue = autoRefreshEnabled ? formatAutoRefreshValue(value, fastLabel) : offLabel;
  const updateDisplayLabel = useCallback((displaySeconds: number) => {
    const titleElement = labelTitleRef.current;
    const valueElement = labelValueRef.current;
    if (titleElement) {
      titleElement.textContent = formatAutoRefreshTitle(label);
    }
    if (valueElement) {
      valueElement.textContent = autoRefreshEnabled ? formatAutoRefreshValue(displaySeconds, fastLabel) : offLabel;
    }
  }, [autoRefreshEnabled, fastLabel, label, offLabel]);
  const setLabelTitleElement = useCallback((element: HTMLSpanElement | null) => {
    labelTitleRef.current = element;
    if (element) {
      element.textContent = currentLabelTitle;
    }
  }, [currentLabelTitle]);
  const setLabelValueElement = useCallback((element: HTMLSpanElement | null) => {
    labelValueRef.current = element;
    if (element) {
      element.textContent = currentLabelValue;
    }
  }, [currentLabelValue]);

  useEffect(() => {
    if (labelTitleRef.current) {
      labelTitleRef.current.textContent = currentLabelTitle;
    }
    if (labelValueRef.current) {
      labelValueRef.current.textContent = currentLabelValue;
    }
  }, [currentLabelTitle, currentLabelValue]);

  useAutoRefreshTimer({
    active: autoRefreshEnabled && !isDisabled,
    isRefreshing: isAutoRefreshing ?? isRefreshing,
    onDisplaySecondsChange: updateDisplayLabel,
    onRefresh: autoRefreshHandler,
    resetKey: manualRefreshVersion,
    seconds: value,
  });
  const optionLabel = (seconds: number) => (seconds === 0 ? offLabel : formatAutoRefreshOption(label, seconds, fastLabel));
  const handleRefresh = useCallback(() => {
    void onRefresh();
    if (autoRefreshEnabled) {
      setManualRefreshVersion((version) => version + 1);
    }
  }, [autoRefreshEnabled, onRefresh]);

  return (
    <>
      {beforeRefresh}
      <Button
        isIconOnly
        aria-label={refreshAriaLabel}
        isDisabled={isDisabled || isRefreshing}
        size="sm"
        variant="ghost"
        className={['h-8 w-8 min-w-8', refreshButtonClassName].filter(Boolean).join(' ')}
        onPress={handleRefresh}
      >
        <RefreshCw className={`h-4 w-4 ${isRefreshing ? 'animate-spin' : ''}`} />
      </Button>
      {afterRefresh}
      <ToolbarMenu
        ariaLabel={ariaLabel}
        rootClassName="ag-auto-refresh-menu"
        label={(
          <span className="ag-auto-refresh-label" data-enabled={currentLabelValue ? 'true' : 'false'}>
            <span ref={setLabelTitleElement} className="ag-auto-refresh-label-title" />
            <span ref={setLabelValueElement} className="ag-auto-refresh-label-value" />
          </span>
        )}
        className={[
          'ag-auto-refresh-trigger button button--sm h-8 min-w-[7.5rem] whitespace-nowrap px-3',
          autoRefreshEnabled ? 'button--secondary' : 'button--ghost',
          triggerClassName,
        ].filter(Boolean).join(' ')}
        disabled={isDisabled || isAutoRefreshDisabled}
        onOpenChange={onMenuOpenChange}
      >
        {(close) => (
          <>
            {options.map((seconds) => {
              const itemLabel = optionLabel(seconds);
              return (
                <ToolbarMenuItem
                  key={`auto_${seconds}`}
                  isSelected={value === seconds}
                  role="menuitemradio"
                  onSelect={() => {
                    onChange(normalizeAutoRefresh(seconds, options));
                    close();
                  }}
                >
                  {itemLabel}
                </ToolbarMenuItem>
              );
            })}
          </>
        )}
      </ToolbarMenu>
    </>
  );
});
