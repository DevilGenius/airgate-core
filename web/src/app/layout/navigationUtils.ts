export function normalizePath(path: string) {
  const [pathname = '/'] = path.split(/[?#]/, 1);
  if (!pathname || pathname === '/') return '/';
  return pathname.replace(/\/+$/, '');
}

export function isMenuItemActive(itemPath: string, currentPath: string) {
  const item = normalizePath(itemPath);
  const current = normalizePath(currentPath);
  if (item === '/') return current === '/';
  return current === item || current.startsWith(`${item}/`);
}

export function scheduleAfterPaint(work: () => void, delayMs = 0) {
  if (typeof window === 'undefined') {
    work();
    return () => {};
  }

  let timerId: number | null = null;
  const frameId = window.requestAnimationFrame(() => {
    timerId = window.setTimeout(work, delayMs);
  });

  return () => {
    window.cancelAnimationFrame(frameId);
    if (timerId != null) window.clearTimeout(timerId);
  };
}
