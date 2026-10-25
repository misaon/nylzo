export const LANGUAGES = ['en', 'cs'] as const;

export type Language = (typeof LANGUAGES)[number];

export interface PreferenceStore {
  getItem: (key: string) => string | null;
  setItem: (key: string, value: string) => void;
}

const LANGUAGE_KEY = 'nylzo:lang';
const CZECH_READERS = new Set(['cs', 'sk']);

export function pickLanguage(preferred: readonly string[]): Language {
  for (const tag of preferred) {
    const [base] = tag.toLowerCase().split('-', 1);
    if (base !== undefined && CZECH_READERS.has(base)) {
      return 'cs';
    }
    if (base === 'en') {
      return 'en';
    }
  }
  return 'en';
}

export function browserPreferences(): PreferenceStore | undefined {
  try {
    return localStorage;
  } catch {
    return;
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
  } catch {
    return;
  }
}
