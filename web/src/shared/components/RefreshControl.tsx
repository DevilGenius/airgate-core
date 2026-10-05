import { memo, useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import styles from './RefreshControl.module.css';
import { RefreshCw } from 'lucide-react';
import { normalizeAutoRefresh, type AutoRefreshOptions } from '../hooks/usePersistentAutoRefresh';
import { ToolbarMenu, ToolbarMenuItem } from './ToolbarMenu';

interface RefreshControlProps {
  value: number;
  options: AutoRefreshOptions;
  fastLabel?: string;
  beforeRefresh?: ReactNode;
  afterRefresh?: ReactNode;
  afterAutoRefresh?: ReactNode;
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

export const RefreshControl = memo(function RefreshControl({
  value,
  options,
  fastLabel,
  beforeRefresh,
  afterRefresh,
  afterAutoRefresh,
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
}: RefreshControlProps) {
  const { t } = useTranslation();
  const label = t('usage.auto_update');
  const offLabel = t('common.manual_update');
  const enabled = value > 0;
  const autoRefreshEnabled = enabled && !isAutoRefreshDisabled;
  const [manualRefreshVersion, setManualRefreshVersion] = useState(0);
  const autoRefreshHandler = onAutoRefresh ?? onRefresh;
  const labelTitleRef = useRef<HTMLSpanElement | null>(null);
  const labelValueRef = useRef<HTMLSpanElement | null>(null);
  const currentLabelTitle = autoRefreshEnabled ? formatAutoRefreshTitle(label) : offLabel;
  const currentLabelValue = autoRefreshEnabled ? formatAutoRefreshValue(value, fastLabel) : '';
  const updateDisplayLabel = useCallback((displaySeconds: number) => {
    const titleElement = labelTitleRef.current;
    const valueElement = labelValueRef.current;
    if (titleElement) {
      titleElement.textContent = autoRefreshEnabled ? formatAutoRefreshTitle(label) : offLabel;
    }
    if (valueElement) {
      valueElement.textContent = autoRefreshEnabled ? formatAutoRefreshValue(displaySeconds, fastLabel) : '';
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
      <div className={styles.control} role="group" aria-label={ariaLabel}>
        <button
          type="button"
          aria-label={refreshAriaLabel}
          aria-busy={isRefreshing}
          disabled={isDisabled || isRefreshing}
          className={styles.refresh}
          onClick={handleRefresh}
        >
          <RefreshCw className={`h-4 w-4 shrink-0 ${isRefreshing ? 'animate-spin' : ''}`} />
          <span className={styles.label}>
            <span aria-hidden="true" className={styles.labelSizer}>{offLabel}</span>
            <span aria-hidden="true" className={styles.labelSizer}>{formatAutoRefreshTitle(label)}</span>
            <span ref={setLabelTitleElement} />
          </span>
          <span ref={setLabelValueElement} className={styles.value} />
        </button>
        <ToolbarMenu
          ariaLabel={ariaLabel}
          rootClassName={styles.menu}
          label={null}
          className={styles.arrow}
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
      </div>
      {afterRefresh}
      {afterAutoRefresh}
    </>
  );
});
