import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react';
import { injectThemeStyle, setTheme, getStoredTheme, type ThemeName } from '@devilgenius/airgate-theme';

const RELIABLE_MONO_FONT = '"Fira Code", ui-monospace, "SFMono-Regular", "SF Mono", "Cascadia Code", Consolas, "Liberation Mono", Menlo, Monaco, "Courier New", monospace';

interface ThemeContextValue {
  theme: ThemeName;
  toggleTheme: () => void;
}

const ThemeContext = createContext<ThemeContextValue | null>(null);

function syncHeroUIThemeClass(theme: ThemeName) {
  document.documentElement.classList.toggle('light', theme === 'light');
  document.documentElement.classList.toggle('dark', theme === 'dark');
}

function applyReliableMonoFont() {
  document.documentElement.style.setProperty('--ag-font-mono', RELIABLE_MONO_FONT);
  document.documentElement.style.setProperty('--font-mono', RELIABLE_MONO_FONT);
  const themeStyle = document.getElementById('ag-theme-vars');
  if (themeStyle?.textContent) {
    themeStyle.textContent = themeStyle.textContent.replace(
      /--ag-font-mono:\s*[^;]+;/g,
      `--ag-font-mono: ${RELIABLE_MONO_FONT};`,
    );
  }
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<ThemeName>(getStoredTheme);

  // 初始化：注入 AirGate CSS 变量。
  useEffect(() => {
    injectThemeStyle();
    applyReliableMonoFont();
  }, []);

  // 主题变化时同步 AirGate data-theme 与 HeroUI light/dark class。
  useEffect(() => {
    setTheme(theme);
    syncHeroUIThemeClass(theme);
    applyReliableMonoFont();
  }, [theme]);

  const toggleTheme = useCallback(() => {
    setThemeState((t) => (t === 'dark' ? 'light' : 'dark'));
  }, []);
  const value = useMemo(() => ({ theme, toggleTheme }), [theme, toggleTheme]);

  return (
    <ThemeContext value={value}>
      {children}
    </ThemeContext>
  );
}

export function useTheme(): ThemeContextValue {
  const ctx = useContext(ThemeContext);
  if (!ctx) throw new Error('useTheme 必须在 ThemeProvider 内使用');
  return ctx;
}
