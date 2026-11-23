import { afterEach, describe, expect, it, vi } from 'vitest';

import { catalogOf } from './catalog.ts';
import type { PluralForms } from './catalog.ts';
import { browserPreferences, LANGUAGES, pickLanguage, savedLanguage, saveLanguage } from './language.ts';
import type { Language, PreferenceStore } from './language.ts';
import { Translator } from './translator.svelte.ts';

const WHOLE_COUNTS = Array.from({ length: 1001 }, (_, count) => count);

function entries(language: Language): ReadonlyMap<string, PluralForms | string> {
  return new Map(Object.entries(catalogOf(language)));
}

function placeholders(text: string): string[] {
  return Array.from(text.matchAll(/\{(\w+)\}/gu), (match) => match[1] ?? '').toSorted();
}

describe('translations', () => {
  it.each(LANGUAGES)('%s keeps the placeholders of every Czech message', (language) => {
    const translated = entries(language);
    for (const [key, source] of entries('cs')) {
      const expected = placeholders(typeof source === 'string' ? source : source.other);
      const entry = translated.get(key) ?? '';
      for (const text of typeof entry === 'string' ? [entry] : Object.values(entry)) {
        expect(placeholders(text), `${language} ${key}`).toEqual(expected);
      }
    }
  });

  it.each(LANGUAGES)('%s has the plural forms its grammar needs for whole counts', (language) => {
    const rules = new Intl.PluralRules(language);
    const needed = [...new Set(WHOLE_COUNTS.map((count) => rules.select(count)))];
    const known = rules.resolvedOptions().pluralCategories;
    const plurals = [...entries(language)].filter(
      (entry): entry is [string, PluralForms] => typeof entry[1] !== 'string',
    );
    expect(plurals.length).toBeGreaterThan(0);
    for (const [key, forms] of plurals) {
      expect(Object.keys(forms), `${language} ${key}`).toEqual(expect.arrayContaining(needed));
      expect(known, `${language} ${key}`).toEqual(expect.arrayContaining(Object.keys(forms)));
    }
  });

  it('picks the first supported language', () => {
    expect(pickLanguage(['cs-CZ', 'en'])).toBe('cs');
    expect(pickLanguage(['sk'])).toBe('sk');
    expect(pickLanguage(['de-AT', 'en-GB'])).toBe('de');
    expect(pickLanguage(['pt-PT'])).toBe('pt-BR');
    expect(pickLanguage(['zh-CN'])).toBe('zh-Hans');
    expect(pickLanguage(['zh-Hant-TW', 'ja-JP'])).toBe('ja');
    expect(pickLanguage(['in-ID'])).toBe('id');
    expect(pickLanguage(['nl', 'fr-CA'])).toBe('fr');
    expect(pickLanguage(['ru'])).toBe('en');
    expect(pickLanguage([])).toBe('en');
  });

  it('prefers a saved choice and survives storage that throws', () => {
    const saved = new Map<string, string>();
    const store: PreferenceStore = {
      getItem: (key) => saved.get(key) ?? null,
      setItem: (key, value) => {
        saved.set(key, value);
      },
    };
    expect(savedLanguage(store, ['en'])).toBe('en');
    saveLanguage(store, 'cs');
    expect(saved.get('nylzo:lang')).toBe('cs');
    expect(savedLanguage(store, ['en'])).toBe('cs');
    saveLanguage(store, 'pt-BR');
    expect(savedLanguage(store, ['cs-CZ'])).toBe('pt-BR');
    saved.set('nylzo:lang', 'ru');
    expect(savedLanguage(store, ['cs-CZ'])).toBe('cs');
    const broken: PreferenceStore = {
      getItem: () => {
        throw new Error('blocked');
      },
      setItem: () => {
        throw new Error('blocked');
      },
    };
    expect(savedLanguage(broken, ['cs'])).toBe('cs');
    expect(() => saveLanguage(broken, 'en')).not.toThrow();
    expect(savedLanguage(undefined, ['en-US'])).toBe('en');
  });
});

describe('plural forms', () => {
  it('fall back to the other form for a count whose category the catalog leaves out', () => {
    const { plural } = new Translator('cs');
    const forms = catalogOf('cs')['wait.minute'];
    expect(forms.many).toBeUndefined();
    expect(plural('wait.minute', 1.5)).toBe(forms.other.replace('{count}', '1,5'));
  });
});

describe('the language table', () => {
  afterEach(() => {
    vi.doUnmock('../../../../../internal/server/webclient/languages.json');
    vi.resetModules();
  });

  it('refuses a language without a message catalog', async () => {
    vi.resetModules();
    vi.doMock('../../../../../internal/server/webclient/languages.json', () => ({
      languages: [{ tag: 'tlh', name: 'tlhIngan Hol', openGraphLocale: 'tlh', fontSample: 'Aa' }],
    }));
    await expect(import('./language.ts')).rejects.toThrow('languages.json names tlh, which has no message catalog');
  });

  it('refuses to describe a language it has no entry for', async () => {
    vi.resetModules();
    vi.doMock('../../../../../internal/server/webclient/languages.json', () => ({
      languages: [{ tag: 'en', name: 'English', openGraphLocale: 'en_US', fontSample: 'Aa' }],
    }));
    const { languageDetails } = await import('./language.ts');
    expect(languageDetails('en').name).toBe('English');
    expect(() => languageDetails('cs')).toThrow('languages.json has no entry for cs');
  });

  it('names no catalog that was not loaded', async () => {
    vi.resetModules();
    const { catalogOf: freshCatalogOf } = await import('./catalog.ts');
    expect(() => freshCatalogOf('cs')).toThrow('the cs messages are not loaded');
  });
});

function servedFrom(origin: string, instance = origin): void {
  vi.stubGlobal('location', { origin });
  vi.stubGlobal('document', { querySelector: () => ({ content: instance }) });
}

describe('browser preferences', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('keep the choice in local storage on the instance itself', () => {
    const storage: PreferenceStore = { getItem: () => null, setItem: vi.fn<PreferenceStore['setItem']>() };
    servedFrom('https://nylzo.com');
    vi.stubGlobal('localStorage', storage);
    expect(browserPreferences()).toBe(storage);
  });

  it('keep nothing in the local client, whose origin nobody purges', () => {
    servedFrom('http://127.0.0.1:8471', 'https://nylzo.com');
    vi.stubGlobal('localStorage', { getItem: () => null, setItem: vi.fn<PreferenceStore['setItem']>() });
    expect(browserPreferences()).toBeUndefined();
  });

  it('keep nothing when the browser blocks local storage', () => {
    servedFrom('https://nylzo.com');
    Object.defineProperty(globalThis, 'localStorage', {
      configurable: true,
      get: () => {
        throw new DOMException('blocked', 'SecurityError');
      },
    });
    try {
      expect(browserPreferences()).toBeUndefined();
    } finally {
      Reflect.deleteProperty(globalThis, 'localStorage');
    }
  });
});
