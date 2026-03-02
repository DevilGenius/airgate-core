import { type ReactNode, useState } from 'react';
import { Link } from '@tanstack/react-router';
import { useAuth } from '../providers/AuthProvider';

interface AppShellProps {
  children: ReactNode;
}

// 侧边栏菜单定义
const adminMenuItems = [
  { path: '/', label: '仪表盘', icon: '📊' },
  { path: '/admin/users', label: '用户管理', icon: '👥' },
  { path: '/admin/accounts', label: '账号管理', icon: '🔑' },
  { path: '/admin/groups', label: '分组管理', icon: '📁' },
  { path: '/admin/api-keys', label: 'API 密钥', icon: '🗝️' },
  { path: '/admin/subscriptions', label: '订阅管理', icon: '📋' },
  { path: '/admin/proxies', label: '代理池', icon: '🌐' },
  { path: '/admin/usage', label: '使用记录', icon: '📈' },
  { path: '/admin/plugins', label: '插件管理', icon: '🧩' },
  { path: '/admin/settings', label: '系统设置', icon: '⚙️' },
];

const userMenuItems = [
  { path: '/profile', label: '个人资料', icon: '👤' },
  { path: '/keys', label: '我的密钥', icon: '🔐' },
];

export function AppShell({ children }: AppShellProps) {
  const { user, logout } = useAuth();
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);

  const isAdmin = user?.role === 'admin';
  const menuItems = isAdmin ? [...adminMenuItems, ...userMenuItems] : userMenuItems;

  return (
    <div className="flex h-screen">
      {/* 侧边栏 */}
      <aside
        className="bg-white border-r border-gray-200 flex flex-col transition-all"
        style={{ width: sidebarCollapsed ? 64 : 240 }}
      >
        {/* Logo */}
        <div className="h-14 flex items-center px-4 border-b border-gray-200">
          {!sidebarCollapsed && <span className="font-bold text-lg">AirGate</span>}
          <button
            onClick={() => setSidebarCollapsed(!sidebarCollapsed)}
            className="ml-auto text-gray-400 hover:text-gray-600"
          >
            {sidebarCollapsed ? '→' : '←'}
          </button>
        </div>

        {/* 菜单 */}
        <nav className="flex-1 overflow-y-auto py-2">
          {menuItems.map((item) => (
            <Link
              key={item.path}
              to={item.path}
              className="flex items-center px-4 py-2 mx-2 rounded-md text-sm text-gray-700 hover:bg-gray-100"
              activeProps={{ className: 'bg-indigo-50 text-indigo-700 font-medium' }}
            >
              <span className="text-base">{item.icon}</span>
              {!sidebarCollapsed && <span className="ml-3">{item.label}</span>}
            </Link>
          ))}
        </nav>

        {/* 底部用户信息 */}
        <div className="border-t border-gray-200 p-4">
          {!sidebarCollapsed && (
            <div className="text-sm">
              <p className="font-medium truncate">{user?.email}</p>
              <button
                onClick={logout}
                className="text-gray-500 hover:text-red-500 mt-1 text-xs"
              >
                退出登录
              </button>
            </div>
          )}
        </div>
      </aside>

      {/* 主内容区 */}
      <main className="flex-1 overflow-auto">
        {children}
      </main>
    </div>
  );
}
