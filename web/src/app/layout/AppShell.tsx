import { type ReactNode, useState, useRef, useEffect } from 'react';
import { Link, useMatchRoute, useLocation } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '../providers/AuthProvider';
import { pluginsApi } from '../../shared/api/plugins';
import { loadPluginFrontend, registerPlatformIcon } from '../plugin-loader';
import { useTheme } from '../providers/ThemeProvider';
import {
  LayoutDashboard,
  Users,
  KeyRound,
  FolderTree,
  Key,
  CreditCard,
  Globe,
  BarChart3,
  Puzzle,
  Settings,
  User,
  LogOut,
  PanelLeftClose,
  PanelLeft,
  Zap,
  Languages,
  Sun,
  Moon,
  ChevronDown,
  Github,
} from 'lucide-react';

interface AppShellProps {
  children: ReactNode;
}

interface MenuItem {
  path: string;
  labelKey: string;
  descriptionKey?: string;
  icon: ReactNode;
  sectionKey?: string;
}

const adminMenuItems: MenuItem[] = [
  { path: '/', labelKey: 'nav.dashboard', descriptionKey: 'dashboard.description', icon: <LayoutDashboard className="w-5 h-5" /> },
  { path: '/admin/users', labelKey: 'nav.users', descriptionKey: 'users.description', icon: <Users className="w-5 h-5" /> },
  { path: '/admin/groups', labelKey: 'nav.groups', descriptionKey: 'groups.description', icon: <FolderTree className="w-5 h-5" /> },
  { path: '/admin/subscriptions', labelKey: 'nav.subscriptions', descriptionKey: 'subscriptions.description', icon: <CreditCard className="w-5 h-5" /> },
  { path: '/admin/accounts', labelKey: 'nav.accounts', descriptionKey: 'accounts.description', icon: <KeyRound className="w-5 h-5" /> },
  { path: '/admin/proxies', labelKey: 'nav.proxies', descriptionKey: 'proxies.description', icon: <Globe className="w-5 h-5" /> },
  { path: '/admin/usage', labelKey: 'nav.usage', descriptionKey: 'usage.description', icon: <BarChart3 className="w-5 h-5" /> },
  { path: '/admin/plugins', labelKey: 'nav.plugins', descriptionKey: 'plugins.description', icon: <Puzzle className="w-5 h-5" /> },
  { path: '/admin/settings', labelKey: 'nav.settings', descriptionKey: 'settings.description', icon: <Settings className="w-5 h-5" /> },
];

const userMenuItems: MenuItem[] = [
  { path: '/keys', labelKey: 'nav.my_keys', descriptionKey: 'user_keys.description', icon: <Key className="w-5 h-5" />, sectionKey: 'nav.personal' },
  { path: '/usage', labelKey: 'nav.usage', descriptionKey: 'usage.description', icon: <BarChart3 className="w-5 h-5" /> },
  { path: '/profile', labelKey: 'nav.profile', descriptionKey: 'profile.description', icon: <User className="w-5 h-5" /> },
];

/** 已预加载过图标的插件集合，避免重复加载 */
const preloadedIcons = new Set<string>();

function usePluginMenuItems(): MenuItem[] {
  const { data } = useQuery({
    queryKey: ['plugins-menu'],
    queryFn: () => pluginsApi.list(),
    staleTime: 60_000,
  });

  // 插件列表拿到后，预加载所有平台图标
  useEffect(() => {
    if (!data?.list) return;
    for (const p of data.list) {
      if (!p.platform || preloadedIcons.has(p.name)) continue;
      preloadedIcons.add(p.name);
      loadPluginFrontend(p.name).then((mod) => {
        if (mod?.platformIcon) registerPlatformIcon(p.platform, mod.platformIcon);
      });
    }
  }, [data?.list]);

  if (!data?.list) return [];

  const items: MenuItem[] = [];
  let first = true;
  for (const p of data.list) {
    if (!p.frontend_pages?.length) continue;
    for (const page of p.frontend_pages) {
      items.push({
        path: `/plugins/${p.name}${page.path}`,
        labelKey: page.title,
        icon: <Puzzle className="w-5 h-5" />,
        ...(first ? { sectionKey: 'nav.plugins' } : {}),
      });
      first = false;
    }
  }
  return items;
}

