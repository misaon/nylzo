import { describe, expect, it } from 'vitest';

import { KDF_SCRYPT, derivePassphraseKey } from '../src/index.ts';
import { bytesToHex, hexToBytes, loadVectors } from './helpers.ts';

describe('passphrase key derivation', () => {
  it('reports scrypt progress up to completion and still matches the vector', async () => {
    const vector = loadVectors().derive.find((v) => v.name === 'scrypt-ascii');
    if (!vector?.passphrase) {
      throw new Error('vector scrypt-ascii is missing');
    }
    const fractions: number[] = [];
    const key = await derivePassphraseKey(KDF_SCRYPT, vector.passphrase, hexToBytes(vector.salt_pw_hex), {
      onProgress: (fraction) => fractions.push(fraction),
    });
    expect(bytesToHex(key)).toBe(vector.p_hex);
    expect(fractions.length).toBeGreaterThan(1);
    expect(fractions.at(-1)).toBe(1);
  });
});
