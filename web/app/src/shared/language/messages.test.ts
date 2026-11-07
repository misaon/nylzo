import { MIN_PASSPHRASE_CHARACTERS } from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import { WEAK_BELOW_CHARACTERS, WEAK_BELOW_WORDS } from '../../features/create/create-message.ts';
import { RETENTION_DAYS } from '../sent-records/sent-record.ts';
import { czech } from './czech.ts';
import type { MessageKey } from './czech.ts';
import { english } from './english.ts';
import { Translator } from './translator.svelte.ts';

describe('messages', () => {
  it('takes the limits it names from the code', () => {
    const limits = {
      min: MIN_PASSPHRASE_CHARACTERS,
      words: WEAK_BELOW_WORDS,
      characters: WEAK_BELOW_CHARACTERS,
      days: RETENTION_DAYS,
    };
    for (const [language, messages] of [
      ['cs', czech],
      ['en', english],
    ] as const) {
      const { translate } = new Translator(language);
      for (const [key, text] of Object.entries(messages)) {
        if (key.startsWith('ttl.')) {
          continue;
        }
        expect(text, `${language} ${key}`).not.toMatch(/\b(?:12|16|1000)\b|\b7 (?:days|dní|dnů)\b/u);
        expect(translate(key as MessageKey, limits), `${language} ${key}`).not.toMatch(
          /\{(?:min|words|characters|bytes|days)\}/u,
        );
      }
    }
    const { translate } = new Translator('en');
    expect(translate('compose.passphrase.placeholder', limits)).toContain(`${MIN_PASSPHRASE_CHARACTERS} characters`);
    expect(translate('create.passphrase.weak', limits)).toContain(
      `${WEAK_BELOW_WORDS} random words and ${WEAK_BELOW_CHARACTERS} characters`,
    );
  });
});
