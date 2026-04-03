import { createContext, useContext, useEffect, type ReactNode } from 'react';
import { useQuery } from '@tanstack/react-query';
import { settingsApi } from '../../shared/api/settings';

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
};

const SiteSettingsContext = createContext<SiteSettings>(defaults);

export function SiteSettingsProvider({ children }: { children: ReactNode }) {
  const { data } = useQuery({
    queryKey: ['site-settings'],
    queryFn: () => settingsApi.getPublic(),
    staleTime: 60_000,
    refetchOnWindowFocus: true,
  });

  const value: SiteSettings = {
    ...defaults,
    ...data,
    // Boolean 字段从字符串转换
    registration_enabled: data?.registration_enabled !== 'false',
    email_verify_enabled: data?.email_verify_enabled === 'true',
  };

  // 动态设置 favicon
  useEffect(() => {
    if (!value.site_logo) return;
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!link) {
      link = document.createElement('link');
      link.rel = 'icon';
      document.head.appendChild(link);
    }
    link.href = value.site_logo;
  }, [value.site_logo]);

  return (
    <SiteSettingsContext.Provider value={value}>
      {children}
    </SiteSettingsContext.Provider>
  );
}

export function useSiteSettings(): SiteSettings {
  return useContext(SiteSettingsContext);
}
