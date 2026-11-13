import { describe, expect, it, vi } from 'vitest';

import { hexToBytes, randomId, vectors } from '../../test/vectors.ts';
import { ShareError } from '../errors.ts';
import { KEY_LENGTH } from '../limits.ts';
import { encodeBase64url } from '../primitives/base64url.ts';
import { newSecret, parseId } from '../primitives/identifiers.ts';
import { requestKey } from '../request/signature.ts';
import { deriveContent, deriveLink, derivePassphraseSalt, hsalt } from './derive.ts';

async function publicKeyOf(seed: Uint8Array): Promise<string> {
  const key = await requestKey(seed);
  return encodeBase64url(key.publicKey);
}

describe('AK9: derive vectors', () => {
  it.each(vectors.derive)('$name', async (vector) => {
    const id = parseId(vector.id);
    const secret = hexToBytes(vector.s);
    const salt = await hsalt(id);
    expect(salt.toHex()).toBe(vector.hsalt_hex);

    const link = await deriveLink(id, secret);
    expect(link.readSeed.toHex()).toBe(vector.r_hex);
    expect(link.passphraseSalt.toHex()).toBe(vector.salt_pw_hex);
    const passphraseSalt = await derivePassphraseSalt(secret);
    expect(passphraseSalt.toHex()).toBe(vector.salt_pw_hex);
    expect(await publicKeyOf(link.readSeed)).toBe(vector.read_key);
    expect(await publicKeyOf(hexToBytes(vector.m_hex))).toBe(vector.manage_key);

    const content = await deriveContent(id, secret, hexToBytes(vector.p_hex), hexToBytes(vector.e_hex));
    expect(content.contentKey.extractable).toBe(false);
    expect(content.commitmentKey.extractable).toBe(false);
  });
});

const exportKey = new Uint8Array(64).fill(7);

describe('key derivation (spec 2)', () => {
  it('derives K and K_c as non-extractable keys for their one use each', async () => {
    const { contentKey, commitmentKey } = await deriveContent(randomId(), newSecret(), exportKey, newSecret());
    expect(contentKey.extractable).toBe(false);
    expect(contentKey.algorithm).toEqual({ name: 'AES-GCM', length: KEY_LENGTH * 8 });
    expect(contentKey.usages.toSorted((left, right) => left.localeCompare(right))).toEqual(['decrypt', 'encrypt']);
    expect(commitmentKey.extractable).toBe(false);
    expect(commitmentKey.algorithm).toMatchObject({ name: 'HMAC', hash: { name: 'SHA-256' } });
    expect(commitmentKey.usages.toSorted((left, right) => left.localeCompare(right))).toEqual(['sign', 'verify']);
    await expect(crypto.subtle.exportKey('raw', contentKey)).rejects.toBeInstanceOf(Error);
    await expect(crypto.subtle.exportKey('jwk', contentKey)).rejects.toBeInstanceOf(Error);
    const wrappingKey = await crypto.subtle.generateKey({ name: 'AES-KW', length: 256 }, false, ['wrapKey']);
    await expect(crypto.subtle.wrapKey('raw', contentKey, wrappingKey, 'AES-KW')).rejects.toBeInstanceOf(Error);
  });

  it('F3: needs the share', async () => {
    await expect(deriveContent(randomId(), newSecret(), exportKey, undefined)).rejects.toBeInstanceOf(ShareError);
  });

  it('rejects an id or a secret of the wrong length', async () => {
    await expect(hsalt(new Uint8Array(15))).rejects.toBeInstanceOf(Error);
    await expect(deriveLink(randomId(), new Uint8Array(31))).rejects.toBeInstanceOf(Error);
    await expect(deriveContent(new Uint8Array(17), newSecret(), exportKey, newSecret())).rejects.toBeInstanceOf(Error);
    await expect(deriveContent(randomId(), newSecret(), new Uint8Array(32), newSecret())).rejects.toBeInstanceOf(Error);
  });

  it('wipes the copy of S, P and E it feeds into HKDF', async () => {
    const imported: Uint8Array[] = [];
    const importKey = crypto.subtle.importKey.bind(crypto.subtle);
    const spy = vi.spyOn(crypto.subtle, 'importKey').mockImplementation(((
      format: KeyFormat,
      data: BufferSource,
      ...rest: [AlgorithmIdentifier, boolean, KeyUsage[]]
    ) => {
      if (data instanceof Uint8Array && data.length === 128) {
        imported.push(data);
      }
      return importKey(format as 'raw', data, ...rest);
    }) as typeof crypto.subtle.importKey);
    try {
      await deriveContent(randomId(), newSecret(), new Uint8Array(64).fill(9), newSecret());
    } finally {
      spy.mockRestore();
    }
    expect(imported).toEqual([new Uint8Array(128)]);
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
      await expect(
        deriveContent(randomId(), newSecret(), new Uint8Array(64).fill(9), newSecret()),
      ).rejects.toMatchObject({
        name: 'OperationError',
      });
      expect(imported).toEqual(new Uint8Array(128));
    } finally {
      refuse.mockRestore();
    }
  });
});
