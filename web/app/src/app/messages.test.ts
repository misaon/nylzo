import { MIN_PASSPHRASE_CHARS } from '@nylzo/core';
import { describe, expect, it } from 'vitest';

import { WEAK_BELOW_CHARS, WEAK_BELOW_WORDS } from '../features/create/create-message.ts';
import { czech, type MessageKey } from '../shared/language/czech.ts';
import { english } from '../shared/language/english.ts';
import { Translator } from '../shared/language/translator.svelte.ts';
import { MAX_REASON_BYTES } from '../shared/report/report.ts';
import { RETENTION_DAYS } from '../shared/sent-records/sent-record.ts';

describe('messages', () => {
  it('takes the limits it names from the code', () => {
    const limits = {
      min: MIN_PASSPHRASE_CHARS,
      words: WEAK_BELOW_WORDS,
      chars: WEAK_BELOW_CHARS,
      bytes: MAX_REASON_BYTES,
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
          /\{(?:min|words|chars|bytes|days)\}/,
        );
      }
    }
    const { translate } = new Translator('en');
    expect(translate('create.passphrase.hint', limits)).toContain(`${MIN_PASSPHRASE_CHARS} characters`);
    expect(translate('create.passphrase.weak', limits)).toContain(
      `${WEAK_BELOW_WORDS} random words and ${WEAK_BELOW_CHARS} characters`,
    );
    expect(translate('report.problem.reason_too_long', limits)).toContain(`${MAX_REASON_BYTES} bytes`);
  });
});
