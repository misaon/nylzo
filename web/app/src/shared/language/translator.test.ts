import { describe, expect, it } from 'vitest';

import { catalogOf } from './catalog.ts';
import type { PluralForms } from './catalog.ts';
import { LANGUAGES, pickLanguage, savedLanguage, saveLanguage } from './language.ts';
import type { Language, PreferenceStore } from './language.ts';

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
