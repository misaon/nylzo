import { servedByLocalClient } from '../local-client/instance-origin.ts';

export const LANGUAGES = ['en', 'cs', 'de', 'es', 'fr', 'id', 'it', 'ja', 'pl', 'pt-BR', 'sk', 'zh-Hans'] as const;

export type Language = (typeof LANGUAGES)[number];

export const DEFAULT_LANGUAGE = 'en' satisfies Language;

interface LanguageDetails {
  readonly name: string;
  readonly openGraphLocale: string;
  readonly fontSample: string;
}

export const LANGUAGE_DETAILS = {
  en: { name: 'English', openGraphLocale: 'en_US', fontSample: 'Aa' },
  cs: { name: 'Čeština', openGraphLocale: 'cs_CZ', fontSample: 'Aáčř' },
  de: { name: 'Deutsch', openGraphLocale: 'de_DE', fontSample: 'Aäß' },
  es: { name: 'Español', openGraphLocale: 'es_ES', fontSample: 'Añá' },
  fr: { name: 'Français', openGraphLocale: 'fr_FR', fontSample: 'Aàç' },
  id: { name: 'Bahasa Indonesia', openGraphLocale: 'id_ID', fontSample: 'Aa' },
  it: { name: 'Italiano', openGraphLocale: 'it_IT', fontSample: 'Aàè' },
  ja: { name: '日本語', openGraphLocale: 'ja_JP', fontSample: 'Aa' },
  pl: { name: 'Polski', openGraphLocale: 'pl_PL', fontSample: 'Aąłó' },
  'pt-BR': { name: 'Português (Brasil)', openGraphLocale: 'pt_BR', fontSample: 'Aãç' },
  sk: { name: 'Slovenčina', openGraphLocale: 'sk_SK', fontSample: 'Aáčľ' },
  'zh-Hans': { name: '简体中文', openGraphLocale: 'zh_CN', fontSample: 'Aa' },
} as const satisfies Readonly<Record<Language, LanguageDetails>>;

export interface PreferenceStore {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
}

const LANGUAGE_KEY = 'nylzo:lang';
const TRADITIONAL_CHINESE = new Set(['hant', 'hk', 'mo', 'tw']);

function baseOf(tag: string): string {
  return tag.toLowerCase().split('-', 1)[0] ?? '';
}

const BASE_LANGUAGES: ReadonlyMap<string, Language> = new Map([
  ...LANGUAGES.map((language) => [baseOf(language), language] as const),
  ['in', 'id'],
]);

export function homePath(language: Language): string {
  return language === DEFAULT_LANGUAGE ? '/' : `/${language.toLowerCase()}/`;
}

function matchLanguage(tag: string): Language | undefined {
  const [base = '', ...subtags] = tag.toLowerCase().split('-');
  if (base === 'zh' && subtags.some((subtag) => TRADITIONAL_CHINESE.has(subtag))) {
    return undefined;
  }
  return BASE_LANGUAGES.get(base);
}

export function pickLanguage(preferred: readonly string[]): Language {
  for (const tag of preferred) {
    const language = matchLanguage(tag);
    if (language) {
      return language;
    }
  }
  return DEFAULT_LANGUAGE;
}

export function browserPreferences(): PreferenceStore | undefined {
  if (servedByLocalClient()) {
    return undefined;
  }
  try {
    return localStorage;
  } catch {
    return undefined;
  }
}

export function storedLanguage(store: PreferenceStore | undefined): Language | undefined {
  let saved: string | null | undefined;
  try {
    saved = store?.getItem(LANGUAGE_KEY);
  } catch {
    return;
  }
  return LANGUAGES.find((language) => language === saved);
}

export function savedLanguage(store: PreferenceStore | undefined, preferred: readonly string[]): Language {
  return storedLanguage(store) ?? pickLanguage(preferred);
}

export function saveLanguage(store: PreferenceStore | undefined, language: Language): void {
  try {
    store?.setItem(LANGUAGE_KEY, language);
  } catch {}
}
