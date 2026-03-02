import type { ComponentType } from 'react';

/**
 * 插件前端模块接口
 * 每个插件可选地暴露路由和菜单项，由核心 Shell 动态注入。
 */
export interface PluginFrontendModule {
  routes?: Array<{ path: string; component: ComponentType }>;
  menuItems?: Array<{ path: string; title: string; icon: string }>;
}

/**
 * 加载单个插件的前端模块
 * 插件前端打包后部署在 /plugins/{pluginId}/assets/index.js
 */
export async function loadPluginFrontend(
  pluginId: string,
): Promise<PluginFrontendModule | null> {
  try {
    const module = await import(
      /* @vite-ignore */ `/plugins/${pluginId}/assets/index.js`
    );
    return module.default as PluginFrontendModule;
  } catch {
    // 插件可能没有前端模块，静默忽略
    return null;
  }
}

/**
 * 批量加载所有启用插件的前端模块
 * 使用 Promise.allSettled 确保单个插件加载失败不影响其他插件
 */
export async function loadAllPluginFrontends(
  pluginIds: string[],
): Promise<Map<string, PluginFrontendModule>> {
  const results = new Map<string, PluginFrontendModule>();

  await Promise.allSettled(
    pluginIds.map(async (id) => {
      const mod = await loadPluginFrontend(id);
      if (mod) results.set(id, mod);
    }),
  );

  return results;
}
