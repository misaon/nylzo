import { describe, expect, it } from 'vitest';

import { formatBytes, formatDuration } from '../src/lib/format.ts';
import { createI18n, dictionaries, pickLanguage, type MessageKey } from '../src/lib/i18n.ts';

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
});
