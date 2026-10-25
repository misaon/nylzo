import { type Language, LANGUAGES } from '../shared/language/language.ts';

export type Screen = 'create' | 'read' | 'report' | 'sent' | 'transparency';

export const HOME_PATHS: Readonly<Record<Language, string>> = { en: '/', cs: '/cs/' };

const PAGES: ReadonlyMap<string, Screen> = new Map([
  ['/sent', 'sent'],
  ['/report', 'report'],
  ['/transparency', 'transparency'],
]);

export function homeLanguage(path: string): Language | undefined {
  return LANGUAGES.find((language) => HOME_PATHS[language] === path);
}

export function screenOf(path: string): Screen {
  return PAGES.get(path) ?? (path.startsWith('/m/') ? 'read' : 'create');
}
