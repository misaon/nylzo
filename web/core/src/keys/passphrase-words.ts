export const PASSPHRASE_LANGUAGES = [
  'en',
  'cs',
  'de',
  'es',
  'fr',
  'id',
  'it',
  'ja',
  'pl',
  'pt-BR',
  'sk',
  'zh-Hans',
] as const;

export type PassphraseLanguage = (typeof PASSPHRASE_LANGUAGES)[number];

// Password fields turn input methods off, and these word lists cannot be typed without one.
export const INPUT_METHOD_LANGUAGES: ReadonlySet<PassphraseLanguage> = new Set(['ja', 'zh-Hans']);

export const WORD_LIST_LENGTH = 2048;

interface Layout {
  readonly separator: string;
  readonly count: number;
}

export interface PassphraseWords extends Layout {
  readonly words: readonly string[];
}

const DASHED: Layout = { separator: '-', count: 5 };

const LAYOUTS = {
  en: DASHED,
  cs: DASHED,
  de: DASHED,
  es: DASHED,
  fr: DASHED,
  id: DASHED,
  it: DASHED,
  ja: { separator: '\u{3000}', count: 5 },
  pl: DASHED,
  'pt-BR': DASHED,
  sk: DASHED,
  'zh-Hans': { separator: ' ', count: 6 },
} as const satisfies Readonly<Record<PassphraseLanguage, Layout>>;

const WORD_SOURCES = {
  en: async () => import('./words/en.ts'),
  cs: async () => import('./words/cs.ts'),
  de: async () => import('./words/de.ts'),
  es: async () => import('./words/es.ts'),
  fr: async () => import('./words/fr.ts'),
  id: async () => import('./words/id.ts'),
  it: async () => import('./words/it.ts'),
  ja: async () => import('./words/ja.ts'),
  pl: async () => import('./words/pl.ts'),
  'pt-BR': async () => import('./words/pt-br.ts'),
  sk: async () => import('./words/sk.ts'),
  'zh-Hans': async () => import('./words/zh-hans.ts'),
} satisfies Readonly<Record<PassphraseLanguage, () => Promise<{ readonly WORDS: string }>>>;

// Only the language a sender uses loads, so the page does not carry all twelve lists.
export async function loadPassphraseWords(language: PassphraseLanguage): Promise<PassphraseWords> {
  const { WORDS } = await WORD_SOURCES[language]();
  const words = WORDS.split(' ');
  if (words.length !== WORD_LIST_LENGTH) {
    throw new Error(`the ${language} word list does not hold ${WORD_LIST_LENGTH} words`);
  }
  return { words, ...LAYOUTS[language] };
}
