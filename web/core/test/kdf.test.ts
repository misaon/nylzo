import { describe, expect, it } from 'vitest';

import { KDF_SCRYPT, derivePassphraseKey } from '../src/index.ts';
import { bytesToHex, hexToBytes, loadVectors } from './helpers.ts';

describe('passphrase key derivation', () => {
  it.each([false, true])('reports scrypt progress and matches the vector with blocking %s', async (blocking) => {
    const vector = loadVectors().derive.find((v) => v.name === 'scrypt-ascii');
    if (!vector?.passphrase) {
      throw new Error('vector scrypt-ascii is missing');
    }
    const fractions: number[] = [];
    const key = await derivePassphraseKey(KDF_SCRYPT, vector.passphrase, hexToBytes(vector.salt_pw_hex), {
      blocking,
      onProgress: (fraction) => fractions.push(fraction),
    });
    expect(bytesToHex(key)).toBe(vector.p_hex);
    expect(fractions.length).toBeGreaterThan(1);
    expect(fractions.at(-1)).toBe(1);
  });
});
