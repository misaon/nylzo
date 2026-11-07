import { createContext } from 'svelte';

import type { MessageKey } from './czech.ts';
import type { Language } from './language.ts';

export type Catalog = Readonly<Record<MessageKey, string>>;

const CATALOG_SOURCES = {
  cs: async () => import('./czech.ts').then(({ czech }) => czech),
  en: async () => import('./english.ts').then(({ english }) => english),
} satisfies Record<Language, () => Promise<Catalog>>;
const catalogs = new Map<Language, Catalog>();
const PLACEHOLDER = /\{(\w+)\}/gu;

export function addCatalog(language: Language, catalog: Catalog): void {
  catalogs.set(language, catalog);
}

// Each visitor downloads only the language they read; the other one loads before a switch.
export async function loadCatalog(language: Language): Promise<void> {
  if (!catalogs.has(language)) {
    catalogs.set(language, await CATALOG_SOURCES[language]());
  }
}

function catalogOf(language: Language): Catalog {
  const catalog = catalogs.get(language);
  if (!catalog) {
    throw new Error(`the ${language} messages are not loaded`);
  }
  return catalog;
}

export type Translate = (key: MessageKey, values?: Readonly<Record<string, number | string>>) => string;

export class Translator {
  language = $state<Language>('en');

  readonly translate: Translate = (key, values = {}) =>
    catalogOf(this.language)[key].replaceAll(PLACEHOLDER, (placeholder, name: string) =>
      Object.hasOwn(values, name) ? String(values[name]) : placeholder,
    );

  constructor(language: Language) {
    this.language = language;
  }
}

export const [getTranslator, setTranslator] = createContext<Translator>();
