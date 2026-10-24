export type Language = 'cs' | 'en';

const LANGUAGE_KEY = 'nylzo:lang';

export interface PreferenceStore {
  getItem(key: string): string | null;
  setItem(key: string, value: string): void;
}

export function pickLanguage(preferred: readonly string[]): Language {
  for (const tag of preferred) {
    const base = tag.toLowerCase().split('-')[0];
    if (base === 'cs' || base === 'sk') {
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
    return globalThis.localStorage;
  } catch {
    return undefined;
  }
}

export function storedLanguage(store: PreferenceStore | undefined): Language | undefined {
  let saved: string | null = null;
  try {
    saved = store?.getItem(LANGUAGE_KEY) ?? null;
  } catch {
    saved = null;
  }
  return saved === 'cs' || saved === 'en' ? saved : undefined;
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
