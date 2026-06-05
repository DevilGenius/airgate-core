import {
  memo,
  useCallback,
  type MouseEvent as ReactMouseEvent,
  type PointerEvent as ReactPointerEvent,
} from 'react';
import { Button, Tooltip } from '@heroui/react';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { ChevronLeft, ChevronRight, HelpCircle } from 'lucide-react';
import { settingsApi } from '../../shared/api/settings';
import { effectiveDocUrl } from '../../shared/utils/docUrl';
import { defaultLogoUrl, useSiteSettings } from '../providers/SiteSettingsProvider';
import { preloadRoutePath } from '../routePreloads';
import type { MenuItem, MenuSection } from './menuModel';
import { isMenuItemActive } from './navigationUtils';
import type { ShellIdentity } from './useShellIdentity';
import { useMenuNavigation } from './useMenuNavigation';

export const SIDEBAR_COLLAPSED_STORAGE_KEY = 'airgate:sidebar:collapsed';

interface AppSidebarProps {
  collapsed: boolean;
  isMobile: boolean;
  mobileOpen: boolean;
  onCollapsedChange: (collapsed: boolean) => void;
  onMobileOpenChange: (open: boolean) => void;
  sections: MenuSection[];
  shell: ShellIdentity;
}

interface SidebarNavLinkProps {
  activePath: string;
  collapsed: boolean;
  item: MenuItem;
  label: string;
  onNavigate: (path: string) => void;
}

interface NavigationModifierState {
  altKey: boolean;
  ctrlKey: boolean;
  metaKey: boolean;
  shiftKey: boolean;
}

function isModifiedNavigation(event: NavigationModifierState) {
  return event.altKey || event.ctrlKey || event.metaKey || event.shiftKey;
}

const SidebarNavLink = memo(function SidebarNavLink({
  activePath,
  collapsed,
  item,
  label,
  onNavigate,
}: SidebarNavLinkProps) {
  const active = isMenuItemActive(item.path, activePath);
  const preload = useCallback(() => {
    void preloadRoutePath(item.path, { deep: false });
  }, [item.path]);
  const navigate = useCallback(() => {
    onNavigate(item.path);
  }, [item.path, onNavigate]);

  const handlePointerDown = useCallback((event: ReactPointerEvent<HTMLAnchorElement>) => {
    if (event.button !== 0 || isModifiedNavigation(event)) return;
    event.preventDefault();
    navigate();
  }, [navigate]);

  const handleClick = useCallback((event: ReactMouseEvent<HTMLAnchorElement>) => {
    if (event.button !== 0 || isModifiedNavigation(event)) return;
    event.preventDefault();
    if (event.detail === 0) navigate();
  }, [navigate]);

  return (
    <a
      href={item.path}
      data-active={active ? 'true' : undefined}
      className={`ag-sidebar-nav-item group relative flex items-center transition-colors duration-150 ${collapsed ? 'mx-auto h-10 w-10 justify-center p-0' : 'px-2 py-1.5'}`}
      onClick={handleClick}
      onFocus={preload}
      onPointerDown={handlePointerDown}
      onPointerEnter={preload}
    >
      <span className="flex shrink-0 items-center justify-center">{item.icon}</span>
      {!collapsed && (
        <span className="ag-sidebar-nav-item-label truncate">{label}</span>
      )}
    </a>
  );
});

const SidebarBrand = memo(function SidebarBrand({
  collapsed,
  coreVersion,
  isMobile,
  onCollapsedChange,
  shell,
}: {
  collapsed: boolean;
  coreVersion?: { go_version: string; platform: string; version: string };
  isMobile: boolean;
  onCollapsedChange: (collapsed: boolean) => void;
  shell: ShellIdentity;
}) {
  const { t } = useTranslation();
  const site = useSiteSettings();

  return (
    <>
      <div className="flex h-20 items-center px-4">
        <div className={`flex min-w-0 ${collapsed ? 'w-full flex-col items-center justify-center' : 'w-full items-center gap-3'}`}>
          <div className="relative flex h-10 w-10 shrink-0 items-center justify-center overflow-hidden rounded-[var(--radius)] bg-primary-subtle">
            <img src={site.site_logo || defaultLogoUrl} alt="" className="h-full w-full object-cover" />
          </div>
          {!collapsed && (
            <div className="min-w-0 flex-1">
              <div className="flex min-w-0 items-center gap-1.5">
                <h1 className="truncate text-sm font-semibold text-text">{shell.displayName}</h1>
                {coreVersion?.version && (
                  <span
                    className="shrink-0 text-[9px] text-text-tertiary font-mono"
                    title={`${coreVersion.version} · ${coreVersion.platform} · ${coreVersion.go_version}`}
                  >
                    {coreVersion.version}
                  </span>
                )}
              </div>
              <p className="mt-0.5 truncate text-xs text-text-tertiary">{shell.roleLabel}</p>
            </div>
          )}
          {!isMobile && !collapsed && (
            <Button
              aria-label={t('nav.collapse_sidebar', 'Collapse sidebar')}
              className="ag-sidebar-collapse-button shrink-0"
              isIconOnly
              size="sm"
              variant="ghost"
              onPress={() => onCollapsedChange(true)}
            >
              <ChevronLeft className="h-4 w-4" />
            </Button>
          )}
        </div>
      </div>

      {!isMobile && collapsed && (
        <div className="mb-1 flex justify-center">
          <Button
            aria-label={t('nav.expand_sidebar', 'Expand sidebar')}
            className="ag-sidebar-collapse-button"
            isIconOnly
            size="sm"
            variant="ghost"
            onPress={() => onCollapsedChange(false)}
          >
            <ChevronRight className="h-4 w-4" />
          </Button>
        </div>
      )}
    </>
  );
});

