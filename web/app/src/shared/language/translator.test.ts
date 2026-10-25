import { describe, expect, it } from 'vitest';

import { czech } from './czech.ts';
import { english } from './english.ts';
import { pickLanguage, type PreferenceStore, savedLanguage, saveLanguage } from './language.ts';

function placeholders(messages: Readonly<Record<string, string>>): Record<string, string[]> {
  return Object.fromEntries(
    Object.entries(messages).map(([key, text]) => [
      key,
      Array.from(text.matchAll(/\{(\w+)\}/g), (match) => match[1] ?? '').toSorted((first, second) =>
        first.localeCompare(second),
      ),
    ]),
  );
}

describe('translations', () => {
  it('has the same placeholders in both languages', () => {
    expect(placeholders(english)).toEqual(placeholders(czech));
  });

  it('picks the first supported language', () => {
    expect(pickLanguage(['cs-CZ', 'en'])).toBe('cs');
    expect(pickLanguage(['sk'])).toBe('cs');
    expect(pickLanguage(['de-DE', 'en-GB'])).toBe('en');
    expect(pickLanguage(['fr'])).toBe('en');
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
    saveLanguage(store, 'en');
    expect(savedLanguage(store, ['cs-CZ'])).toBe('en');
    saved.set('nylzo:lang', 'de');
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
