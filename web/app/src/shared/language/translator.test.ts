import { describe, expect, it } from 'vitest';

import { czech, type MessageKey } from './czech.ts';
import { english } from './english.ts';
import { formatBytes, formatDays, formatDuration, formatSize, formatWait } from './format.ts';
import { pickLanguage, saveLanguage, savedLanguage, type PreferenceStore } from './language.ts';
import { Translator } from './translator.svelte.ts';

const placeholders = (text: string) => [...text.matchAll(/\{(\w+)\}/g)].map((match) => match[1]).sort();

describe('translations', () => {
  it('has the same keys and placeholders in both languages', () => {
    const keys = Object.keys(czech) as MessageKey[];
    expect(Object.keys(english).sort()).toEqual([...keys].sort());
    for (const key of keys) {
      expect(placeholders(english[key]), key).toEqual(placeholders(czech[key]));
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
    const { translate } = new Translator('en');
    expect(translate('open.chip.opens.other', { count: 3 })).toBe('3 opens left');
    expect(translate('open.chip.opens.other')).toBe('{count} opens left');
  });

  it('formats durations and sizes', () => {
    expect(formatDuration(45, 'en')).toBe('45 seconds');
    expect(formatDuration(61, 'en')).toBe('2 minutes');
    expect(formatDuration(3600, 'en')).toBe('1 hour');
    expect(formatDuration(3601, 'en')).toBe('2 hours');
    expect(formatBytes(5_242_880, 'en')).toBe('5 MB');
    expect(formatBytes(102_400, 'en')).toBe('100 kB');
    const plain = (text: string) => text.replaceAll(/\s/gu, ' ');
    expect(plain(formatSize(0, 'en'))).toBe('0 B');
    expect(plain(formatSize(312, 'cs'))).toBe('312 B');
    expect(formatSize(5_242_880, 'en')).toBe('5 MB');
    expect([1, 60, 3600, 120, 300].map((seconds) => plain(formatWait(seconds, 'cs')))).toEqual([
      '1 sekundu',
      '1 minutu',
      '1 hodinu',
      '2 minuty',
      '5 minut',
    ]);
    expect(formatWait(60, 'en')).toBe('1 minute');
    expect(formatWait(undefined, 'en')).toBe('1 minute');
    expect(formatDays(7, 'en')).toBe('7 days');
  });

  it('switches the language in place', () => {
    const translator = new Translator('en');
    const { translate } = translator;
    expect(translate('footer.sent')).toBe('Sent messages');
    translator.language = 'cs';
    expect(translate('footer.sent')).toBe('Odeslané zprávy');
  });

  it('prefers a saved choice and survives storage that throws', () => {
    const saved = new Map<string, string>();
    const store: PreferenceStore = {
      getItem: (key) => saved.get(key) ?? null,
      setItem: (key, value) => void saved.set(key, value),
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
