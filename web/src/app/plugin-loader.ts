import { createElement, type ComponentType } from 'react';
import type {
  AccountSurfaceProps,
  PluginFrontendModule,
  PluginPlatformIconProps,
} from '@doudou-start/airgate-theme/plugin';

function wrapPluginComponent<TProps extends object>(
  Component: ComponentType<TProps>,
): ComponentType<TProps> {
  return function WrappedPluginComponent(props) {
    return createElement(Component, (props ?? {}) as TProps);
  };
}

function normalizePluginFrontendModule(
  mod: PluginFrontendModule | null,
): PluginFrontendModule | null {
  if (!mod) return null;

  return {
    ...mod,
    accountCreate: mod.accountCreate
      ? wrapPluginComponent(mod.accountCreate)
      : undefined,
    accountEdit: mod.accountEdit
      ? wrapPluginComponent(mod.accountEdit)
      : undefined,
    platformIcon: mod.platformIcon
      ? wrapPluginComponent(mod.platformIcon)
      : undefined,
    accountUsageWindow: mod.accountUsageWindow
      ? wrapPluginComponent(mod.accountUsageWindow)
      : undefined,
    routes: mod.routes?.map((route) => ({
      ...route,
      component: wrapPluginComponent(route.component),
    })),
  };
}

// 核心通过 window.__airgate_shared 暴露的共享模块列表
const SHARED_MODULES = ['react', 'react-dom', 'react/jsx-runtime', 'react-i18next'];
const pluginFrontendCache = new Map<string, Promise<PluginFrontendModule | null>>();
const pluginFrontendCacheListeners = new Set<(pluginId?: string) => void>();

function rewriteNamedImportSpecifiers(specifiers: string): string {
  return specifiers
    .split(',')
    .map((part) => part.trim())
    .filter(Boolean)
    .map((part) => {
      const aliasParts = part.split(/\s+as\s+/);
      const imported = aliasParts[0]?.trim();
      const local = aliasParts[1]?.trim();
      if (imported && local) {
        return `${imported}: ${local}`;
      }
      return part;
    })
    .join(', ');
}

/**
 * 将插件 ESM 代码中的裸 import 重写为从 window.__airgate_shared 取值
 * 例：import { jsx } from "react/jsx-runtime"
 * →  const { jsx } = window.__airgate_shared["react/jsx-runtime"]
 */
function rewriteBareImports(code: string): string {
  for (const mod of SHARED_MODULES) {
    // 匹配 import { ... } from "react/jsx-runtime" 和 import { ... } from 'react'
    const pattern = new RegExp(
      `import\\s*\\{([^}]+)\\}\\s*from\\s*["']${mod.replace('/', '\\/')}["'];?`,
      'g',
    );
    code = code.replace(pattern, (_match, imports: string) => {
      return `const { ${rewriteNamedImportSpecifiers(imports)} } = window.__airgate_shared["${mod}"];`;
    });
    // 匹配 import * as X from "react"
    const starPattern = new RegExp(
      `import\\s*\\*\\s*as\\s+(\\w+)\\s+from\\s*["']${mod.replace('/', '\\/')}["'];?`,
      'g',
    );
    code = code.replace(starPattern, (_match, name: string) => {
      return `const ${name} = window.__airgate_shared["${mod}"];`;
    });
    // 匹配 import Default, { named } from "react" (混合导入)
    const mixedPattern = new RegExp(
      `import\\s+([\\w$]+)\\s*,\\s*\\{([^}]+)\\}\\s*from\\s*["']${mod.replace('/', '\\/')}["'];?`,
      'g',
    );
    code = code.replace(mixedPattern, (_match, defaultName: string, imports: string) => {
      return `const ${defaultName} = window.__airgate_shared["${mod}"]; const { ${rewriteNamedImportSpecifiers(imports)} } = window.__airgate_shared["${mod}"];`;
    });
    // 匹配 import React from "react"
    const defaultPattern = new RegExp(
      `import\\s+([\\w$]+)\\s+from\\s*["']${mod.replace('/', '\\/')}["'];?`,
      'g',
    );
    code = code.replace(defaultPattern, (_match, name: string) => {
      return `const ${name} = window.__airgate_shared["${mod}"];`;
    });
    // 匹配 import "react-dom" (纯副作用导入，无变量绑定)
    const sideEffectPattern = new RegExp(
      `import\\s*["']${mod.replace('/', '\\/')}["'];?`,
      'g',
    );
    code = code.replace(sideEffectPattern, () => {
      return `/* side-effect import: ${mod} */`;
    });
  }
  return code;
}

/**
 * 加载单个插件的前端模块
 * 插件前端打包后部署在 /plugins/{pluginId}/assets/index.js
 *
 * 由于插件构建时将 react 等声明为 external，产物包含裸 import（浏览器无法解析）。
 * 这里通过 fetch → 重写 import → Blob URL → dynamic import 来解决。
 */
