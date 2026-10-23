import { MIN_PASSPHRASE_CHARS } from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import { WEAK_BELOW_CHARS, WEAK_BELOW_WORDS } from '../src/lib/create.ts';
import { formatBytes, formatDays, formatDuration, formatWait } from '../src/lib/format.ts';
import { MAX_REASON_BYTES } from '../src/lib/report.ts';
import { RETENTION_DAYS } from '../src/lib/sent.ts';
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
    const plain = (text: string) => text.replace(/\s/gu, ' ');
    expect([1, 60, 3600, 120, 300].map((seconds) => plain(formatWait(seconds, 'cs')))).toEqual(['1 sekundu', '1 minutu', '1 hodinu', '2 minuty', '5 minut']);
    expect(formatWait(60, 'en')).toBe('1 minute');
    expect(formatWait(undefined, 'en')).toBe('1 minute');
    expect(formatDays(7, 'en')).toBe('7 days');
  });

  it('takes the limits it names from the code', () => {
    const limits = { min: MIN_PASSPHRASE_CHARS, words: WEAK_BELOW_WORDS, chars: WEAK_BELOW_CHARS, bytes: MAX_REASON_BYTES, days: RETENTION_DAYS };
    for (const language of ['cs', 'en'] as const) {
      for (const [key, text] of Object.entries(dictionaries[language]).filter(([key]) => !key.startsWith('ttl.'))) {
        expect(text, `${language} ${key}`).not.toMatch(/\b(12|16|1000)\b|\b7 (days|dní|dnů)\b/u);
        expect(createI18n(language).t(key as MessageKey, limits), `${language} ${key}`).not.toMatch(/\{(min|words|chars|bytes|days)\}/);
      }
    }
    const { t } = createI18n('en');
    expect(t('create.passphrase.hint', limits)).toContain(`${MIN_PASSPHRASE_CHARS} characters`);
    expect(t('create.passphrase.weak', limits)).toContain(`${WEAK_BELOW_WORDS} random words and ${WEAK_BELOW_CHARS} characters`);
    expect(t('report.problem.reason_too_long', limits)).toContain(`${MAX_REASON_BYTES} bytes`);
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
