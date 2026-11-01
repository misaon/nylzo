import { describe, expect, it } from 'vitest';

import { sha256Hex, vectors } from '../../test/vectors.ts';
import { WeakPassphraseError } from '../errors.ts';
import { utf8 } from '../primitives/bytes.ts';
import { COMMON_PASSPHRASES } from './common-passphrases.ts';
import {
  assertStrongPassphrase,
  ASSIGNED_UNICODE_VERSION,
  assignedCodePointRanges,
  generatePassphrase,
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

describe('generated passphrases', () => {
  it('are well formed, unique and pass every check', () => {
    const seen = new Set<string>();
    for (let round = 0; round < 500; round++) {
      const passphrase = generatePassphrase();
      expect(passphrase).toMatch(/^[0-9a-hjkmnp-tv-z]{4}(?:-[0-9a-hjkmnp-tv-z]{4}){3}$/u);
      expect(() => {
        assertStrongPassphrase(passphrase);
      }).not.toThrow();
      expect(seen.has(passphrase)).toBe(false);
      seen.add(passphrase);
    }
  });
});
