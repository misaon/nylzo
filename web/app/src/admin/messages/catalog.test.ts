import { afterEach, describe, expect, it, vi } from 'vitest';

import { adminLanguage, adminTranslator } from './catalog.ts';

describe('admin catalog', () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it.each([
    [['cs-CZ', 'en'], 'cs'],
    [['CS'], 'cs'],
    [['en-US', 'cs'], 'en'],
    [['de'], 'en'],
    [[], 'en'],
  ])('picks the language for the browser languages %j', (languages, language) => {
    expect(adminLanguage(languages)).toBe(language);
  });

  it('follows the first browser language by default', () => {
    vi.stubGlobal('navigator', { languages: ['cs-CZ'] });

    expect(adminLanguage()).toBe('cs');
    expect(adminTranslator().language).toBe('cs');
  });

  it('fills the placeholders it is given and leaves the others visible', () => {
    const { translate } = adminTranslator('en');

    expect(translate('access.expires', { time: '14:41' })).toContain('14:41');
    expect(translate('access.expires', { time: '14:41' })).not.toContain('{time}');
    expect(translate('access.expires')).toContain('{time}');
    expect(translate('access.expires', { other: 1 })).toContain('{time}');
  });
});
