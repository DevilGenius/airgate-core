import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import zh from './zh.json';
import en from './en.json';
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

i18n.use(initReactI18next).init({
  resources: {
    zh: { translation: zh },
    en: { translation: en },
  },
  lng: getStoredLanguage(),
  fallbackLng: 'zh',
  interpolation: { escapeValue: false },
});

export default i18n;
