import { memo, startTransition, useEffect, useState } from 'react';
import { Outlet, useRouterState } from '@tanstack/react-router';
import { PageFooterProvider } from '../../shared/components/PageFooter';
import { normalizePath, scheduleAfterPaint } from './navigationUtils';

const ROUTE_CONTENT_ACTIVATION_DELAY_MS = 300;
const COMPACT_FOOTER_HEIGHT_PX = 50;

function RouteRenderPlaceholder() {
  return <div className="min-h-[320px]" aria-hidden="true" />;
}

export const AppMainOutlet = memo(function AppMainOutlet() {
  const routerPath = useRouterState({ select: (s) => s.location.pathname });
  const [activatedPath, setActivatedPath] = useState(routerPath);
  const ready = normalizePath(activatedPath) === normalizePath(routerPath);

  useEffect(() => {
    if (ready) return undefined;
    return scheduleAfterPaint(() => {
      startTransition(() => {
        setActivatedPath(routerPath);
      });
    }, ROUTE_CONTENT_ACTIVATION_DELAY_MS);
  }, [ready, routerPath]);

  const [footerContainer, setFooterContainer] = useState<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!footerContainer || typeof ResizeObserver === 'undefined') return undefined;
    const shell = footerContainer.closest<HTMLElement>('.ag-app-shell');
    if (!shell) return undefined;

    const borderTop = Number.parseFloat(getComputedStyle(footerContainer).borderTopWidth) || 0;
    const updateHeight = (borderBoxHeight: number) => {
      // The container owns the separator border. Exclude it before sharing
      // the content height with the sidebar, otherwise every observer pass
      // would add another pixel to both flex regions.
      const contentHeight = Math.max(COMPACT_FOOTER_HEIGHT_PX, borderBoxHeight - borderTop);
      const nextValue = `${Math.ceil(contentHeight)}px`;
      if (shell.style.getPropertyValue('--ag-shell-footer-height') === nextValue) return;
      shell.style.setProperty('--ag-shell-footer-height', nextValue);
    };
    const observer = new ResizeObserver(([entry]) => {
      const borderBoxHeight = entry?.borderBoxSize?.[0]?.blockSize
        ?? entry?.target.getBoundingClientRect().height
        ?? 0;
      updateHeight(borderBoxHeight);
    });
    observer.observe(footerContainer);
    updateHeight(footerContainer.getBoundingClientRect().height);
    return () => {
      observer.disconnect();
      shell.style.removeProperty('--ag-shell-footer-height');
    };
  }, [footerContainer]);

  return (
    <main className="min-h-0 flex flex-1 flex-col bg-bg pt-12 ag-main">
      <PageFooterProvider container={footerContainer}>
        <div className="ag-main-scroll min-h-0 flex-1 overflow-auto">
          <div className="ag-main-content mx-auto w-full max-w-[1920px]">
            {ready ? <Outlet /> : <RouteRenderPlaceholder />}
          </div>
        </div>
        <div ref={setFooterContainer} className="ag-page-footer-container empty:hidden" />
      </PageFooterProvider>
    </main>
  );
});
