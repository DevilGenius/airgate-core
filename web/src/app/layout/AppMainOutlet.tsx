import { memo, startTransition, useEffect, useState } from 'react';
import { Outlet, useRouterState } from '@tanstack/react-router';
import { PageFooterProvider } from '../../shared/components/PageFooter';
import { normalizePath, scheduleAfterPaint } from './navigationUtils';
import styles from './AppShell.module.css';

const ROUTE_CONTENT_ACTIVATION_DELAY_MS = 300;

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

  return (
    <main id="main-content" tabIndex={-1} className={`${styles.main} min-h-0 flex flex-1 flex-col bg-bg ag-main`}>
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
