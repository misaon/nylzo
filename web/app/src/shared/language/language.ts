import { languages as table } from '../../../../../internal/server/webclient/languages.json';
import { servedByLocalClient } from '../local-client/instance-origin.ts';
import { hasCatalog } from './catalog.ts';
import type { Language } from './catalog.ts';

export type { Language } from './catalog.ts';

export const LANGUAGES: readonly Language[] = table.map(({ tag }) => {
  if (!hasCatalog(tag)) {
    throw new Error(`languages.json names ${tag}, which has no message catalog`);
  }
  return tag;
});

export const DEFAULT_LANGUAGE = 'en' satisfies Language;

interface LanguageDetails {
  readonly name: string;
  readonly openGraphLocale: string;
  readonly fontSample: string;
}

const DETAILS: ReadonlyMap<string, LanguageDetails> = new Map(table.map(({ tag, ...details }) => [tag, details]));

export function languageDetails(language: Language): LanguageDetails {
  const details = DETAILS.get(language);
  if (details === undefined) {
    throw new Error(`languages.json has no entry for ${language}`);
  }
  return details;
}

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