async function fetchPluginFrontend(
  pluginId: string,
): Promise<PluginFrontendModule | null> {
  try {
    const url = `/plugins/${pluginId}/assets/index.js`;
    const resp = await fetch(url);
    if (!resp.ok) return null;

    // Load plugin CSS if available
    const cssUrl = `/plugins/${pluginId}/assets/index.css`;
    fetch(cssUrl).then((cssResp) => {
      if (!cssResp.ok) return;
      return cssResp.text().then((css) => {
        const existingStyle = document.getElementById(`plugin-css-${pluginId}`);
        if (existingStyle) return;
        const style = document.createElement('style');
        style.id = `plugin-css-${pluginId}`;
        style.textContent = css;
        document.head.appendChild(style);
      });
    }).catch(() => {});

    let code = await resp.text();
    code = rewriteBareImports(code);

    const blob = new Blob([code], { type: 'application/javascript' });
    const blobUrl = URL.createObjectURL(blob);
    try {
      // 记录 import 前已有的 <style> id，用于检测插件注入的主题变量样式
      const styleIdsBefore = new Set(
        Array.from(document.querySelectorAll('style[id]'), (el) => el.id),
      );

      const module = await import(/* @vite-ignore */ blobUrl);

      // 移除插件注入的主题变量样式（id 含 "theme-vars"）
      // 插件会打包旧版 SDK token 并通过 injectThemeStyle 注入作用域变量，
      // 这些变量优先级高于全局 :root 声明，导致配色不同步。
      // 移除后插件元素自然继承核心注入的最新主题变量。
      document.querySelectorAll('style[id]').forEach((el) => {
        if (!styleIdsBefore.has(el.id) && el.id.includes('theme-vars')) {
          el.remove();
        }
      });

      return normalizePluginFrontendModule(module.default as PluginFrontendModule);
    } finally {
      URL.revokeObjectURL(blobUrl);
    }
  } catch (err) {
    // 插件可能没有前端模块，记录错误便于排查
    console.warn(`[plugin-loader] Failed to load plugin frontend: ${pluginId}`, err);
    return null;
  }
}

export function loadPluginFrontend(
  pluginId: string,
): Promise<PluginFrontendModule | null> {
  const cached = pluginFrontendCache.get(pluginId);
  if (cached) return cached;

  const promise = fetchPluginFrontend(pluginId).then((mod) => {
    if (!mod) pluginFrontendCache.delete(pluginId);
    return mod;
  });
  pluginFrontendCache.set(pluginId, promise);
  return promise;
}

export function clearPluginFrontendCache(pluginId?: string) {
  if (pluginId) {
    pluginFrontendCache.delete(pluginId);
  } else {
    pluginFrontendCache.clear();
  }
  pluginFrontendCacheListeners.forEach((listener) => listener(pluginId));
}

export function onPluginFrontendCacheClear(listener: (pluginId?: string) => void) {
  pluginFrontendCacheListeners.add(listener);
  return () => {
    pluginFrontendCacheListeners.delete(listener);
  };
}

/** 全局平台图标注册表：platform → Icon 组件 */
const platformIconRegistry = new Map<string, ComponentType<PluginPlatformIconProps>>();

/** 图标注册变更监听器 */
const iconListeners = new Set<() => void>();

/** 注册插件提供的平台图标 */
export function registerPlatformIcon(platform: string, icon: ComponentType<PluginPlatformIconProps>) {
  platformIconRegistry.set(platform.toLowerCase(), icon);
  // 通知所有监听者图标已更新
  iconListeners.forEach((fn) => fn());
}

/** 获取插件提供的平台图标 */
export function getPluginPlatformIcon(platform: string): ComponentType<PluginPlatformIconProps> | undefined {
  return platformIconRegistry.get(platform.toLowerCase());
}

/** 订阅图标注册变更，返回取消订阅函数 */
export function onPlatformIconChange(listener: () => void): () => void {
  iconListeners.add(listener);
  return () => iconListeners.delete(listener);
}

/** 全局用量窗口渲染器注册表：platform → UsageWindow 组件 */
const usageWindowRegistry = new Map<string, ComponentType<AccountSurfaceProps>>();
const usageWindowListeners = new Set<() => void>();
let usageWindowVersion = 0;

export function registerUsageWindow(platform: string, component: ComponentType<AccountSurfaceProps>) {
  usageWindowRegistry.set(platform.toLowerCase(), component);
  usageWindowVersion++;
  usageWindowListeners.forEach((fn) => fn());
}

export function getPluginUsageWindow(
  platform: string,
): ComponentType<AccountSurfaceProps> | undefined {
  return usageWindowRegistry.get(platform.toLowerCase());
}

export function subscribeUsageWindowChange(listener: () => void): () => void {
  usageWindowListeners.add(listener);
  return () => usageWindowListeners.delete(listener);
}

export function getUsageWindowVersion(): number {
  return usageWindowVersion;
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
