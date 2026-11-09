import type { MessageKey } from './czech.ts';
import type { Language } from './language.ts';

type Catalog = Readonly<Record<MessageKey, string>>;

const CATALOG_SOURCES = {
  cs: async () => import('./czech.ts').then(({ czech }) => czech),
  en: async () => import('./english.ts').then(({ english }) => english),
} satisfies Record<Language, () => Promise<Catalog>>;
const catalogs = new Map<Language, Catalog>();

// The language a page opens in loads with it; another loads only when the reader switches to it.
export async function loadCatalog(language: Language): Promise<void> {
  if (!catalogs.has(language)) {
    catalogs.set(language, await CATALOG_SOURCES[language]());
  }
}

export function catalogOf(language: Language): Catalog {
  const catalog = catalogs.get(language);
  if (!catalog) {
    throw new Error(`the ${language} messages are not loaded`);
  }
  return catalog;
}
