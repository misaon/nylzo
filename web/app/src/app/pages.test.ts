import { describe, expect, it } from 'vitest';

import { homePath, LANGUAGES } from '../shared/language/language.ts';
import { homeLanguage, screenOf } from './pages.ts';

describe('pages', () => {
  it('gives each language of the home page its own address', () => {
    expect(homeLanguage('/')).toBe('en');
    expect(homeLanguage('/cs/')).toBe('cs');
    expect(homeLanguage('/cs')).toBeUndefined();
    expect(homeLanguage('/sent')).toBeUndefined();
    expect(homeLanguage('/pt-br/')).toBe('pt-BR');
    expect(homeLanguage('/zh-hans/')).toBe('zh-Hans');
    expect(new Set(LANGUAGES.map((language) => homePath(language))).size).toBe(LANGUAGES.length);
  });

  it.each([
    ['/', 'create'],
    ['/de/', 'create'],
    ['/sent', 'sent'],
    ['/agents', 'agents'],
    ['/open', 'read'],
    ['/r', 'read'],
    ['/m/AAAAAAAAAAAAAAAAAAAAAA', 'read'],
    ['/m/not-an-id', 'notFound'],
    ['/nothing', 'notFound'],
  ] as const)('shows %s as the %s screen', (path, screen) => {
    expect(screenOf(path)).toBe(screen);
  });
});
