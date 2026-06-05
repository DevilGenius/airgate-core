import {
  FRONTEND_CACHE_VERSION,
  FRONTEND_CACHE_VERSION_KEY,
  PRESERVED_LOCAL_STORAGE_KEYS,
} from '../storageKeys';

export function initializeFrontendCache() {
  if (typeof window === 'undefined') return;

  try {
    const currentVersion = window.localStorage.getItem(FRONTEND_CACHE_VERSION_KEY);
    if (currentVersion === FRONTEND_CACHE_VERSION) return;

    const keysToRemove: string[] = [];
    for (let index = 0; index < window.localStorage.length; index += 1) {
      const key = window.localStorage.key(index);
      if (key && !PRESERVED_LOCAL_STORAGE_KEYS.has(key)) {
        keysToRemove.push(key);
      }
    }

    keysToRemove.forEach((key) => window.localStorage.removeItem(key));
    window.localStorage.setItem(FRONTEND_CACHE_VERSION_KEY, FRONTEND_CACHE_VERSION);
  } catch {
    // Storage may be unavailable. The app should still boot.
  }
}
