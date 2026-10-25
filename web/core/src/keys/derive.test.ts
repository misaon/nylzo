import { describe, expect, it, vi } from 'vitest';

import { bytesToHex, hexToBytes, kdfIdOf, randomId, vectors } from '../../test/vectors.ts';
import { KEY_LENGTH } from '../limits.ts';
import { fragment } from '../link/link.ts';
import { encodeBase64url } from '../primitives/base64url.ts';
import { newSecret, parseId } from '../primitives/identifiers.ts';
import { requestKey } from '../request/signature.ts';
import { derivePassphraseKey } from '../scrypt.ts';
import { deriveContent, deriveLink, hsalt } from './derive.ts';
import { KDF_NONE } from './kdf.ts';

async function publicKeyOf(seed: Uint8Array): Promise<string> {
  const key = await requestKey(seed);
  return encodeBase64url(key.publicKey);
}

describe('AK9: derive vectors', () => {
  it.each(vectors.derive)('$name', { timeout: 120_000 }, async (vector) => {
    const id = parseId(vector.id);
    const secret = hexToBytes(vector.s);
    const kdfId = kdfIdOf(vector.kdf_id);
    expect(bytesToHex(await hsalt(id))).toBe(vector.hsalt_hex);

    const link = await deriveLink(id, secret);
    expect(bytesToHex(link.readSeed)).toBe(vector.r_hex);
    expect(bytesToHex(link.passphraseSalt)).toBe(vector.salt_pw_hex);

    const passphraseKey = derivePassphraseKey(kdfId, vector.passphrase ?? '', link.passphraseSalt);
    expect(bytesToHex(passphraseKey)).toBe(vector.p_hex);

    const content = await deriveContent(id, secret, passphraseKey);
    expect(bytesToHex(content.verifierSeed)).toBe(vector.v_hex);
    expect(await publicKeyOf(link.readSeed)).toBe(vector.read_key);
    expect(await publicKeyOf(hexToBytes(vector.m_hex))).toBe(vector.manage_key);
    expect(kdfId === KDF_NONE ? undefined : await publicKeyOf(content.verifierSeed)).toBe(vector.verifier_key);
    expect(fragment(kdfId, secret)).toBe(vector.fragment);
  });

  it('agree for the NFC composed and decomposed passphrase', () => {
    const composed = vectors.derive.find((vector) => vector.name === 'scrypt-nfc-composed');
    const decomposed = vectors.derive.find((vector) => vector.name === 'scrypt-nfc-decomposed');
    expect(composed).toBeDefined();
    expect(composed?.passphrase).not.toBe(decomposed?.passphrase);
    expect(composed?.p_hex).toBe(decomposed?.p_hex);
    expect(composed?.k_hex).toBe(decomposed?.k_hex);
  });
});

describe('key derivation (spec 2)', () => {
  it('derives K as a non-extractable AES-GCM key for encrypt and decrypt only', async () => {
    const { contentKey } = await deriveContent(randomId(), newSecret(), new Uint8Array(0));
    expect(contentKey.type).toBe('secret');
    expect(contentKey.extractable).toBe(false);
    expect(contentKey.algorithm).toEqual({ name: 'AES-GCM', length: KEY_LENGTH * 8 });
    expect(contentKey.usages.toSorted((left, right) => left.localeCompare(right))).toEqual(['decrypt', 'encrypt']);
    await expect(crypto.subtle.exportKey('raw', contentKey)).rejects.toThrow();
    await expect(crypto.subtle.exportKey('jwk', contentKey)).rejects.toThrow();
  });

  it('rejects an id or a secret of the wrong length', async () => {
    await expect(hsalt(new Uint8Array(15))).rejects.toThrow();
    await expect(deriveLink(randomId(), new Uint8Array(31))).rejects.toThrow();
    await expect(deriveContent(new Uint8Array(17), newSecret(), new Uint8Array(0))).rejects.toThrow();
  });

  it('wipes the copy of S and P it feeds into HKDF', async () => {
    const imported: Uint8Array[] = [];
    const importKey = crypto.subtle.importKey.bind(crypto.subtle);
    const spy = vi.spyOn(crypto.subtle, 'importKey').mockImplementation(((
      format: KeyFormat,
      data: BufferSource,
      ...rest: [AlgorithmIdentifier, boolean, KeyUsage[]]
    ) => {
      if (data instanceof Uint8Array && data.length === 64) {
        imported.push(data);
      }
      return importKey(format as 'raw', data, ...rest);
    }) as typeof crypto.subtle.importKey);
    try {
      await deriveContent(randomId(), newSecret(), new Uint8Array(32).fill(9));
    } finally {
      spy.mockRestore();
    }
    expect(imported).toEqual([new Uint8Array(64)]);
  });
});
