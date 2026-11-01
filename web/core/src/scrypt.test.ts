import { scryptAsync } from '@noble/hashes/scrypt.js';
import { describe, expect, it } from 'vitest';

import { bytesToHex, randomId, vectors } from '../test/vectors.ts';
import { MalformedPassphraseError } from './errors.ts';
import { KDF_NONE, KDF_SCRYPT } from './keys/kdf.ts';
import { utf8 } from './primitives/bytes.ts';
import { newSecret } from './primitives/identifiers.ts';
import { deriveAll, derivePassphraseKey, scryptPassphraseKey } from './scrypt.ts';

const salt = new Uint8Array(32);
const loneSurrogate = 'caf\u{D800}';
const heavyTestsEnabled = process.env['NYLZO_HEAVY_TESTS'] === '1';

describe('AK9: scrypt vectors of RFC 7914 for the pinned @noble/hashes', () => {
  const light = vectors.scrypt_rfc7914.filter((vector) => !vector.heavy);
  const heavy = vectors.scrypt_rfc7914.filter((vector) => vector.heavy);
  const derivedHex = async (vector: (typeof light)[number]): Promise<string> => {
    const parameters = { N: vector.N, r: vector.r, p: vector.p, dkLen: vector.dkLen };
    return bytesToHex(await scryptAsync(utf8(vector.password), utf8(vector.salt), parameters));
  };

  it.each(light)('$name', async (vector) => {
    expect(await derivedHex(vector)).toBe(vector.dk_hex);
  });

  it.runIf(heavyTestsEnabled).each(heavy)('$name', { timeout: 600_000 }, async (vector) => {
    expect(await derivedHex(vector)).toBe(vector.dk_hex);
  });
});

describe('passphrase key P (spec 2)', () => {
  it('rejects a passphrase that is not well-formed Unicode before deriving anything', async () => {
    expect(() => scryptPassphraseKey(loneSurrogate, salt)).toThrow(MalformedPassphraseError);
    expect(() => derivePassphraseKey(KDF_SCRYPT, loneSurrogate, salt)).toThrow(MalformedPassphraseError);
    await expect(deriveAll(randomId(), newSecret(), KDF_SCRYPT, loneSurrogate, newSecret())).rejects.toBeInstanceOf(
      MalformedPassphraseError,
    );
  });

  it('is empty for profile 0 whatever the passphrase', async () => {
    expect(derivePassphraseKey(KDF_NONE, loneSurrogate, salt)).toHaveLength(0);
    const id = randomId();
    const secret = newSecret();
    const share = newSecret();
    const first = await deriveAll(id, secret, KDF_NONE, 'first', share);
    const second = await deriveAll(id, secret, KDF_NONE, 'second', share);
    expect(first.verifierSeed).toEqual(second.verifierSeed);
  });
});
