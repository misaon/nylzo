import { czech as accessCzech } from './access.cs.ts';
import { english as accessEnglish } from './access.en.ts';
import { czech as dashboardCzech } from './dashboard.cs.ts';
import { english as dashboardEnglish } from './dashboard.en.ts';

const CATALOGS = {
  cs: { ...accessCzech, ...dashboardCzech },
  en: { ...accessEnglish, ...dashboardEnglish },
} as const;

export type AdminLanguage = keyof typeof CATALOGS;

export type AdminMessageKey = keyof (typeof CATALOGS)['cs'];

type Values = Readonly<Record<string, number | string>>;

const PLACEHOLDER = /\{(\w+)\}/gu;

export function adminLanguage(languages: readonly string[] = navigator.languages): AdminLanguage {
  return languages[0]?.toLowerCase().startsWith('cs') ? 'cs' : 'en';
}

export interface AdminTranslator {
  readonly language: AdminLanguage;
  readonly translate: (key: AdminMessageKey, values?: Values) => string;
}

export function adminTranslator(language: AdminLanguage = adminLanguage()): AdminTranslator {
  const catalog: Readonly<Record<AdminMessageKey, string>> = CATALOGS[language];
  return {
    language,
    translate: (key, values = {}) =>
      catalog[key].replaceAll(PLACEHOLDER, (placeholder, name: string) =>
        Object.hasOwn(values, name) ? String(values[name]) : placeholder,
      ),
  };
}
