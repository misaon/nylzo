import { createContext } from 'svelte';

import { catalogOf } from './catalogs.ts';
import type { MessageKey } from './czech.ts';
import type { Language } from './language.ts';

const PLACEHOLDER = /\{(\w+)\}/gu;

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
