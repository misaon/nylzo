import { MIN_PASSPHRASE_CHARACTERS } from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import { WEAK_BELOW_CHARACTERS, WEAK_BELOW_WORDS } from '../../features/create/create-message.ts';
import { RETENTION_DAYS } from '../sent-records/sent-record.ts';
import { catalogOf } from './catalog.ts';
import type { MessageKey } from './catalog.ts';
import { LANGUAGES } from './language.ts';
import { Translator } from './translator.svelte.ts';

describe('messages', () => {
  it.each(LANGUAGES)('%s takes the limits it names from the code', (language) => {
    const limits = {
      min: MIN_PASSPHRASE_CHARACTERS,
      words: WEAK_BELOW_WORDS,
      characters: WEAK_BELOW_CHARACTERS,
      days: RETENTION_DAYS,
    };
    const { translate } = new Translator(language);
    const entries = Object.entries(catalogOf(language)).filter(([key]) => !key.startsWith('ttl.'));
    for (const [key, entry] of entries) {
      for (const text of typeof entry === 'string' ? [entry] : Object.values(entry)) {
        expect(text, `${language} ${key}`).not.toMatch(/\b(?:12|16|1000)\b|\b7 (?:days|dní|dnů)\b/u);
      }
    }
    const texts = entries.filter(([, entry]) => typeof entry === 'string');
    for (const [key] of texts) {
      expect(translate(key as MessageKey, limits), `${language} ${key}`).not.toMatch(
        /\{(?:min|words|characters|bytes|days)\}/u,
      );
    }
  });

  it('fills the limits into the English passphrase advice', () => {
    const limits = { min: MIN_PASSPHRASE_CHARACTERS, words: WEAK_BELOW_WORDS, characters: WEAK_BELOW_CHARACTERS };
    const { translate } = new Translator('en');
    expect(translate('compose.passphrase.placeholder', limits)).toContain(`${MIN_PASSPHRASE_CHARACTERS} characters`);
    expect(translate('create.passphrase.weak', limits)).toContain(
      `${WEAK_BELOW_WORDS} random words and ${WEAK_BELOW_CHARACTERS} characters`,
    );
  });
});
