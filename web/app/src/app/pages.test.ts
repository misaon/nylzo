import { describe, expect, it } from 'vitest';

import { homePath, LANGUAGES } from '../shared/language/language.ts';
import { homeLanguage } from './pages.ts';

describe('pages', () => {
  it('gives each language of the home page its own address (D82)', () => {
    expect(homeLanguage('/')).toBe('en');
    expect(homeLanguage('/cs/')).toBe('cs');
    expect(homeLanguage('/cs')).toBeUndefined();
    expect(homeLanguage('/sent')).toBeUndefined();
    expect(homeLanguage('/pt-br/')).toBe('pt-BR');
    expect(homeLanguage('/zh-hans/')).toBe('zh-Hans');
    expect(new Set(LANGUAGES.map((language) => homePath(language))).size).toBe(LANGUAGES.length);
  });
});
