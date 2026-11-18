import { describe, expect, it } from 'vitest';

import { hexToBytes, sha256Hex, vectors } from '../../test/vectors.ts';
import { WeakPassphraseError } from '../errors.ts';
import { utf8 } from '../primitives/bytes.ts';
import { COMMON_PASSPHRASES } from './common-passphrases.ts';
import { loadPassphraseWords, PASSPHRASE_LANGUAGES } from './passphrase-words.ts';
import type { PassphraseLanguage } from './passphrase-words.ts';
import {
  assertStrongPassphrase,
  ASSIGNED_UNICODE_VERSION,
  assignedCodePointRanges,
  generatePassphrase,
  passphraseFrom,
} from './passphrase.ts';

function upperHex(codePoint: number): string {
  return codePoint.toString(16).toUpperCase();
}

describe('AK9: passphrase strength vectors', () => {
  const accepted = vectors.passphrase_strength.filter((vector) => vector.accepted);
  const rejected = vectors.passphrase_strength.filter((vector) => !vector.accepted);

  it.each(accepted)('$name is accepted', (vector) => {
    expect(() => {
      assertStrongPassphrase(vector.passphrase);
    }).not.toThrow();
  });

  it.each(rejected)('$name is rejected as $problem', (vector) => {
    expect(() => {
      assertStrongPassphrase(vector.passphrase);
    }).toThrow(expect.objectContaining({ name: WeakPassphraseError.name, problem: vector.problem }));
  });

  it('uses the same list of common passwords as the Go core', async () => {
    const list = utf8(`${COMMON_PASSPHRASES.join('\n')}\n`);
    expect(await sha256Hex(list)).toBe(vectors.passphrase_list_sha256);
  });

  it('A1: uses the same table of code points assigned in Unicode 15.1 as the Go core', async () => {
    const file = assignedCodePointRanges()
      .map(([first, last]) => `${upperHex(first)} ${upperHex(last)}\n`)
      .join('');
    expect(await sha256Hex(utf8(file))).toBe(vectors.unicode_assigned_sha256);
    expect(ASSIGNED_UNICODE_VERSION).toBe(vectors.unicode_assigned_version);
  });
});

function languageOf(tag: string): PassphraseLanguage {
  const language = PASSPHRASE_LANGUAGES.find((candidate) => candidate === tag);
  if (language === undefined) {
    throw new Error(`vector names unknown language ${tag}`);
  }
  return language;
}

function bytesInOrder(hex: string): (length: number) => Uint8Array {
  const bytes = hexToBytes(hex);
  let offset = 0;
  return (length) => {
    const part = bytes.slice(offset, offset + length);
    offset += length;
    if (part.length !== length) {
      throw new Error('the vector ran out of random bytes');
    }
    return part;
  };
}

describe('passphrase word list vectors', () => {
  it('cover every language', () => {
    const languages = vectors.passphrase_words.map((vector) => vector.language);
    expect(languages).toEqual(PASSPHRASE_LANGUAGES);
  });

  it.each(vectors.passphrase_words)('$language uses the same list and layout as the Go core', async (vector) => {
    const list = await loadPassphraseWords(languageOf(vector.language));
    const text = utf8(`${list.words.join('\n')}\n`);
    expect(await sha256Hex(text)).toBe(vector.sha256);
    expect({ separator: list.separator, count: list.count }).toEqual({
      separator: vector.separator,
      count: vector.words,
    });
  });

  it.each(vectors.generated_passphrase)('$name gives the vector passphrase', async (vector) => {
    const list = await loadPassphraseWords(languageOf(vector.language));
    expect(passphraseFrom(list, bytesInOrder(vector.random_hex))).toBe(vector.passphrase);
  });
});

describe('generated passphrases', () => {
  it.each(PASSPHRASE_LANGUAGES)('in %s are distinct words of the list and pass every check', async (language) => {
    const list = await loadPassphraseWords(language);
    const seen = new Set<string>();
    for (let round = 0; round < 100; round++) {
      const passphrase = await generatePassphrase(language);
      const chosen = passphrase.split(list.separator);
      expect(chosen).toHaveLength(list.count);
      expect(chosen.every((word) => list.words.includes(word))).toBe(true);
      expect(() => {
        assertStrongPassphrase(passphrase);
      }).not.toThrow();
      expect(seen.has(passphrase)).toBe(false);
      seen.add(passphrase);
    }
  });
});
