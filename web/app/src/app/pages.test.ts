import { describe, expect, it } from 'vitest';

import { HOME_PATHS, homeLanguage, screenOf } from './pages.ts';

describe('pages', () => {
  it('gives each language of the home page its own address (D82)', () => {
    expect(homeLanguage('/')).toBe('en');
    expect(homeLanguage('/cs/')).toBe('cs');
    expect(homeLanguage('/cs')).toBeUndefined();
    expect(homeLanguage('/sent')).toBeUndefined();
    expect(HOME_PATHS).toEqual({ en: '/', cs: '/cs/' });
  });

  it('chooses the screen by path', () => {
    expect(screenOf('/')).toBe('create');
    expect(screenOf('/cs/')).toBe('create');
    expect(screenOf('/sent')).toBe('sent');
    expect(screenOf('/report')).toBe('report');
    expect(screenOf('/transparency')).toBe('transparency');
    expect(screenOf('/m/AAAAAAAAAAAAAAAAAAAAAA')).toBe('read');
  });
});
