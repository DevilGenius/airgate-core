import { useCallback, useEffect, useState } from 'react';
import { AppHeader } from './AppHeader';
import { AppMainOutlet } from './AppMainOutlet';
import { AppSidebar, SIDEBAR_COLLAPSED_STORAGE_KEY } from './AppSidebar';
import { ShellLoadingLine } from './ShellLoadingLine';
import { useAppMenuModel } from './menuModel';
import { useShellIdentity } from './useShellIdentity';
import { useIsMobile } from '../../shared/hooks/useMediaQuery';
import { usePersistentBoolean } from '../../shared/hooks/usePersistentBoolean';
import { useSiteSettings } from '../providers/SiteSettingsProvider';

export function AppShell() {
  const shell = useShellIdentity();
  const site = useSiteSettings();
  const isMobile = useIsMobile();
  const [collapsed, setCollapsed] = usePersistentBoolean(SIDEBAR_COLLAPSED_STORAGE_KEY, false);
  const [mobileOpen, setMobileOpen] = useState(false);
  const { healthInstalled, sections } = useAppMenuModel({
    isAdmin: shell.isAdmin,
    isAPIKeySession: shell.isAPIKeySession,
  });
  const closeMobileMenu = useCallback(() => setMobileOpen(false), []);
  const openMobileMenu = useCallback(() => setMobileOpen(true), []);

  useEffect(() => {
    setMobileOpen(false);
  }, [isMobile]);

  useEffect(() => {
    if (!mobileOpen) return undefined;
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = '';
    };
  }, [mobileOpen]);

  useEffect(() => {
    document.title = site.site_name || 'AirGate';
  }, [site.site_name]);

  return (
    <div className="fixed inset-0 flex overflow-hidden bg-bg text-text">
      <ShellLoadingLine />

      {isMobile && mobileOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/40"
          onClick={closeMobileMenu}
        />
      )}

      <AppSidebar
        collapsed={collapsed}
        isMobile={isMobile}
        mobileOpen={mobileOpen}
        onCollapsedChange={setCollapsed}
        onMobileOpenChange={setMobileOpen}
        sections={sections}
        shell={shell}
      />

      <div className="relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        <AppHeader
          isMobile={isMobile}
          onOpenMobileMenu={openMobileMenu}
          shell={shell}
          showStatusEntry={healthInstalled}
        />
        <AppMainOutlet />
      </div>
    </div>
  );
}
