import { startTransition, useCallback, useEffect, useRef, useState } from 'react';
import { useRouter, useRouterState } from '@tanstack/react-router';
import { preloadRoutePath } from '../routePreloads';
import { normalizePath, scheduleAfterPaint } from './navigationUtils';

interface UseMenuNavigationOptions {
  closeMobileMenu: () => void;
  isMobile: boolean;
}

export function useMenuNavigation({ closeMobileMenu, isMobile }: UseMenuNavigationOptions) {
  const router = useRouter();
  const routerPath = useRouterState({ select: (s) => s.location.pathname });
  const routerStatus = useRouterState({ select: (s) => s.status });
  const [requestedPath, setRequestedPath] = useState<string | null>(null);
  const cancelScheduledNavigationRef = useRef<(() => void) | null>(null);
  const navigationStartedRef = useRef(false);
  const activePath = requestedPath ?? routerPath;

  const cancelScheduledNavigation = useCallback(() => {
    cancelScheduledNavigationRef.current?.();
    cancelScheduledNavigationRef.current = null;
  }, []);

  const navigate = useCallback((path: string) => {
    const nextPath = normalizePath(path);
    setRequestedPath(nextPath);
    navigationStartedRef.current = false;
    if (isMobile) closeMobileMenu();
    cancelScheduledNavigation();
    void preloadRoutePath(nextPath, { deep: false });

    if (nextPath === normalizePath(routerPath)) {
      setRequestedPath(null);
      return;
    }

    cancelScheduledNavigationRef.current = scheduleAfterPaint(() => {
      cancelScheduledNavigationRef.current = null;
      navigationStartedRef.current = true;
      startTransition(() => {
        void router.navigate({ to: nextPath as never });
      });
    });
  }, [cancelScheduledNavigation, closeMobileMenu, isMobile, router, routerPath]);

  useEffect(() => cancelScheduledNavigation, [cancelScheduledNavigation]);

  useEffect(() => {
    if (!requestedPath) return;
    const currentPath = normalizePath(routerPath);
    if (currentPath === requestedPath) {
      setRequestedPath(null);
      navigationStartedRef.current = false;
      return;
    }
    if (navigationStartedRef.current && routerStatus !== 'pending') {
      setRequestedPath(null);
      navigationStartedRef.current = false;
    }
  }, [requestedPath, routerPath, routerStatus]);

  return { activePath, navigate };
}
