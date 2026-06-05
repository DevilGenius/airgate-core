import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { settingsApi } from '../../shared/api/settings';
import defaultLogoUrl from '../../assets/logo.svg';
import { STORAGE_KEYS } from '../../shared/storageKeys';

export { defaultLogoUrl };

interface SiteSettings {
  site_name: string;
  site_subtitle: string;
  site_logo: string;
  api_base_url: string;
  frontend_url: string;
  contact_info: string;
  doc_url: string;
  home_content: string;
  registration_enabled: boolean;
  email_verify_enabled: boolean;
  settings_loaded: boolean;
}

const defaults: SiteSettings = {
  site_name: 'AirGate',
  site_subtitle: 'Control Panel',
  site_logo: '',
  api_base_url: '',
  frontend_url: '',
  contact_info: '',
  doc_url: '',
  home_content: '',
  registration_enabled: true,
  email_verify_enabled: false,
  settings_loaded: false,
};

const SiteSettingsContext = createContext<SiteSettings>(defaults);

function normalizeSiteSettings(data?: Record<string, string>, settingsLoaded = false): SiteSettings {
  return {
    ...defaults,
    ...data,
    registration_enabled: data?.registration_enabled !== 'false',
    email_verify_enabled: data?.email_verify_enabled === 'true',
    settings_loaded: settingsLoaded,
  };
}

function readCachedSiteSettings(): SiteSettings {
  if (typeof window === 'undefined') return defaults;
  try {
    const raw = window.localStorage.getItem(STORAGE_KEYS.settings.publicSite);
    if (!raw) return defaults;
    const parsed = JSON.parse(raw);
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) return defaults;
    return normalizeSiteSettings(parsed as Record<string, string>, false);
  } catch {
    return defaults;
  }
}

function writeCachedSiteSettings(data: Record<string, string> | undefined) {
  if (!data || typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(STORAGE_KEYS.settings.publicSite, JSON.stringify(data));
  } catch {
    // Storage can be unavailable in restricted browser modes.
  }
}

export function SiteSettingsProvider({ children }: { children: ReactNode }) {
  const [cachedSiteSettings] = useState(readCachedSiteSettings);
  const { data, isPending } = useQuery({
    queryKey: ['site-settings'],
    queryFn: () => settingsApi.getPublic(),
    staleTime: 60_000,
    refetchOnWindowFocus: true,
  });

  const value = useMemo(
    () => (data ? normalizeSiteSettings(data, true) : { ...cachedSiteSettings, settings_loaded: !isPending }),
    [cachedSiteSettings, data, isPending],
  );

  useEffect(() => {
    writeCachedSiteSettings(data);
  }, [data]);

  // 动态设置 favicon（优先自定义 logo，否则使用默认 logo）
  useEffect(() => {
    const logoHref = value.site_logo || defaultLogoUrl;
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!link) {
      link = document.createElement('link');
      link.rel = 'icon';
      document.head.appendChild(link);
    }
    link.href = logoHref;
    if (value.site_name) {
      document.title = value.site_subtitle
        ? `${value.site_name} - ${value.site_subtitle}`
        : value.site_name;
    }
  }, [value.site_logo, value.site_name, value.site_subtitle]);

  return (
    <SiteSettingsContext.Provider value={value}>
      {children}
    </SiteSettingsContext.Provider>
  );
}

export function useSiteSettings(): SiteSettings {
  return useContext(SiteSettingsContext);
}
