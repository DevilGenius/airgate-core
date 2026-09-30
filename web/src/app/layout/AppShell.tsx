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
import { useTranslation } from 'react-i18next';
import styles from './AppShell.module.css';

export function AppShell() {
  const { t } = useTranslation();
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
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const previousOverflow = document.body.style.overflow;
    const sidebar = document.querySelector<HTMLElement>('[data-mobile-sidebar]');
    const focusable = () => Array.from(sidebar?.querySelectorAll<HTMLElement>('a[href], button:not([disabled]), [tabindex="0"]') ?? [])
      .filter((element) => element.getClientRects().length > 0);
    focusable()[0]?.focus();
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault();
        closeMobileMenu();
      } else if (event.key === 'Tab') {
        const elements = focusable();
        const first = elements[0];
        const last = elements[elements.length - 1];
        if (event.shiftKey && document.activeElement === first) {
          event.preventDefault();
          last?.focus();
        } else if (!event.shiftKey && document.activeElement === last) {
          event.preventDefault();
          first?.focus();
        }
      }
    };
    document.addEventListener('keydown', handleKeyDown);
    document.body.style.overflow = 'hidden';
    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener('keydown', handleKeyDown);
      previousFocus?.focus();
    };
  }, [closeMobileMenu, mobileOpen]);

  useEffect(() => {
    document.title = site.site_name || 'AirGate';
  }, [site.site_name]);

  return (
    <div className={`${styles.shell} ag-app-shell fixed inset-0 flex overflow-hidden bg-bg text-text`}>
      <a className={styles.skipLink} href="#main-content">{t('nav.skip_content', 'Skip to content')}</a>
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

      <div inert={isMobile && mobileOpen} className="relative flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">
        <AppHeader
          isMobile={isMobile}
          mobileMenuOpen={mobileOpen}
          onOpenMobileMenu={openMobileMenu}
          shell={shell}
          showStatusEntry={healthInstalled}
        />
        <AppMainOutlet />
      </div>
    </div>
  );
}
