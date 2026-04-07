import { useNavigate } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { useSiteSettings, defaultLogoUrl } from '../app/providers/SiteSettingsProvider';
import { useTheme } from '../app/providers/ThemeProvider';
import { getToken } from '../shared/api/client';
import {
  Zap, Shield, Globe, ArrowRight, Sun, Moon, Code, BarChart3, KeyRound, Layers,
} from 'lucide-react';

export default function HomePage() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const site = useSiteSettings();
  const { theme, toggleTheme } = useTheme();

  const isLoggedIn = !!getToken();

  const features = [
    { icon: <Zap className="w-6 h-6" />, titleKey: 'home.feature_gateway', descKey: 'home.feature_gateway_desc' },
    { icon: <Shield className="w-6 h-6" />, titleKey: 'home.feature_security', descKey: 'home.feature_security_desc' },
    { icon: <Layers className="w-6 h-6" />, titleKey: 'home.feature_plugins', descKey: 'home.feature_plugins_desc' },
    { icon: <BarChart3 className="w-6 h-6" />, titleKey: 'home.feature_analytics', descKey: 'home.feature_analytics_desc' },
    { icon: <KeyRound className="w-6 h-6" />, titleKey: 'home.feature_keys', descKey: 'home.feature_keys_desc' },
    { icon: <Globe className="w-6 h-6" />, titleKey: 'home.feature_multi_platform', descKey: 'home.feature_multi_platform_desc' },
  ];

  return (
    <div className="min-h-screen bg-bg-deep text-text relative overflow-hidden">
      {/* 背景装饰 */}
      <div className="absolute inset-0 pointer-events-none overflow-hidden">
        <div
          className="absolute -top-[300px] left-1/2 -translate-x-1/2 w-[800px] h-[800px] rounded-full opacity-[0.07]"
          style={{ background: 'radial-gradient(circle, var(--ag-primary), transparent 65%)' }}
        />
        <div
          className="absolute top-[60%] -right-[200px] w-[500px] h-[500px] rounded-full opacity-[0.04]"
          style={{ background: 'radial-gradient(circle, var(--ag-info), transparent 65%)' }}
        />
      </div>

      {/* 导航栏 */}
      <nav className="relative z-10 flex items-center justify-between px-6 md:px-12 py-4 max-w-6xl mx-auto">
        <div className="flex items-center gap-2.5">
          <img src={site.site_logo || defaultLogoUrl} alt="" className="w-8 h-8 rounded-sm object-cover" />
          <span className="text-base font-bold tracking-tight">{site.site_name || 'AirGate'}</span>
        </div>
        <div className="flex items-center gap-2">
          {site.doc_url && (
            <a
              href={site.doc_url}
              target="_blank"
              rel="noopener noreferrer"
              className="px-3 py-1.5 text-xs font-medium text-text-secondary hover:text-text transition-colors"
            >
              {t('home.docs')}
            </a>
          )}
          <button
            onClick={toggleTheme}
            className="flex items-center justify-center w-8 h-8 rounded-lg text-text-tertiary hover:text-text hover:bg-bg-hover transition-colors"
          >
            {theme === 'dark' ? <Sun className="w-4 h-4" /> : <Moon className="w-4 h-4" />}
          </button>
          <button
            onClick={() => navigate({ to: isLoggedIn ? '/' : '/login' })}
            className="ml-2 px-4 py-1.5 text-xs font-medium rounded-lg bg-[var(--ag-primary)] text-white hover:opacity-90 transition-opacity"
          >
            {isLoggedIn ? t('home.go_dashboard') : t('home.login')}
          </button>
        </div>
      </nav>

      {/* Hero */}
      <section className="relative z-10 text-center px-6 pt-16 pb-20 md:pt-24 md:pb-28 max-w-4xl mx-auto">
        <div className="inline-flex items-center gap-2 px-4 py-1.5 rounded-full text-xs font-medium mb-6 border border-[var(--ag-glass-border)] bg-[var(--ag-bg-surface)]">
          <Code className="w-3.5 h-3.5 text-[var(--ag-primary)]" />
          <span className="text-text-secondary">{t('home.badge')}</span>
        </div>
        <h1 className="text-4xl md:text-5xl font-bold leading-tight tracking-tight mb-4">
          {site.site_name || 'AirGate'}
        </h1>
        <p className="text-base md:text-lg text-text-tertiary max-w-xl mx-auto mb-8 leading-relaxed">
          {site.site_subtitle || t('home.subtitle')}
        </p>
        <div className="flex items-center justify-center gap-3">
          <button
            onClick={() => navigate({ to: isLoggedIn ? '/' : '/login' })}
            className="inline-flex items-center gap-2 px-6 py-2.5 text-sm font-medium rounded-xl bg-[var(--ag-primary)] text-white hover:opacity-90 transition-opacity"
          >
            {isLoggedIn ? t('home.go_dashboard') : t('home.get_started')}
            <ArrowRight className="w-4 h-4" />
          </button>
          {site.doc_url && (
            <a
              href={site.doc_url}
              target="_blank"
              rel="noopener noreferrer"
              className="inline-flex items-center gap-2 px-6 py-2.5 text-sm font-medium rounded-xl border border-[var(--ag-glass-border)] text-text-secondary hover:text-text hover:bg-bg-hover transition-colors"
            >
              {t('home.view_docs')}
            </a>
          )}
        </div>

        {/* API 地址展示 */}
        {site.api_base_url && (
          <div className="mt-10 inline-flex items-center gap-2 px-5 py-2.5 rounded-xl bg-[var(--ag-bg-surface)] border border-[var(--ag-glass-border)] text-sm font-mono">
            <span className="text-text-tertiary">API</span>
            <span className="text-text">{site.api_base_url}</span>
          </div>
        )}
      </section>

      {/* 特性卡片 */}
      <section className="relative z-10 px-6 pb-20 max-w-5xl mx-auto">
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {features.map((f) => (
            <div
              key={f.titleKey}
              className="p-5 rounded-2xl border border-[var(--ag-glass-border)] bg-[var(--ag-bg-surface)] hover:border-[var(--ag-border)] transition-colors"
            >
              <div className="flex items-center justify-center w-10 h-10 rounded-xl bg-[var(--ag-primary-subtle)] text-[var(--ag-primary)] mb-3">
                {f.icon}
              </div>
              <h3 className="text-sm font-semibold mb-1">{t(f.titleKey)}</h3>
              <p className="text-xs text-text-tertiary leading-relaxed">{t(f.descKey)}</p>
            </div>
          ))}
        </div>
      </section>

      {/* 自定义 HTML 内容 */}
      {site.home_content && (
        <section className="relative z-10 px-6 pb-16 max-w-4xl mx-auto">
          <div
            className="prose prose-sm dark:prose-invert max-w-none text-text-secondary"
            dangerouslySetInnerHTML={{ __html: site.home_content }}
          />
        </section>
      )}

      {/* 联系方式 & 底部 */}
      <footer className="relative z-10 border-t border-[var(--ag-glass-border)] py-8 text-center">
        <div className="flex items-center justify-center gap-4 text-xs text-text-tertiary">
          <span>{site.site_name || 'AirGate'}</span>
          {site.contact_info && (
            <>
              <span className="w-px h-3 bg-[var(--ag-border)]" />
              <span>{site.contact_info}</span>
            </>
          )}
          {site.doc_url && (
            <>
              <span className="w-px h-3 bg-[var(--ag-border)]" />
              <a href={site.doc_url} target="_blank" rel="noopener noreferrer" className="hover:text-text transition-colors">
                {t('home.docs')}
              </a>
            </>
          )}
        </div>
      </footer>
    </div>
  );
}
