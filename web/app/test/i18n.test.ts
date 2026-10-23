import { describe, expect, it } from 'vitest';

import { formatBytes, formatDuration } from '../src/lib/format.ts';
import { createI18n, dictionaries, otherLanguage, pickLanguage, saveLanguage, savedLanguage, type MessageKey, type PreferenceStore } from '../src/lib/i18n.svelte.ts';

const placeholders = (text: string) => [...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort();

describe('i18n', () => {
  it('has the same keys and placeholders in both languages', () => {
    const keys = Object.keys(dictionaries.cs) as MessageKey[];
    expect(Object.keys(dictionaries.en).sort()).toEqual([...keys].sort());
    for (const key of keys) {
      expect(placeholders(dictionaries.en[key]), key).toEqual(placeholders(dictionaries.cs[key]));
    }
  });

  it('picks the first supported language', () => {
    expect(pickLanguage(['cs-CZ', 'en'])).toBe('cs');
    expect(pickLanguage(['sk'])).toBe('cs');
    expect(pickLanguage(['de-DE', 'en-GB'])).toBe('en');
    expect(pickLanguage(['fr'])).toBe('en');
    expect(pickLanguage([])).toBe('en');
  });

  it('fills placeholders and leaves unknown ones visible', () => {
    const { t } = createI18n('en');
    expect(t('read.opens', { count: 3 })).toBe('Opens left: 3');
    expect(t('read.opens')).toBe('Opens left: {count}');
  });

  it('formats durations and sizes', () => {
    expect(formatDuration(45, 'en')).toBe('45 seconds');
    expect(formatDuration(61, 'en')).toBe('2 minutes');
    expect(formatDuration(3600, 'en')).toBe('1 hour');
    expect(formatDuration(3601, 'en')).toBe('2 hours');
    expect(formatBytes(5242880, 'en')).toBe('5 MB');
    expect(formatBytes(102400, 'en')).toBe('100 kB');
  });

  it('switches the language in place', () => {
    const i18n = createI18n('en');
    const { t } = i18n;
    expect(t('nav.sent')).toBe('Sent');
    i18n.language = otherLanguage(i18n.language);
    expect(t('nav.sent')).toBe('Odeslané');
    expect(otherLanguage('cs')).toBe('en');
  });

  it('prefers a saved choice and survives storage that throws', () => {
    const map = new Map<string, string>();
    const store: PreferenceStore = { getItem: (key) => map.get(key) ?? null, setItem: (key, value) => void map.set(key, value) };
    expect(savedLanguage(store, ['en'])).toBe('en');
    saveLanguage(store, 'cs');
    expect(map.get('nylzo:lang')).toBe('cs');
    expect(savedLanguage(store, ['en'])).toBe('cs');
    saveLanguage(store, 'en');
    expect(savedLanguage(store, ['cs-CZ'])).toBe('en');
    map.set('nylzo:lang', 'de');
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
