import {
  createRouter,
  createRootRoute,
  createRoute,
  Outlet,
  redirect,
} from '@tanstack/react-router';
import { lazy } from 'react';
import { AppShell } from './layout/AppShell';
import { getToken } from '../shared/api/client';

// 懒加载页面组件
const SetupPage = lazy(() => import('../pages/SetupPage'));
const LoginPage = lazy(() => import('../pages/LoginPage'));
const DashboardPage = lazy(() => import('../pages/DashboardPage'));
const UsersPage = lazy(() => import('../pages/admin/UsersPage'));
const AccountsPage = lazy(() => import('../pages/admin/AccountsPage'));
const GroupsPage = lazy(() => import('../pages/admin/GroupsPage'));
const APIKeysPage = lazy(() => import('../pages/admin/APIKeysPage'));
const SubscriptionsPage = lazy(() => import('../pages/admin/SubscriptionsPage'));
const ProxiesPage = lazy(() => import('../pages/admin/ProxiesPage'));
const UsagePage = lazy(() => import('../pages/admin/UsagePage'));
const PluginsPage = lazy(() => import('../pages/admin/PluginsPage'));
const SettingsPage = lazy(() => import('../pages/admin/SettingsPage'));
const ProfilePage = lazy(() => import('../pages/user/ProfilePage'));
const UserKeysPage = lazy(() => import('../pages/user/UserKeysPage'));

// 根路由
const rootRoute = createRootRoute({
  component: () => <Outlet />,
});

// 安装向导（无需认证）
const setupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/setup',
  component: SetupPage,
});

// 登录页（无需认证）
const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/login',
  component: LoginPage,
});

// 认证布局（需要登录）
const authLayout = createRoute({
  getParentRoute: () => rootRoute,
  id: 'auth',
  beforeLoad: () => {
    if (!getToken()) {
      throw redirect({ to: '/login' });
    }
  },
  component: () => (
    <AppShell>
      <Outlet />
    </AppShell>
  ),
});

// 仪表盘
const dashboardRoute = createRoute({ getParentRoute: () => authLayout, path: '/', component: DashboardPage });

// 管理员路由
const adminUsersRoute = createRoute({ getParentRoute: () => authLayout, path: '/admin/users', component: UsersPage });
const adminAccountsRoute = createRoute({ getParentRoute: () => authLayout, path: '/admin/accounts', component: AccountsPage });
const adminGroupsRoute = createRoute({ getParentRoute: () => authLayout, path: '/admin/groups', component: GroupsPage });
const adminAPIKeysRoute = createRoute({ getParentRoute: () => authLayout, path: '/admin/api-keys', component: APIKeysPage });
const adminSubscriptionsRoute = createRoute({ getParentRoute: () => authLayout, path: '/admin/subscriptions', component: SubscriptionsPage });
const adminProxiesRoute = createRoute({ getParentRoute: () => authLayout, path: '/admin/proxies', component: ProxiesPage });
const adminUsageRoute = createRoute({ getParentRoute: () => authLayout, path: '/admin/usage', component: UsagePage });
const adminPluginsRoute = createRoute({ getParentRoute: () => authLayout, path: '/admin/plugins', component: PluginsPage });
const adminSettingsRoute = createRoute({ getParentRoute: () => authLayout, path: '/admin/settings', component: SettingsPage });

// 用户路由
const profileRoute = createRoute({ getParentRoute: () => authLayout, path: '/profile', component: ProfilePage });
const userKeysRoute = createRoute({ getParentRoute: () => authLayout, path: '/keys', component: UserKeysPage });

// 路由树
const routeTree = rootRoute.addChildren([
  setupRoute,
  loginRoute,
  authLayout.addChildren([
    dashboardRoute,
    adminUsersRoute,
    adminAccountsRoute,
    adminGroupsRoute,
    adminAPIKeysRoute,
    adminSubscriptionsRoute,
    adminProxiesRoute,
    adminUsageRoute,
    adminPluginsRoute,
    adminSettingsRoute,
    profileRoute,
    userKeysRoute,
  ]),
]);

export const router = createRouter({ routeTree });
