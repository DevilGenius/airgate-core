import { memo } from 'react';
import { Button, Link as HeroLink } from '@heroui/react';
import { useTranslation } from 'react-i18next';
import {
  Activity,
  BookOpen,
  Languages,
  LogOut,
  Menu,
  MessageCircle,
  Moon,
  Sun,
} from 'lucide-react';
import { ensureLanguageBundle, setStoredLanguage } from '../../i18n';
import { useTheme } from '../providers/ThemeProvider';
import { useSiteSettings } from '../providers/SiteSettingsProvider';
import { effectiveDocUrl } from '../../shared/utils/docUrl';
import type { ShellIdentity } from './useShellIdentity';

interface AppHeaderProps {
  isMobile: boolean;
  onOpenMobileMenu: () => void;
  shell: ShellIdentity;
  showStatusEntry: boolean;
}

export const AppHeader = memo(function AppHeader({
  isMobile,
  onOpenMobileMenu,
  shell,
  showStatusEntry,
}: AppHeaderProps) {
  const { t, i18n } = useTranslation();
  const { theme, toggleTheme } = useTheme();
  const site = useSiteSettings();
  const docs = effectiveDocUrl(site.doc_url);

  const toggleLanguage = () => {
    const nextLang = i18n.language === 'zh' ? 'en' : 'zh';
    void ensureLanguageBundle(nextLang).then(() => i18n.changeLanguage(nextLang));
    setStoredLanguage(nextLang);
  };

  return (
    <header className="ag-topbar pointer-events-auto absolute inset-x-0 top-0 z-20 flex h-12 items-center justify-between gap-3 px-4 md:px-5">
      <div className="flex shrink-0 items-center gap-3">
        {isMobile && (
          <Button
            aria-label={t('nav.open_menu', 'Open menu')}
            isIconOnly
            size="sm"
            variant="ghost"
            onPress={onOpenMobileMenu}
          >
            <Menu className="h-5 w-5" />
          </Button>
        )}
      </div>

      <div className="flex shrink-0 items-center gap-2">
        {showStatusEntry && (
          <HeroLink
            href="/status"
            aria-label={t('nav.status')}
            className="flex h-10 w-10 items-center justify-center rounded-[var(--radius)] text-text-secondary transition-colors hover:text-text"
          >
            <Activity className="h-5 w-5" />
          </HeroLink>
        )}
        <HeroLink
          href={docs.href}
          {...(docs.isExternal ? { target: '_blank', rel: 'noopener noreferrer' } : {})}
          aria-label={t('nav.docs')}
          className="hidden h-10 w-10 items-center justify-center rounded-[var(--radius)] text-text-secondary transition-colors hover:text-text sm:flex"
        >
          <BookOpen className="h-5 w-5" />
        </HeroLink>
        {site.contact_info && (
          <div className="hidden items-center gap-2 text-text-tertiary lg:flex">
            <MessageCircle className="h-5 w-5 shrink-0" />
            <span className="text-sm">{site.contact_info}</span>
          </div>
        )}
        <Button
          aria-label={i18n.language === 'zh' ? 'Switch to English' : '切换为中文'}
          className="h-10 px-3"
          size="sm"
          variant="ghost"
          onPress={toggleLanguage}
        >
          <Languages className="h-5 w-5" />
          <span className="hidden w-8 text-center font-mono text-xs uppercase sm:inline-block">
            {i18n.language === 'zh' ? 'EN' : '中文'}
          </span>
        </Button>
        <Button
          aria-label={theme === 'dark' ? '切换亮色模式' : '切换暗色模式'}
          className="h-10 w-10"
          isIconOnly
          size="sm"
          variant="ghost"
          onPress={toggleTheme}
        >
          {theme === 'dark' ? <Sun className="h-5 w-5" /> : <Moon className="h-5 w-5" />}
        </Button>

        <div className="mx-1.5 hidden h-6 w-px bg-border sm:block" />

        <div className="hidden items-center gap-2.5 pl-1 sm:flex">
          {!shell.isAPIKeySession && shell.balanceValue !== null && (
            <div
              className="flex h-7 items-center rounded-[calc(var(--radius)-2px)] bg-success-subtle px-2.5 text-text"
              title={`${t('user_overview.balance', 'Balance')}: ${shell.balanceText}`}
            >
              <span className="font-mono text-sm font-bold tabular-nums">
                <span className="text-success">$</span>
                {shell.balanceValue.toFixed(4)}
              </span>
            </div>
          )}
          {!shell.isAPIKeySession && (
            <div className="hidden text-right md:block">
              <p className="text-sm font-medium leading-tight text-text">
                {shell.displayName}
              </p>
              <p className="text-xs leading-tight text-text-tertiary">
                {shell.user?.email}
              </p>
            </div>
          )}
        </div>

        <div className="mx-1 hidden h-6 w-px bg-border sm:block" />
        <Button
          aria-label={t('common.logout')}
          className="h-10 w-10 text-text-secondary hover:bg-danger/10 hover:text-danger"
          isIconOnly
          size="sm"
          variant="ghost"
          onPress={shell.logout}
        >
          <LogOut className="h-5 w-5" />
        </Button>
      </div>
    </header>
  );
});
