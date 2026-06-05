import { useMemo, useRef, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import {
  Activity,
  CreditCard,
  Globe,
  IdCard,
  LayoutDashboard,
  LayoutList,
  ListChecks,
  ListOrdered,
  MessageSquareMore,
  Palette,
  Puzzle,
  Settings,
  UserRoundCog,
  UserRoundKey,
  Users,
  Wallet,
  Wrench,
} from 'lucide-react';
import { pluginsApi } from '../../shared/api/plugins';
import { queryKeys } from '../../shared/queryKeys';

export interface MenuItem {
  icon: ReactNode;
  labelKey: string;
  path: string;
  sectionKey?: string;
}

export interface MenuSection {
  items: MenuItem[];
  titleKey?: string;
}

interface AppMenuModelInput {
  isAdmin: boolean;
  isAPIKeySession: boolean;
}

interface PluginMenuModel {
  adminItems: MenuItem[];
  healthInstalled: boolean;
  userItems: MenuItem[];
}

const PLUGIN_MENU_LOADING_POLL_MS = 1_000;
const PLUGIN_MENU_LOADING_TIMEOUT_MS = 60_000;

const adminMenuItems: MenuItem[] = [
  { path: '/', labelKey: 'nav.dashboard', icon: <LayoutDashboard className="h-5 w-5" />, sectionKey: 'nav.overview' },
  { path: '/admin/users', labelKey: 'nav.users', icon: <Users className="h-5 w-5" />, sectionKey: 'nav.management' },
  { path: '/admin/accounts', labelKey: 'nav.accounts', icon: <IdCard className="h-5 w-5" /> },
  { path: '/admin/groups', labelKey: 'nav.groups', icon: <LayoutList className="h-5 w-5" /> },
  { path: '/admin/subscriptions', labelKey: 'nav.subscriptions', icon: <CreditCard className="h-5 w-5" /> },
  { path: '/admin/proxies', labelKey: 'nav.proxies', icon: <Globe className="h-5 w-5" /> },
  { path: '/admin/usage', labelKey: 'nav.usage', icon: <Activity className="h-5 w-5" /> },
  { path: '/admin/plugins', labelKey: 'nav.plugins', icon: <Wrench className="h-5 w-5" />, sectionKey: 'nav.system' },
  { path: '/admin/settings', labelKey: 'nav.settings', icon: <Settings className="h-5 w-5" /> },
];

const userMenuItems: MenuItem[] = [
  { path: '/', labelKey: 'nav.my_overview', icon: <LayoutDashboard className="h-5 w-5" />, sectionKey: 'nav.personal' },
  { path: '/profile', labelKey: 'nav.profile', icon: <UserRoundCog className="h-5 w-5" /> },
  { path: '/keys', labelKey: 'nav.my_keys', icon: <UserRoundKey className="h-5 w-5" /> },
  { path: '/usage', labelKey: 'nav.my_usage', icon: <ListOrdered className="h-5 w-5" /> },
];

const apiKeyMenuItems: MenuItem[] = [
  { path: '/usage', labelKey: 'nav.my_usage', icon: <ListOrdered className="h-5 w-5" />, sectionKey: 'nav.personal' },
];

function pluginPagePath(pluginName: string, pagePath: string) {
  if (pluginName === 'airgate-playground' && pagePath === '/playground') return '/chat';
  if (pluginName === 'airgate-studio' && pagePath === '/studio') return '/studio';
  return `/plugins/${pluginName}${pagePath}`;
}

function pluginMenuIcon(pluginName: string, pagePath: string): ReactNode {
  if (pluginName === 'airgate-playground' && pagePath === '/playground') return <MessageSquareMore className="h-5 w-5" />;
  if (pluginName === 'airgate-studio' && pagePath === '/studio') return <Palette className="h-5 w-5" />;
  if (pluginName === 'payment-epay' && pagePath === '/recharge') return <Wallet className="h-5 w-5" />;
  if (pluginName === 'payment-epay' && pagePath === '/orders') return <ListChecks className="h-5 w-5" />;
  return <Puzzle className="h-5 w-5" />;
}

function usePluginMenuModel(isAdmin: boolean, isAPIKeySession: boolean): PluginMenuModel {
  const loadingStartedAtRef = useRef<number | null>(null);
  const { data } = useQuery({
    queryKey: queryKeys.pluginsMenu(),
    queryFn: () => pluginsApi.menu(),
    enabled: !isAPIKeySession,
    staleTime: 60_000,
    refetchInterval: (query) => {
      if (!query.state.data?.loading) {
        loadingStartedAtRef.current = null;
        return false;
      }
      const now = Date.now();
      loadingStartedAtRef.current ??= now;
      return now - loadingStartedAtRef.current < PLUGIN_MENU_LOADING_TIMEOUT_MS
        ? PLUGIN_MENU_LOADING_POLL_MS
        : false;
    },
    meta: { globalLoading: false },
  });

  return useMemo(() => {
    if (isAPIKeySession || !data?.list) return { adminItems: [], userItems: [], healthInstalled: false };

    const healthInstalled = data.list.some((plugin) => plugin.name === 'airgate-health');
    const adminItems: MenuItem[] = [];
    const userItems: MenuItem[] = [];
    let firstAdmin = true;
    let firstUser = true;

    const sortedPlugins = [...data.list].sort((a, b) => a.name.localeCompare(b.name));

    for (const plugin of sortedPlugins) {
      if (!plugin.frontend_pages?.length) continue;
      for (const page of plugin.frontend_pages) {
        const audience = page.audience || 'admin';
        const showInUser = audience === 'user' || (audience === 'all' && !isAdmin);
        const showInAdmin = isAdmin && (audience === 'admin' || audience === 'all');
        const item: MenuItem = {
          icon: pluginMenuIcon(plugin.name, page.path),
          labelKey: page.title,
          path: pluginPagePath(plugin.name, page.path),
        };

        if (showInAdmin) {
          adminItems.push({ ...item, ...(firstAdmin ? { sectionKey: 'nav.plugins' } : {}) });
          firstAdmin = false;
        }
        if (showInUser) {
          userItems.push({ ...item, ...(firstUser ? { sectionKey: 'nav.personal' } : {}) });
          firstUser = false;
        }
      }
    }

    return { adminItems, userItems, healthInstalled };
  }, [data?.list, isAdmin, isAPIKeySession]);
}

function groupMenuSections(menuItems: MenuItem[]): MenuSection[] {
  const sections: MenuSection[] = [];
  let currentSection: MenuSection | null = null;

  for (const item of menuItems) {
    if (item.sectionKey) {
      currentSection = { titleKey: item.sectionKey, items: [item] };
      sections.push(currentSection);
    } else if (currentSection) {
      currentSection.items.push(item);
    } else {
      currentSection = { items: [item] };
      sections.push(currentSection);
    }
  }

  return sections;
}

export function useAppMenuModel({ isAdmin, isAPIKeySession }: AppMenuModelInput) {
  const {
    adminItems: pluginAdminItems,
    healthInstalled,
    userItems: pluginUserItems,
  } = usePluginMenuModel(isAdmin, isAPIKeySession);

  const sections = useMemo(() => {
    const adminUserItems = userMenuItems
      .filter((item) => item.path !== '/')
      .map((item, index) => (index === 0 ? { ...item, sectionKey: 'nav.personal' } : item));
    const pluginUserItemsMerged = pluginUserItems.map((item, index) =>
      index === 0 ? { path: item.path, labelKey: item.labelKey, icon: item.icon } : item,
    );
    const menuItems = isAPIKeySession
      ? apiKeyMenuItems
      : isAdmin
        ? [...adminMenuItems, ...pluginAdminItems, ...adminUserItems, ...pluginUserItemsMerged]
        : [...userMenuItems, ...pluginUserItemsMerged];

    return groupMenuSections(menuItems);
  }, [isAPIKeySession, isAdmin, pluginAdminItems, pluginUserItems]);

  return { healthInstalled, sections };
}
