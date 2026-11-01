import { describe, expect, it, vi } from 'vitest';

import { bytesToHex, hexToBytes, kdfIdOf, randomId, vectors } from '../../test/vectors.ts';
import { ShareError } from '../errors.ts';
import { KEY_LENGTH } from '../limits.ts';
import { encodeBase64url } from '../primitives/base64url.ts';
import { newSecret, parseId } from '../primitives/identifiers.ts';
import { requestKey } from '../request/signature.ts';
import { derivePassphraseKey } from '../scrypt.ts';
import { deriveContent, deriveLink, deriveVerifier, hsalt } from './derive.ts';

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

    const verifierSeed = await deriveVerifier(id, secret, passphraseKey);
    expect(bytesToHex(verifierSeed)).toBe(vector.v_hex);
    expect(await publicKeyOf(link.readSeed)).toBe(vector.read_key);
    expect(await publicKeyOf(hexToBytes(vector.m_hex))).toBe(vector.manage_key);
    expect(await publicKeyOf(verifierSeed)).toBe(vector.verifier_key);

    const content = await deriveContent(id, secret, passphraseKey, hexToBytes(vector.e_hex));
    expect(content.contentKey.extractable).toBe(false);
    expect(content.commitmentKey.extractable).toBe(false);
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
  it('derives K and K_c as non-extractable keys for their one use each', async () => {
    const { contentKey, commitmentKey } = await deriveContent(randomId(), newSecret(), new Uint8Array(0), newSecret());
    expect(contentKey.extractable).toBe(false);
    expect(contentKey.algorithm).toEqual({ name: 'AES-GCM', length: KEY_LENGTH * 8 });
    expect(contentKey.usages.toSorted((left, right) => left.localeCompare(right))).toEqual(['decrypt', 'encrypt']);
    expect(commitmentKey.extractable).toBe(false);
    expect(commitmentKey.algorithm).toMatchObject({ name: 'HMAC', hash: { name: 'SHA-256' } });
    expect(commitmentKey.usages.toSorted((left, right) => left.localeCompare(right))).toEqual(['sign', 'verify']);
    await expect(crypto.subtle.exportKey('raw', contentKey)).rejects.toThrow();
    await expect(crypto.subtle.exportKey('jwk', contentKey)).rejects.toThrow();
    const wrappingKey = await crypto.subtle.generateKey({ name: 'AES-KW', length: 256 }, false, ['wrapKey']);
    await expect(crypto.subtle.wrapKey('raw', contentKey, wrappingKey, 'AES-KW')).rejects.toThrow();
  });

  it('F3: needs the share', async () => {
    await expect(deriveContent(randomId(), newSecret(), new Uint8Array(0), undefined)).rejects.toBeInstanceOf(
      ShareError,
    );
  });

  it('rejects an id or a secret of the wrong length', async () => {
    await expect(hsalt(new Uint8Array(15))).rejects.toThrow();
    await expect(deriveLink(randomId(), new Uint8Array(31))).rejects.toThrow();
    await expect(deriveContent(new Uint8Array(17), newSecret(), new Uint8Array(0), newSecret())).rejects.toThrow();
  });

  it('wipes the copy of S, P and E it feeds into HKDF', async () => {
    const imported: Uint8Array[] = [];
    const importKey = crypto.subtle.importKey.bind(crypto.subtle);
    const spy = vi.spyOn(crypto.subtle, 'importKey').mockImplementation(((
      format: KeyFormat,
      data: BufferSource,
      ...rest: [AlgorithmIdentifier, boolean, KeyUsage[]]
    ) => {
      if (data instanceof Uint8Array && data.length === 96) {
        imported.push(data);
      }
      return importKey(format as 'raw', data, ...rest);
    }) as typeof crypto.subtle.importKey);
    try {
      await deriveContent(randomId(), newSecret(), new Uint8Array(32).fill(9), newSecret());
    } finally {
      spy.mockRestore();
    }
    expect(imported).toEqual([new Uint8Array(96)]);
  });

  it('wipes S and P even when their native import rejects', async () => {
    let imported: Uint8Array | undefined;
    const refuse = vi.spyOn(crypto.subtle, 'importKey').mockImplementation(((
      _format: KeyFormat,
      data: BufferSource,
    ) => {
      imported = data as Uint8Array;
      return Promise.reject(new DOMException('refused', 'OperationError'));
    }) as typeof crypto.subtle.importKey);
    try {
      await expect(deriveVerifier(randomId(), newSecret(), new Uint8Array(32).fill(9))).rejects.toMatchObject({
        name: 'OperationError',
      });
      expect(imported).toEqual(new Uint8Array(64));
    } finally {
      refuse.mockRestore();
    }
  });
});
