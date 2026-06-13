import { lazy } from 'react';
import type { ComponentType, LazyExoticComponent } from 'react';
import { recoverFromDynamicImportError } from '../shared/utils/dynamicImportRecovery';

export type RoutePreloadModule<TProps = Record<string, never>> = {
  default: ComponentType<TProps>;
  preloadAccountsPageContent?: () => Promise<unknown>;
  preloadUserUsageContent?: () => Promise<unknown>;
};

export type PreloadableLazyComponent<TProps = Record<string, never>> =
  LazyExoticComponent<ComponentType<TProps>> & {
    preload: () => Promise<RoutePreloadModule<TProps>>;
  };

type AnyPreloadableLazyComponent = PreloadableLazyComponent<any>;

export function lazyWithPreload<TProps>(
  load: () => Promise<RoutePreloadModule<TProps>>,
): PreloadableLazyComponent<TProps> {
  let promise: Promise<RoutePreloadModule<TProps>> | undefined;
  const preload = () => {
    promise ??= load().catch((error: unknown) => {
      promise = undefined;
      recoverFromDynamicImportError(error, 'route-preload');
      throw error;
    });
    return promise;
  };
  const Component = lazy(preload) as PreloadableLazyComponent<TProps>;
  Component.preload = preload;
  return Component;
}

export const SetupPage = lazyWithPreload(() => import('../pages/SetupPage'));
export const LoginPage = lazyWithPreload(() => import('../pages/LoginPage'));
export const PluginPage = lazyWithPreload(() => import('../pages/PluginPage'));
export const PublicHomePage = lazyWithPreload(() => import('../pages/HomePage'));
export const DocsPage = lazyWithPreload(() => import('../pages/DocsPage'));
export const DashboardPage = lazyWithPreload(() => import('../pages/DashboardPage'));
export const UserOverviewPage = lazyWithPreload(() => import('../pages/user/UserOverviewPage'));
export const UsersPage = lazyWithPreload(() => import('../pages/admin/UsersPage'));
export const AccountsPage = lazyWithPreload(() => import('../pages/admin/AccountsPage'));
export const GroupsPage = lazyWithPreload(() => import('../pages/admin/GroupsPage'));
export const MonitorPage = lazyWithPreload(() => import('../pages/admin/MonitorPage'));
export const SubscriptionsPage = lazyWithPreload(() => import('../pages/admin/SubscriptionsPage'));
export const ProxiesPage = lazyWithPreload(() => import('../pages/admin/ProxiesPage'));
export const UsagePage = lazyWithPreload(() => import('../pages/admin/UsagePage'));
export const PluginsPage = lazyWithPreload(() => import('../pages/admin/PluginsPage'));
export const SettingsPage = lazyWithPreload(() => import('../pages/admin/SettingsPage'));
export const ProfilePage = lazyWithPreload(() => import('../pages/user/ProfilePage'));
export const UserKeysPage = lazyWithPreload(() => import('../pages/user/UserKeysPage'));
export const UserUsagePage = lazyWithPreload(() => import('../pages/user/UserUsagePage'));

const ROUTE_PRELOADS = new Map<string, AnyPreloadableLazyComponent[]>([
  ['/', [DashboardPage, UserOverviewPage]],
  ['/home', [PublicHomePage]],
  ['/login', [LoginPage]],
  ['/setup', [SetupPage]],
  ['/docs', [DocsPage]],
  ['/profile', [ProfilePage]],
  ['/keys', [UserKeysPage]],
  ['/usage', [UserUsagePage]],
  ['/chat', [PluginPage]],
  ['/studio', [PluginPage]],
  ['/admin/users', [UsersPage]],
  ['/admin/accounts', [AccountsPage]],
  ['/admin/groups', [GroupsPage]],
  ['/admin/monitor', [MonitorPage]],
  ['/admin/subscriptions', [SubscriptionsPage]],
  ['/admin/proxies', [ProxiesPage]],
  ['/admin/usage', [UsagePage]],
  ['/admin/plugins', [PluginsPage]],
  ['/admin/settings', [SettingsPage]],
]);

function normalizePreloadPath(path: string) {
  const [pathname = '/'] = path.split(/[?#]/, 1);
  return pathname || '/';
}

export function preloadRoutePage(
  page: AnyPreloadableLazyComponent,
  options: { deep?: boolean } = {},
) {
  return page.preload().then((module) => (
    options.deep === false
      ? undefined
      : Promise.all([
        module.preloadAccountsPageContent?.(),
        module.preloadUserUsageContent?.(),
      ]).then(() => undefined)
  ));
}

export function preloadRoutePath(path: string, options: { deep?: boolean } = {}) {
  const pathname = normalizePreloadPath(path);
  const pages = pathname.startsWith('/plugins/')
    ? [PluginPage]
    : ROUTE_PRELOADS.get(pathname);

  if (!pages?.length) return Promise.resolve();
  return Promise.all(pages.map((page) => preloadRoutePage(page, options))).then(() => undefined);
}
