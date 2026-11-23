import { isReadPath } from '@nylzo/core';

import type { MessageKey } from '../shared/language/catalog.ts';
import { homePath, LANGUAGES } from '../shared/language/language.ts';
import type { Language } from '../shared/language/language.ts';
import { HANDOFF_PATH } from '../shared/local-client/handoff.ts';

export type Screen = 'agents' | 'create' | 'notFound' | 'read' | 'sent' | 'terms' | 'transparency';

export const SCREEN_TITLES = {
  agents: 'title.agents',
  create: 'seo.title',
  notFound: 'title.notFound',
  read: 'title.read',
  sent: 'title.sent',
  terms: 'title.terms',
  transparency: 'title.transparency',
} as const satisfies Record<Screen, MessageKey>;

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
