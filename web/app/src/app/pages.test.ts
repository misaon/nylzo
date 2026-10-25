import { describe, expect, it } from 'vitest';

import { HOME_PATHS, homeLanguage } from './pages.ts';

describe('pages', () => {
  it('gives each language of the home page its own address (D82)', () => {
    expect(homeLanguage('/')).toBe('en');
    expect(homeLanguage('/cs/')).toBe('cs');
    expect(homeLanguage('/cs')).toBeUndefined();
    expect(homeLanguage('/sent')).toBeUndefined();
    expect(HOME_PATHS).toEqual({ en: '/', cs: '/cs/' });
  });
});
