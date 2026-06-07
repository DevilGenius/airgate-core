export const FRONTEND_CACHE_VERSION = 'ui-2026-06-06';
export const FRONTEND_CACHE_VERSION_KEY = 'ag:web:cache:version';

export const STORAGE_KEYS = {
  auth: {
    apiKeySecret: 'ag:web:auth:api_key_secret',
    token: 'ag:web:auth:token',
    tokenMode: 'ag:web:auth:token_mode',
  },
  i18n: {
    language: 'ag:web:i18n:language',
  },
  layout: {
    sidebarCollapsed: 'ag:web:layout:sidebar_collapsed',
  },
  pagination: {
    pageSizePrefix: 'ag:web:pagination:page_size:',
  },
  setup: {
    complete: 'ag:web:setup:complete',
  },
  settings: {
    publicSite: 'ag:web:settings:public_site',
  },
  ui: {
    adminAccountsAutoRefresh: 'ag:web:admin:accounts:auto_refresh',
    adminAccountsCapacityRefresh: 'ag:web:admin:accounts:capacity_refresh',
    adminMonitorAutoRefresh: 'ag:web:admin:monitor:auto_refresh',
    adminUsageAutoRefresh: 'ag:web:admin:usage:auto_refresh',
    adminUsageColumns: 'ag:web:admin:usage:columns',
    tableStatePrefix: 'ag:web:table:',
    userUsageAutoRefresh: 'ag:web:user:usage:auto_refresh',
  },
} as const;

export const PRESERVED_LOCAL_STORAGE_KEYS = new Set<string>([
  FRONTEND_CACHE_VERSION_KEY,
  STORAGE_KEYS.auth.token,
  STORAGE_KEYS.auth.tokenMode,
  STORAGE_KEYS.i18n.language,
  STORAGE_KEYS.setup.complete,
  STORAGE_KEYS.settings.publicSite,
]);

export function storagePageSizeKey(scope: string) {
  return `${STORAGE_KEYS.pagination.pageSizePrefix}${scope.replace(/[^a-zA-Z0-9:_-]/g, ':')}`;
}

export function storageTableStateKey(scope: string) {
  return `${STORAGE_KEYS.ui.tableStatePrefix}${scope.replace(/[^a-zA-Z0-9:_-]/g, ':')}`;
}
