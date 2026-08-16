// Lightweight built-in i18n for MAGI (P2 D14). No external dependency: a small
// zustand store keeps the active language reactive, dictionaries live in
// resources.ts, and t() falls back en -> key so untranslated strings degrade
// gracefully instead of breaking the UI.
import { create } from 'zustand';

export type Lang = 'en' | 'zh';

export const STORAGE_KEY = 'magi.lang';

// en is the source dictionary; zh overlays translations. t() falls back to en,
// then to the raw key.
import { en, zh } from './resources';

export interface I18nState {
  lang: Lang;
  setLang: (l: Lang) => void;
  toggle: () => void;
}

function initialLang(): Lang {
  if (typeof window === 'undefined') return 'en';
  const stored = window.localStorage.getItem(STORAGE_KEY);
  return stored === 'zh' || stored === 'en' ? stored : 'en';
}

export const useI18nStore = create<I18nState>((set, get) => ({
  lang: initialLang(),
  setLang: (l) => {
    if (typeof window !== 'undefined') window.localStorage.setItem(STORAGE_KEY, l);
    set({ lang: l });
  },
  toggle: () => get().setLang(get().lang === 'en' ? 'zh' : 'en'),
}));

type Dict = Record<string, string>;

function resolve(dict: Dict, key: string): string | undefined {
  return dict[key];
}

// t returns the translated string for the current language.
export function t(key: string, vars?: Record<string, string | number>, lang?: Lang): string {
  const active = lang ?? useI18nStore.getState().lang;
  const dict = active === 'zh' ? zh : en;
  let s = resolve(dict, key) ?? resolve(en, key) ?? key;
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      s = s.split(`{${k}}`).join(String(v));
    }
  }
  return s;
}

// useT returns a bound translate function that re-renders on language change.
export function useT(): (key: string, vars?: Record<string, string | number>) => string {
  const lang = useI18nStore((s) => s.lang);
  return (key, vars) => t(key, vars, lang);
}

// useLang exposes the reactive language + setter for a language switcher.
export function useLang() {
  const lang = useI18nStore((s) => s.lang);
  const setLang = useI18nStore((s) => s.setLang);
  return { lang, setLang };
}
