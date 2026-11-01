import { describe, expect, it } from 'vitest';

import { bytesToHex, hexToBytes, vectors } from '../../test/vectors.ts';
import { IntegrityError } from '../errors.ts';
import { decodeBase64url, encodeBase64url } from '../primitives/base64url.ts';
import { newShareKey, parseWrappedShare, type ShareKey, unwrapShare } from './share.ts';

async function vectorKey(privateHex: string, publicText: string): Promise<ShareKey> {
  const jwk: JsonWebKey = {
    kty: 'OKP',
    crv: 'X25519',
    d: encodeBase64url(hexToBytes(privateHex)),
    x: publicText,
  };
  const privateKey = await crypto.subtle.importKey('jwk', jwk, { name: 'X25519' }, false, ['deriveBits']);
  return { privateKey, publicKey: decodeBase64url(publicText) };
}

describe('AK9: share vectors (spec 5.6)', () => {
  it.each(vectors.share.filter((vector) => vector.valid))('$name unwraps to E', async (vector) => {
    const key = await vectorKey(vector.client_private_hex, vector.client_public);
    const share = await unwrapShare(key, parseWrappedShare(vector.wrapped));
    expect(bytesToHex(share)).toBe(vector.e_hex);
  });

  it.each(vectors.share.filter((vector) => !vector.valid))('$name is refused', async (vector) => {
    const key = await vectorKey(vector.client_private_hex, vector.client_public);
    await expect(unwrapShare(key, parseWrappedShare(vector.wrapped))).rejects.toBeInstanceOf(IntegrityError);
  });
});

describe('share keys', () => {
  it('are one-time X25519 keys whose private half never leaves Web Crypto', async () => {
    const first = await newShareKey();
    const second = await newShareKey();
    expect(first.privateKey.extractable).toBe(false);
    expect(first.publicKey).toHaveLength(32);
    expect(first.publicKey).not.toEqual(second.publicKey);
    await expect(crypto.subtle.exportKey('pkcs8', first.privateKey)).rejects.toThrow();
  });
});