const SidebarNav = memo(function SidebarNav({
  activePath,
  collapsed,
  onNavigate,
  sections,
}: {
  activePath: string;
  collapsed: boolean;
  onNavigate: (path: string) => void;
  sections: MenuSection[];
}) {
  const { t } = useTranslation();

  return (
    <nav className={`ag-sidebar-nav flex-1 overflow-y-auto pb-4 space-y-5 ${collapsed ? 'px-0' : 'px-3'}`}>
      {sections.map((section, sectionIndex) => (
        <div key={section.titleKey ?? `section-${sectionIndex}`}>
          {section.titleKey && !collapsed && (
            <p className="px-2.5 pb-2 text-[10px] font-medium uppercase text-text-tertiary">
              {t(section.titleKey)}
            </p>
          )}
          {collapsed && sectionIndex > 0 && (
            <div className="mx-3 mb-2.5 h-px bg-border" />
          )}
          <div className="space-y-1">
            {section.items.map((item) => {
              const label = t(item.labelKey, { defaultValue: item.labelKey });
              const link = (
                <SidebarNavLink
                  key={item.path}
                  activePath={activePath}
                  collapsed={collapsed}
                  item={item}
                  label={label}
                  onNavigate={onNavigate}
                />
              );

              return collapsed ? (
                <Tooltip key={item.path}>
                  <Tooltip.Trigger className="block w-full">{link}</Tooltip.Trigger>
                  <Tooltip.Content>{label}</Tooltip.Content>
                </Tooltip>
              ) : link;
            })}
          </div>
        </div>
      ))}
    </nav>
  );
});

const SidebarFooter = memo(function SidebarFooter({ collapsed, isMobile }: { collapsed: boolean; isMobile: boolean }) {
  const { t } = useTranslation();
  const site = useSiteSettings();

  const openDocs = () => {
    window.location.href = effectiveDocUrl(site.doc_url).href;
  };

  return (
    <div className="space-y-1 border-t border-border p-3">
      {!collapsed && (
        <Button
          className="w-full justify-center"
          size="sm"
          variant="ghost"
          onPress={openDocs}
        >
          <HelpCircle className="h-4 w-4" />
          {t('nav.docs')}
        </Button>
      )}
      {!isMobile && collapsed && (
        <Button
          aria-label={t('nav.docs')}
          className="w-full"
          isIconOnly
          size="sm"
          variant="ghost"
          onPress={openDocs}
        >
          <HelpCircle className="h-4 w-4" />
        </Button>
      )}
    </div>
  );
});

export const AppSidebar = memo(function AppSidebar({
  collapsed,
  isMobile,
  mobileOpen,
  onCollapsedChange,
  onMobileOpenChange,
  sections,
  shell,
}: AppSidebarProps) {
  const sidebarCollapsed = isMobile ? false : collapsed;
  const { activePath, navigate } = useMenuNavigation({
    closeMobileMenu: () => onMobileOpenChange(false),
    isMobile,
  });
  const { data: coreVersion } = useQuery({
    queryKey: ['core-version'],
    queryFn: () => settingsApi.getCoreVersion(),
    enabled: shell.isAdmin && !shell.isAPIKeySession,
    staleTime: 5 * 60_000,
    refetchOnWindowFocus: false,
  });

  const content = (
    <>
      <SidebarBrand
        collapsed={sidebarCollapsed}
        coreVersion={coreVersion}
        isMobile={isMobile}
        onCollapsedChange={onCollapsedChange}
        shell={shell}
      />
      <SidebarNav
        activePath={activePath}
        collapsed={sidebarCollapsed}
        onNavigate={navigate}
        sections={sections}
      />
      <SidebarFooter collapsed={sidebarCollapsed} isMobile={isMobile} />
    </>
  );

  if (isMobile) {
    return (
      <aside
        className="fixed inset-y-0 left-0 z-50 flex flex-col bg-surface border-r border-border transition-transform duration-150 ease-out"
        style={{ width: 'var(--ag-sidebar-width)', transform: mobileOpen ? 'translateX(0)' : 'translateX(-100%)' }}
      >
        {content}
      </aside>
    );
  }

  return (
    <aside
      className="relative flex flex-col border-r border-border bg-surface transition-[width] duration-150 ease-out"
      style={{ width: collapsed ? 'var(--ag-sidebar-collapsed)' : 'var(--ag-sidebar-width)' }}
    >
      {content}
    </aside>
  );
});
