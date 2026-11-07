import { isReadPath } from '@nylzo/core';
import type { Component } from 'svelte';

import { LANGUAGES } from '../shared/language/language.ts';
import type { Language } from '../shared/language/language.ts';
import { HANDOFF_PATH } from '../shared/local-client/handoff.ts';

export type Screen = 'agents' | 'create' | 'notFound' | 'read' | 'sent' | 'terms' | 'transparency';

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
  if (homeLanguage(path)) {
    return 'create';
  }
  return PAGES.get(path) ?? (isReadPath(path) ? 'read' : 'notFound');
}

export const SCREENS = {
  agents: async () => import('../features/agents/Agents.svelte'),
  create: async () => import('../features/create/Create.svelte'),
  notFound: async () => import('../features/errors/NotFound.svelte'),
  read: async () => import('../features/read/Read.svelte'),
  sent: async () => import('../features/sent/Sent.svelte'),
  terms: async () => import('../features/terms/Terms.svelte'),
  transparency: async () => import('../features/transparency/Transparency.svelte'),
} satisfies Record<Screen, () => Promise<{ readonly default: Component }>>;
