import { isReadPath } from '@nylzo/core';

import { homePath, LANGUAGES } from '../shared/language/language.ts';
import type { Language } from '../shared/language/language.ts';
import { HANDOFF_PATH } from '../shared/local-client/handoff.ts';

export type Screen = 'agents' | 'create' | 'notFound' | 'read' | 'sent' | 'terms' | 'transparency';

const PAGES: ReadonlyMap<string, Screen> = new Map([
  ['/sent', 'sent'],
  ['/transparency', 'transparency'],
  ['/terms', 'terms'],
  ['/agents', 'agents'],
  [HANDOFF_PATH, 'read'],
]);

export function homeLanguage(path: string): Language | undefined {
  return LANGUAGES.find((language) => homePath(language) === path);
}

export function screenOf(path: string): Screen {
  if (homeLanguage(path)) {
    return 'create';
  }
  return PAGES.get(path) ?? (isReadPath(path) ? 'read' : 'notFound');
}
