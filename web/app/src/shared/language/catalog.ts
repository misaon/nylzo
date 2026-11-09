import type { Language } from './language.ts';
import type { messages as czech } from './messages/cs.ts';

export type PluralForms = Readonly<Partial<Record<Intl.LDMLPluralRule, string>>> & { readonly other: string };

export type CatalogShape = Readonly<Record<string, PluralForms | string>>;

type Source = typeof czech;

export type MessageKey = { [Key in keyof Source]: Source[Key] extends string ? Key : never }[keyof Source];

export type PluralKey = Exclude<keyof Source, MessageKey>;

export type Catalog = Readonly<Record<MessageKey, string>> & Readonly<Record<PluralKey, PluralForms>>;

const CATALOG_SOURCES = {
  en: async () => import('./messages/en.ts'),
  cs: async () => import('./messages/cs.ts'),
  de: async () => import('./messages/de.ts'),
  es: async () => import('./messages/es.ts'),
  fr: async () => import('./messages/fr.ts'),
  id: async () => import('./messages/id.ts'),
  it: async () => import('./messages/it.ts'),
  ja: async () => import('./messages/ja.ts'),
  pl: async () => import('./messages/pl.ts'),
  'pt-BR': async () => import('./messages/pt-br.ts'),
  sk: async () => import('./messages/sk.ts'),
  'zh-Hans': async () => import('./messages/zh-hans.ts'),
} satisfies Readonly<Record<Language, () => Promise<{ readonly messages: Catalog }>>>;

const catalogs = new Map<Language, Catalog>();

// The language a page opens in loads with it; another loads only when the reader switches to it.
export async function loadCatalog(language: Language): Promise<void> {
  if (!catalogs.has(language)) {
    const { messages } = await CATALOG_SOURCES[language]();
    catalogs.set(language, messages);
  }
}

export function catalogOf(language: Language): Catalog {
  const catalog = catalogs.get(language);
  if (!catalog) {
    throw new Error(`the ${language} messages are not loaded`);
  }
  return catalog;
}
