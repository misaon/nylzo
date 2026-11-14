import { scryptAsync } from '@noble/hashes/scrypt.js';
import { describe, expect, it } from 'vitest';

import { vectors } from '../test/vectors.ts';
import { LongPassphraseError, MalformedPassphraseError } from './errors.ts';
import { KDF_NONE, KDF_SCRYPT, opaquePassword } from './keys/kdf.ts';
import { MAX_PASSPHRASE_BYTES } from './limits.ts';
import { utf8 } from './primitives/bytes.ts';

const salt = new Uint8Array(32).fill(5);
const loneSurrogate = 'caf\u{D800}';
const heavyTestsEnabled = process.env['NYLZO_HEAVY_TESTS'] === '1';

describe('AK9: scrypt vectors of RFC 7914 for the pinned @noble/hashes', () => {
  const light = vectors.scrypt_rfc7914.filter((vector) => !vector.heavy);
  const heavy = vectors.scrypt_rfc7914.filter((vector) => vector.heavy);
  const derivedHex = async (vector: (typeof light)[number]): Promise<string> => {
    const parameters = { N: vector.N, r: vector.r, p: vector.p, dkLen: vector.dkLen };
    const derived = await scryptAsync(utf8(vector.password), utf8(vector.salt), parameters);
    return derived.toHex();
  };

  it.each(light)('$name', async (vector) => {
    expect(await derivedHex(vector)).toBe(vector.dk_hex);
  });

  it.runIf(heavyTestsEnabled).each(heavy)('$name', { timeout: 600_000 }, async (vector) => {
    expect(await derivedHex(vector)).toBe(vector.dk_hex);
  });
});

describe('OPAQUE password (spec 2.1)', () => {
  it('rejects a passphrase that is not well-formed Unicode before deriving anything', () => {
    expect(() => opaquePassword(salt, KDF_SCRYPT, loneSurrogate)).toThrow(MalformedPassphraseError);
  });

  it('counts the passphrase limit after NFC, as the Go core does', () => {
    const decomposed = `${'e\u0301'.repeat(Math.floor(MAX_PASSPHRASE_BYTES / 2))}x`;
    expect(opaquePassword(salt, KDF_SCRYPT, decomposed)).toHaveLength(salt.length + MAX_PASSPHRASE_BYTES);
    expect(() => opaquePassword(salt, KDF_SCRYPT, `${decomposed}x`)).toThrow(LongPassphraseError);
  });

  it('is salt_pw alone for profile 0 whatever the passphrase', () => {
    expect(opaquePassword(salt, KDF_NONE, loneSurrogate)).toEqual(salt);
    expect(opaquePassword(salt, KDF_NONE, 'other')).toEqual(salt);
  });
});
