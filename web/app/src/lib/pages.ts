import type { Language } from './i18n.svelte.ts';

export type Screen = 'create' | 'sent' | 'report' | 'transparency' | 'read';

export const HOME_PATHS: Readonly<Record<Language, string>> = { en: '/', cs: '/cs/' };

export function homeLanguage(path: string): Language | undefined {
  return path === HOME_PATHS.en ? 'en' : path === HOME_PATHS.cs ? 'cs' : undefined;
}

const PAGES: ReadonlyMap<string, Screen> = new Map([
  ['/sent', 'sent'],
  ['/report', 'report'],
  ['/transparency', 'transparency'],
]);

export function screenOf(path: string): Screen {
  return PAGES.get(path) ?? (path.startsWith('/m/') ? 'read' : 'create');
}
