import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';

export type AccountModalPerfTarget = 'create' | 'edit' | 'delete' | 'bulk-edit' | 'bulk-delete' | 'bulk-refresh' | 'test' | 'stats';

type AccountModalPerfSample = {
  rows: number;
  startedAt: number;
  target: AccountModalPerfTarget;
};

export type AccountSelectionPerfAction = 'select-visible' | 'select-row';

export type AccountToolbarMenuPerfTarget = 'platform' | 'state' | 'type' | 'group' | 'proxy' | 'auto-refresh';

type AccountToolbarMenuPerfSample = {
  rows: number;
  startedAt: number;
  target: AccountToolbarMenuPerfTarget;
};

const ACCOUNT_MODAL_ARIA_HIDE_MAX_FRAMES = 8;
const ACCOUNT_MODAL_ARIA_HIDE_FALLBACK_MS = 160;

function readElementHeight(element: HTMLElement | null) {
  return element ? Math.ceil(element.getBoundingClientRect().height) : 0;
}

function getNodeCount(selector: string) {
  if (typeof document === 'undefined') return 0;
  return document.querySelectorAll(selector).length;
}

function getCheckedAccountRowCount() {
  if (typeof document === 'undefined') return 0;
  return document.querySelectorAll('tbody input.ag-table-selection-checkbox:checked').length;
}

function getAccountsTableFrameContentVisibility() {
  if (typeof document === 'undefined' || typeof window === 'undefined') return '';
  const frame = document.querySelector('.ag-accounts-table-frame');
  return frame instanceof HTMLElement ? window.getComputedStyle(frame).contentVisibility : '';
}

export function useAccountModalRootIsolation(isActive: boolean) {
  useLayoutEffect(() => {
    if (!isActive || typeof document === 'undefined' || typeof window === 'undefined') {
      return undefined;
    }

    const root = document.getElementById('root');
    if (!root) return undefined;

    const hadTopLayer = root.hasAttribute('data-react-aria-top-layer');
    const previousAriaHidden = root.getAttribute('aria-hidden');
    root.setAttribute('data-react-aria-top-layer', 'true');

    let frameId = 0;
    let frameCount = 0;
    let isApplied = false;
    let timeoutId = 0;
    const applyHidden = () => {
      if (isApplied) return;
      isApplied = true;
      if (timeoutId !== 0) {
        window.clearTimeout(timeoutId);
        timeoutId = 0;
      }
      root.setAttribute('aria-hidden', 'true');
    };
    const applyAriaHidden = () => {
      frameId = 0;
      if (root.contains(document.activeElement) && frameCount < ACCOUNT_MODAL_ARIA_HIDE_MAX_FRAMES) {
        frameCount += 1;
        frameId = window.requestAnimationFrame(applyAriaHidden);
        return;
      }
      applyHidden();
    };
    timeoutId = window.setTimeout(applyHidden, ACCOUNT_MODAL_ARIA_HIDE_FALLBACK_MS);
    frameId = window.requestAnimationFrame(applyAriaHidden);

    return () => {
      if (frameId !== 0) {
        window.cancelAnimationFrame(frameId);
      }
      if (timeoutId !== 0) {
        window.clearTimeout(timeoutId);
      }
      if (!hadTopLayer) {
        root.removeAttribute('data-react-aria-top-layer');
      }
      if (previousAriaHidden == null) {
        root.removeAttribute('aria-hidden');
      } else {
        root.setAttribute('aria-hidden', previousAriaHidden);
      }
    };
  }, [isActive]);
}

export function useMeasuredElementHeight<T extends HTMLElement>() {
  const [height, setHeight] = useState(0);
  const [observedElement, setObservedElement] = useState<T | null>(null);

  const ref = useCallback((element: T | null) => {
    setObservedElement(element);
    const nextHeight = readElementHeight(element);
    if (nextHeight > 0) {
      setHeight(nextHeight);
    }
  }, []);

  useLayoutEffect(() => {
    const element = observedElement;
    if (!element || typeof ResizeObserver === 'undefined') return undefined;

    let frameId = 0;
    const observer = new ResizeObserver((entries) => {
      const nextHeight = Math.ceil(entries[0]?.contentRect.height ?? 0);
      if (nextHeight <= 0) return;
      window.cancelAnimationFrame(frameId);
      frameId = window.requestAnimationFrame(() => {
        setHeight((current) => (current === nextHeight ? current : nextHeight));
      });
    });

    observer.observe(element);
    return () => {
      window.cancelAnimationFrame(frameId);
      observer.disconnect();
    };
  }, [observedElement]);

  return [ref, height] as const;
}

