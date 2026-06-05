import { memo, startTransition, useEffect, useState } from 'react';
import { Outlet, useRouterState } from '@tanstack/react-router';
import { normalizePath, scheduleAfterPaint } from './navigationUtils';

const ROUTE_CONTENT_ACTIVATION_DELAY_MS = 140;

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

  return (
    <main className="min-h-0 flex-1 overflow-auto bg-bg pt-12 ag-main">
      <div className="ag-main-content mx-auto w-full max-w-[1920px] p-4 md:p-6 2xl:p-8">
        {ready ? <Outlet /> : <RouteRenderPlaceholder />}
      </div>
    </main>
  );
});