export function AppShell({ children }: AppShellProps) {
  const { user, logout } = useAuth();
  const { t, i18n } = useTranslation();
  const { theme, toggleTheme } = useTheme();
  const [collapsed, setCollapsed] = useState(false);
  const [dropdownOpen, setDropdownOpen] = useState(false);
  const dropdownRef = useRef<HTMLDivElement>(null);
  const matchRoute = useMatchRoute();

  // 点击外部关闭下拉菜单
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (dropdownRef.current && !dropdownRef.current.contains(e.target as Node)) {
        setDropdownOpen(false);
      }
    }
    document.addEventListener('mousedown', handleClickOutside);
    return () => document.removeEventListener('mousedown', handleClickOutside);
  }, []);

  const location = useLocation();
  const isAdmin = user?.role === 'admin';
  const pluginMenuItems = usePluginMenuItems();
  const menuItems = isAdmin
    ? [...adminMenuItems, ...pluginMenuItems, ...userMenuItems]
    : [...userMenuItems, ...pluginMenuItems];

  // 根据当前路由匹配页面标题
  const currentMenuItem = menuItems.find((item) => {
    if (item.path === '/') return location.pathname === '/';
    return location.pathname.startsWith(item.path);
  });
  const pageTitle = currentMenuItem ? t(currentMenuItem.labelKey, { defaultValue: currentMenuItem.labelKey }) : '';
  const pageDescription = currentMenuItem?.descriptionKey ? t(currentMenuItem.descriptionKey, { defaultValue: '' }) : '';

  const sections: Array<{ titleKey?: string; items: MenuItem[] }> = [];
  let currentSection: { titleKey?: string; items: MenuItem[] } | null = null;

  menuItems.forEach((item) => {
    if (item.sectionKey) {
      currentSection = { titleKey: item.sectionKey, items: [item] };
      sections.push(currentSection);
    } else if (currentSection) {
      currentSection.items.push(item);
    } else {
      currentSection = { items: [item] };
      sections.push(currentSection);
    }
  });

  const toggleLanguage = () => {
    const nextLang = i18n.language === 'zh' ? 'en' : 'zh';
    i18n.changeLanguage(nextLang);
    localStorage.setItem('lang', nextLang);
  };

  return (
    <div className="flex h-screen bg-bg-deep">
      {/* 侧边栏 */}
      <aside
        className="relative flex flex-col border-r border-border bg-bg transition-all duration-300 ease-in-out"
        style={{ width: collapsed ? 'var(--ag-sidebar-collapsed)' : 'var(--ag-sidebar-width)' }}
      >
        {/* 右侧发光边线 */}
        <div
          className="absolute right-0 top-0 bottom-0 w-px pointer-events-none"
          style={{ background: 'linear-gradient(180deg, var(--ag-primary-glow), transparent 40%, transparent 60%, var(--ag-primary-glow))' }}
        />

        {/* Logo 区 */}
        <div className="flex items-center h-16 px-4 border-b border-border">
          <div className="flex items-center gap-3 overflow-hidden">
            <div className="flex items-center justify-center w-9 h-9 rounded-xl bg-primary-subtle flex-shrink-0">
              <Zap className="w-[18px] h-[18px] text-primary" />
            </div>
            {!collapsed && (
              <div className="overflow-hidden">
                <h1 className="text-[15px] font-semibold text-text tracking-tight whitespace-nowrap">
                  AirGate
                </h1>
                <p className="text-[10px] text-text-tertiary font-mono tracking-[0.08em]">
                  v0.1.0
                </p>
              </div>
            )}
          </div>
        </div>

        {/* 导航菜单 */}
        <nav className="flex-1 overflow-y-auto py-4 px-3 space-y-6">
          {sections.map((section, si) => (
            <div key={si}>
              {section.titleKey && !collapsed && (
                <p className="text-[10px] font-medium text-text-tertiary uppercase tracking-[0.1em] px-3 mb-2">
                  {t(section.titleKey)}
                </p>
              )}
              {collapsed && si > 0 && (
                <div className="h-px mx-3 mb-3 bg-border" />
              )}
              <div className="space-y-1">
                {section.items.map((item) => {
                  const isActive = !!matchRoute({ to: item.path, fuzzy: item.path !== '/' });
                  const isExactDashboard = item.path === '/' && !!matchRoute({ to: '/' });
                  const active = item.path === '/' ? isExactDashboard : isActive;

                  return (
                    <Link
                      key={item.path}
                      to={item.path}
                      className={`group flex items-center gap-3 rounded-lg transition-all duration-200 relative ${
                        collapsed ? 'justify-center px-0 py-2.5 mx-1' : 'px-3 py-2.5'
                      } ${
                        active
                          ? 'bg-primary-subtle text-primary'
                          : 'text-text-tertiary hover:text-text-secondary hover:bg-bg-hover'
                      }`}
                    >
                      {active && (
                        <div className="absolute left-0 top-1/2 -translate-y-1/2 w-[3px] h-4 rounded-r-full bg-primary" />
                      )}
                      <span className="flex-shrink-0">{item.icon}</span>
                      {!collapsed && (
                        <span className="text-sm font-medium truncate">{t(item.labelKey, { defaultValue: item.labelKey })}</span>
                      )}
                      {collapsed && (
                        <div className="absolute left-full ml-2 px-3 py-1.5 rounded-lg bg-bg-elevated border border-glass-border shadow-md text-xs text-text whitespace-nowrap opacity-0 invisible group-hover:opacity-100 group-hover:visible transition-all z-50">
                          {t(item.labelKey, { defaultValue: item.labelKey })}
                        </div>
                      )}
                    </Link>
                  );
                })}
              </div>
            </div>
          ))}
        </nav>

        {/* 底部折叠按钮 */}
        <div className="border-t border-border p-3">
          <button
            onClick={() => setCollapsed(!collapsed)}
            className="flex items-center justify-center w-full h-8 rounded-lg text-text-tertiary hover:text-text-secondary hover:bg-bg-hover transition-colors"
          >
            {collapsed ? (
              <PanelLeft className="w-4 h-4" />
            ) : (
              <PanelLeftClose className="w-4 h-4" />
            )}
          </button>
        </div>
      </aside>

      {/* 右侧区域 */}
      <div className="flex-1 flex flex-col overflow-hidden">
        {/* 顶栏 */}
        <header className="flex items-center justify-between h-16 px-6 border-b border-border bg-bg/80 backdrop-blur-xl flex-shrink-0">
          {/* 页面标题 */}
          <div className="min-w-0">
            <h2 className="text-base font-semibold text-text tracking-tight">{pageTitle}</h2>
            {pageDescription && (
              <p className="text-xs text-text-tertiary mt-0.5 truncate">{pageDescription}</p>
            )}
          </div>
          <div className="flex items-center gap-1.5">
            {/* 语言切换 */}
            <button
              onClick={toggleLanguage}
              className="flex items-center justify-center h-9 px-3 rounded-xl text-text-tertiary hover:text-text-secondary hover:bg-bg-hover transition-colors gap-2"
              title={i18n.language === 'zh' ? 'Switch to English' : '切换为中文'}
            >
              <Languages className="w-4 h-4" />
              <span className="text-xs font-medium">{i18n.language === 'zh' ? 'EN' : '中文'}</span>
            </button>

            {/* 主题切换 */}
            <button
              onClick={toggleTheme}
              className="flex items-center justify-center w-9 h-9 rounded-xl text-text-tertiary hover:text-text-secondary hover:bg-bg-hover transition-colors"
              title={theme === 'dark' ? '切换亮色模式' : '切换暗色模式'}
            >
              {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
            </button>

            {/* GitHub */}
            <a
              href="https://github.com/DouDOU-start/airgate-core"
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center justify-center w-9 h-9 rounded-xl text-text-tertiary hover:text-text-secondary hover:bg-bg-hover transition-colors"
              title="GitHub"
            >
              <Github className="w-4 h-4" />
            </a>

            {/* 分隔线 */}
            <div className="w-px h-6 bg-border mx-2" />

            {/* 用户下拉菜单 */}
            <div className="relative" ref={dropdownRef}>
              <button
                onClick={() => setDropdownOpen(!dropdownOpen)}
                className="flex items-center gap-2.5 rounded-xl p-1.5 transition-colors hover:bg-bg-hover"
              >
                <div className="flex items-center justify-center w-8 h-8 rounded-xl bg-primary-subtle flex-shrink-0 text-xs font-bold text-primary">
                  {user?.email?.charAt(0).toUpperCase() || 'U'}
                </div>
                <div className="text-left hidden md:block">
                  <div className="text-sm font-medium text-text leading-tight">
                    {user?.username || user?.email}
                  </div>
                  <div className="text-[11px] text-text-tertiary leading-tight capitalize">
                    {user?.role === 'admin' ? t('nav.admin') : t('nav.user')}
                  </div>
                </div>
                <ChevronDown className={`w-4 h-4 text-text-tertiary transition-transform duration-200 hidden md:block ${dropdownOpen ? 'rotate-180' : ''}`} />
              </button>

              {/* 下拉菜单 */}
              {dropdownOpen && (
                <div className="absolute right-0 mt-2 w-56 rounded-xl bg-bg-elevated border border-glass-border shadow-lg z-50 overflow-hidden animate-[ag-slide-down_0.15s_ease-out]">
                  {/* 用户信息 */}
                  <div className="px-4 py-3 border-b border-border">
                    <div className="text-sm font-medium text-text">{user?.username || user?.email}</div>
                    <div className="text-xs text-text-tertiary mt-0.5">{user?.email}</div>
                  </div>
                  {/* 退出登录 */}
                  <div className="p-1.5">
                    <button
                      onClick={() => { setDropdownOpen(false); logout(); }}
                      className="flex items-center gap-2.5 w-full px-3 py-2 rounded-lg text-sm text-danger hover:bg-danger-subtle transition-colors"
                    >
                      <LogOut className="w-4 h-4" />
                      {t('common.logout')}
                    </button>
                  </div>
                </div>
              )}
            </div>
          </div>
        </header>

        {/* 主内容 */}
        <main className="flex-1 overflow-auto">
          <div className="p-6 max-w-[1400px] mx-auto">
            {children}
          </div>
        </main>
      </div>
    </div>
  );
}
