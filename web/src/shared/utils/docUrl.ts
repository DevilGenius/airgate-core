import { isSafeURL } from './sanitizeHtml';

/**
 * effectiveDocUrl 解析"文档"按钮应该跳转到哪里。
 *
 * 仅返回管理员显式配置的安全链接。内置文档页已移除；没有链接时隐藏入口。
 *
 * 同时返回 isExternal 让调用方决定是否加 target="_blank"。
 */
export function effectiveDocUrl(docUrl: string | undefined | null): {
  href: string;
  isExternal: boolean;
} | null {
  const trimmed = (docUrl ?? '').trim();
  if (/^\/docs(?:[/?#]|$)/.test(trimmed)) return null;
  if (trimmed && isSafeURL(trimmed)) {
    return { href: trimmed, isExternal: !trimmed.startsWith('/') };
  }
  return null;
}