export function useAccountModalPerfMonitor(isModalOpen: boolean) {
  const pendingSampleRef = useRef<AccountModalPerfSample | null>(null);

  const markModalOpenStart = useCallback((target: AccountModalPerfTarget, rows: number) => {
    if (!import.meta.env.DEV || typeof performance === 'undefined') return;

    pendingSampleRef.current = {
      rows,
      startedAt: performance.now(),
      target,
    };
  }, []);

  useLayoutEffect(() => {
    if (!isModalOpen || !import.meta.env.DEV || typeof performance === 'undefined') return undefined;

    const sample = pendingSampleRef.current;
    if (!sample) return undefined;
    pendingSampleRef.current = null;

    const committedAt = performance.now();
    let timeoutId: number | undefined;
    const frameId = window.requestAnimationFrame(() => {
      const paintedAt = performance.now();
      timeoutId = window.setTimeout(() => {
        const metric = {
          commitMs: Math.round((committedAt - sample.startedAt) * 10) / 10,
          documentNodesAfter: getNodeCount('*'),
          paintMs: Math.round((paintedAt - sample.startedAt) * 10) / 10,
          rows: sample.rows,
          tableFrameContentVisibility: getAccountsTableFrameContentVisibility(),
          tableMountedAfter: getNodeCount('.ag-accounts-table [data-slot="tbody"]') > 0,
          tableNodesAfter: getNodeCount('.ag-accounts-table *'),
          target: sample.target,
        };
        console.info('[accounts:modal-open]', JSON.stringify(metric));
      }, 0);
    });

    return () => {
      window.cancelAnimationFrame(frameId);
      if (timeoutId !== undefined) {
        window.clearTimeout(timeoutId);
      }
    };
  }, [isModalOpen]);

  return markModalOpenStart;
}

export function useAccountSelectionPerfMonitor() {
  const lastLogAtRef = useRef(0);

  return useCallback((
    action: AccountSelectionPerfAction,
    rows: number,
    nextSelected: boolean,
    work: () => number,
  ) => {
    if (!import.meta.env.DEV || typeof performance === 'undefined') {
      return work();
    }

    const startedAt = performance.now();
    const shouldLog = startedAt - lastLogAtRef.current >= 250;
    const changed = work();
    const workedAt = performance.now();
    if (!shouldLog) return changed;
    lastLogAtRef.current = workedAt;

    const frameId = window.requestAnimationFrame(() => {
      const paintedAt = performance.now();
      window.setTimeout(() => {
        const metric = {
          action,
          changed,
          checkedAfter: getCheckedAccountRowCount(),
          nextSelected,
          paintMs: Math.round((paintedAt - startedAt) * 10) / 10,
          rows,
          selectedCount: getCheckedAccountRowCount(),
          tableNodesAfter: getNodeCount('.ag-accounts-table *'),
          workMs: Math.round((workedAt - startedAt) * 10) / 10,
        };
        console.info('[accounts:selection]', JSON.stringify(metric));
      }, 0);
    });

    window.setTimeout(() => window.cancelAnimationFrame(frameId), 1000);
    return changed;
  }, []);
}

export function useAccountToolbarMenuPerfMonitor() {
  const pendingSampleRef = useRef<AccountToolbarMenuPerfSample | null>(null);
  const paintFrameIdRef = useRef<number | null>(null);
  const paintTimeoutIdRef = useRef<number | null>(null);

  const markToolbarMenuOpenStart = useCallback((target: AccountToolbarMenuPerfTarget, rows: number) => {
    if (!import.meta.env.DEV || typeof performance === 'undefined' || typeof window === 'undefined') return;

    const sample: AccountToolbarMenuPerfSample = {
      rows,
      startedAt: performance.now(),
      target,
    };
    pendingSampleRef.current = sample;

    if (paintFrameIdRef.current != null && typeof window !== 'undefined') {
      window.cancelAnimationFrame(paintFrameIdRef.current);
    }
    if (paintTimeoutIdRef.current != null) {
      window.clearTimeout(paintTimeoutIdRef.current);
      paintTimeoutIdRef.current = null;
    }

    paintFrameIdRef.current = window.requestAnimationFrame(() => {
      paintFrameIdRef.current = null;
      const pendingSample = pendingSampleRef.current;
      if (!pendingSample) return;
      pendingSampleRef.current = null;
      const paintedAt = performance.now();
      paintTimeoutIdRef.current = window.setTimeout(() => {
        paintTimeoutIdRef.current = null;
        const metric = {
          documentNodesAfter: getNodeCount('*'),
          paintMs: Math.round((paintedAt - pendingSample.startedAt) * 10) / 10,
          rows: pendingSample.rows,
          tableFrameContentVisibility: getAccountsTableFrameContentVisibility(),
          tableMountedAfter: getNodeCount('.ag-accounts-table [data-slot="tbody"]') > 0,
          tableNodesAfter: getNodeCount('.ag-accounts-table *'),
          target: pendingSample.target,
        };
        console.info('[accounts:toolbar-menu-open]', JSON.stringify(metric));
      }, 0);
    });
  }, []);

  useEffect(() => () => {
    if (paintFrameIdRef.current != null && typeof window !== 'undefined') {
      window.cancelAnimationFrame(paintFrameIdRef.current);
    }
    if (paintTimeoutIdRef.current != null && typeof window !== 'undefined') {
      window.clearTimeout(paintTimeoutIdRef.current);
    }
  }, []);

  return markToolbarMenuOpenStart;
}
