import { describe, expect, it } from 'vitest';

import { scryptPassphraseKey } from '../src/scrypt.ts';
import { bytesToHex, hexToBytes, loadVectors } from './helpers.ts';

describe('passphrase key derivation', () => {
  it('reports scrypt progress and matches the vector', () => {
    const vector = loadVectors().derive.find((v) => v.name === 'scrypt-ascii');
    if (!vector?.passphrase) {
      throw new Error('vector scrypt-ascii is missing');
    }
    const fractions: number[] = [];
    const key = scryptPassphraseKey(vector.passphrase, hexToBytes(vector.salt_pw_hex), (fraction) => {
      fractions.push(fraction);
    });
    expect(bytesToHex(key)).toBe(vector.p_hex);
    expect(fractions.length).toBeGreaterThan(1);
    expect(fractions.at(-1)).toBe(1);
  });
});
