import { createContext } from 'svelte';

import { catalogOf } from './catalog.ts';
import type { MessageKey, PluralKey } from './catalog.ts';
import { DEFAULT_LANGUAGE } from './language.ts';
import type { Language } from './language.ts';

const PLACEHOLDER = /\{(\w+)\}/gu;

type Values = Readonly<Record<string, number | string>>;

export type Translate = (key: MessageKey, values?: Values) => string;

export type Plural = (key: PluralKey, count: number, values?: Values) => string;

const pluralRules = new Map<Language, Intl.PluralRules>();
const numberFormats = new Map<Language, Intl.NumberFormat>();

function cached<Format>(formats: Map<Language, Format>, language: Language, create: () => Format): Format {
  let format = formats.get(language);
  if (format === undefined) {
    format = create();
    formats.set(language, format);
  }
  return format;
}

function fill(text: string, values: Values): string {
  return text.replaceAll(PLACEHOLDER, (placeholder, name: string) =>
    Object.hasOwn(values, name) ? String(values[name]) : placeholder,
  );
}

export class Translator {
  language = $state<Language>(DEFAULT_LANGUAGE);

  readonly translate: Translate = (key, values = {}) => fill(catalogOf(this.language)[key], values);

  readonly plural: Plural = (key, count, values = {}) => {
    const { language } = this;
    const forms = catalogOf(language)[key];
    const rule = cached(pluralRules, language, () => new Intl.PluralRules(language)).select(count);
    const formatted = cached(numberFormats, language, () => new Intl.NumberFormat(language)).format(count);
    return fill(forms[rule] ?? forms.other, { ...values, count: formatted });
  };

  constructor(language: Language) {
    this.language = language;
  }
}

export const [getTranslator, setTranslator] = createContext<Translator>();
