import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import zh from './zh.json';
import { STORAGE_KEYS } from '../shared/storageKeys';

export function getStoredLanguage() {
  if (typeof window === 'undefined') return 'zh';
  try {
    return window.localStorage.getItem(STORAGE_KEYS.i18n.language) || 'zh';
  } catch {
    return 'zh';
  }
}

export function setStoredLanguage(lang: string) {
  if (typeof window === 'undefined') return;
  try {
    window.localStorage.setItem(STORAGE_KEYS.i18n.language, lang);
  } catch {
    // Language switching should keep working when storage is unavailable.
  }
}

// 仅将默认语言 zh 打包进主 chunk；其余语言按需动态加载，避免非活跃语言
// （en.json 约 54KB）固化在首屏 bundle 中。zh 用户因此免去解析英文词条的开销。
const LANGUAGE_LOADERS: Record<string, () => Promise<{ default: Record<string, unknown> }>> = {
  en: () => import('./en.json'),
};

/** 确保目标语言资源已加载，已加载过的直接命中。 */
export async function ensureLanguageBundle(lng: string): Promise<void> {
  if (lng === 'zh' || !LANGUAGE_LOADERS[lng]) return;
  if (i18n.hasResourceBundle(lng, 'translation')) return;
  const { default: resources } = await LANGUAGE_LOADERS[lng]();
  i18n.addResourceBundle(lng, 'translation', resources, true, true);
}

const initialLng = getStoredLanguage();

i18n.use(initReactI18next).init({
  resources: {
    zh: { translation: zh },
  },
  lng: initialLng,
  fallbackLng: 'zh',
  interpolation: { escapeValue: false },
});

// 启动时若用户偏好非默认语言，先异步补充语言包再切换，避免阻塞首屏渲染。
if (initialLng !== 'zh') {
  void ensureLanguageBundle(initialLng).then(() => {
    if (i18n.language !== initialLng) i18n.changeLanguage(initialLng);
  });
}

export default i18n;
