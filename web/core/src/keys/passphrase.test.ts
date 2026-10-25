import { describe, expect, it } from 'vitest';

import { sha256Hex, vectors } from '../../test/vectors.ts';
import { WeakPassphraseError } from '../errors.ts';
import { utf8 } from '../primitives/bytes.ts';
import { COMMON_PASSPHRASES } from './common-passphrases.ts';
import { assertStrongPassphrase, generatePassphrase } from './passphrase.ts';

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
    expect(await sha256Hex(utf8(`${COMMON_PASSPHRASES.join('\n')}\n`))).toBe(vectors.passphrase_list_sha256);
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
