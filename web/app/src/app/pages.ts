import { isReadPath } from '@nylzo/core';

import { LANGUAGES } from '../shared/language/language.ts';
import type { Language } from '../shared/language/language.ts';
import { HANDOFF_PATH } from '../shared/local-client/handoff.ts';

export type Screen = 'agents' | 'create' | 'read' | 'sent' | 'terms' | 'transparency';

export const HOME_PATHS: Readonly<Record<Language, string>> = { en: '/', cs: '/cs/' };

const PAGES: ReadonlyMap<string, Screen> = new Map([
  ['/sent', 'sent'],
  ['/transparency', 'transparency'],
  ['/terms', 'terms'],
  ['/agents', 'agents'],
  [HANDOFF_PATH, 'read'],
]);

export function homeLanguage(path: string): Language | undefined {
  return LANGUAGES.find((language) => HOME_PATHS[language] === path);
}

export function screenOf(path: string): Screen {
  return PAGES.get(path) ?? (isReadPath(path) ? 'read' : 'create');
}
