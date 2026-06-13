const DYNAMIC_IMPORT_RELOAD_KEY = 'ag:web:dynamic_import_reload_at';
const DYNAMIC_IMPORT_RELOAD_COOLDOWN_MS = 30_000;

let installed = false;
let reloadScheduled = false;

const DYNAMIC_IMPORT_ERROR_PATTERNS = [
  /Failed to fetch dynamically imported module/i,
  /Importing a module script failed/i,
  /error loading dynamically imported module/i,
  /Failed to load module script/i,
  /Loading chunk [\w-]+ failed/i,
  /ChunkLoadError/i,
  /Unable to preload CSS/i,
];

function errorText(error: unknown): string {
  if (error instanceof Error) {
    return [error.name, error.message, error.stack].filter(Boolean).join('\n');
  }
  if (typeof error === 'string') return error;
  if (error && typeof error === 'object') {
    const record = error as { message?: unknown; name?: unknown; stack?: unknown };
    return [record.name, record.message, record.stack]
      .filter((value): value is string => typeof value === 'string' && value.length > 0)
      .join('\n');
  }
  return String(error);
}

export function isDynamicImportLoadError(error: unknown): boolean {
  const text = errorText(error);
  return DYNAMIC_IMPORT_ERROR_PATTERNS.some((pattern) => pattern.test(text));
}

function canScheduleReload(): boolean {
  if (reloadScheduled || typeof window === 'undefined') return false;

  const now = Date.now();
  try {
    const previous = Number(window.sessionStorage.getItem(DYNAMIC_IMPORT_RELOAD_KEY));
    if (Number.isFinite(previous) && now - previous < DYNAMIC_IMPORT_RELOAD_COOLDOWN_MS) {
      return false;
    }
    window.sessionStorage.setItem(DYNAMIC_IMPORT_RELOAD_KEY, String(now));
  } catch {
    // sessionStorage may be blocked; keep the in-memory guard for this runtime.
  }

  reloadScheduled = true;
  return true;
}

export function recoverFromDynamicImportError(error: unknown, source: string): boolean {
  if (!isDynamicImportLoadError(error) || !canScheduleReload()) return false;

  console.warn(`[dynamic-import-recovery] Reloading after ${source} failed`, error);
  window.setTimeout(() => {
    window.location.reload();
  }, 0);
  return true;
}

export function installDynamicImportRecovery() {
  if (installed || typeof window === 'undefined') return;
  installed = true;

  window.addEventListener('vite:preloadError', ((event: Event) => {
    const preloadEvent = event as Event & { payload?: unknown };
    if (recoverFromDynamicImportError(preloadEvent.payload ?? event, 'vite:preloadError')) {
      event.preventDefault();
    }
  }) as EventListener);

  window.addEventListener('unhandledrejection', (event) => {
    if (recoverFromDynamicImportError(event.reason, 'unhandledrejection')) {
      event.preventDefault();
    }
  });

  window.addEventListener('error', (event) => {
    if (recoverFromDynamicImportError(event.error ?? event.message, 'window.error')) {
      event.preventDefault();
    }
  });
}
