import { sha256 } from '@noble/hashes/sha2.js';
import { ml_kem768_x25519 } from '@noble/post-quantum/hybrid.js';
import { describe, expect, it } from 'vitest';

import ietf from '../../test/hpke-pq-mlkem768-x25519.json' with { type: 'json' };
import { hexToBytes, vectors } from '../../test/vectors.ts';
import { IntegrityError } from '../errors.ts';
import { decodeBase64url } from '../primitives/base64url.ts';
import { concatenate, utf8 } from '../primitives/bytes.ts';
import { formatShareKey, hpkeKeySchedule, newShareKey, parseWrappedShare, unwrapShare } from './share.ts';
import type { ShareKey } from './share.ts';

function vectorKey(seedHex: string, clientKey: string): ShareKey {
  const seed = hexToBytes(seedHex);
  const publicKey = ml_kem768_x25519.getPublicKey(seed);
  expect(formatShareKey(publicKey)).toBe(clientKey);
  return { seed, publicKey: decodeBase64url(clientKey.slice(2)) };
}

const wiped = (bytes: Uint8Array) => bytes.every((byte) => byte === 0);

describe('AK9: share vectors (spec 5.6)', () => {
  it.each(vectors.share.filter((vector) => vector.valid))('$name unwraps to E', async (vector) => {
    const key = vectorKey(vector.client_seed_hex, vector.client_key);
    const share = await unwrapShare(key, parseWrappedShare(vector.wrapped));
    expect(share.toHex()).toBe(vector.e_hex);
    expect(wiped(key.seed)).toBe(true);
  });

  it.each(vectors.share.filter((vector) => !vector.valid))('$name is refused', async (vector) => {
    const key = vectorKey(vector.client_seed_hex, vector.client_key);
    await expect(unwrapShare(key, parseWrappedShare(vector.wrapped))).rejects.toBeInstanceOf(IntegrityError);
    expect(wiped(key.seed)).toBe(true);
  });
});

describe('a share key', () => {
  it('opens one share and then refuses as used, not as a changed share', async () => {
    const vector = vectors.share.find((candidate) => candidate.valid);
    if (vector === undefined) {
      throw new Error('the vectors hold no valid share');
    }
    const key = vectorKey(vector.client_seed_hex, vector.client_key);
    const wrapped = parseWrappedShare(vector.wrapped);
    await unwrapShare(key, wrapped);
    const again = unwrapShare(key, wrapped);
    await expect(again).rejects.toThrow('the share key was already used');
    await expect(again).rejects.not.toBeInstanceOf(IntegrityError);
  });
});

describe('HPKE with MLKEM768-X25519 matches draft-ietf-hpke-pq-05', () => {
  it('derives the key pair, the shared secret and the key schedule of the IETF vector', () => {
    const seed = hexToBytes(ietf.skRm);
    expect(sha256(ml_kem768_x25519.getPublicKey(seed)).toHex()).toBe(ietf.pkRm_sha256);
    const sharedSecret = ml_kem768_x25519.decapsulate(hexToBytes(ietf.enc), seed);
    expect(sharedSecret.toHex()).toBe(ietf.shared_secret);
    const ids = new Uint8Array([ietf.kem_id >> 8, ietf.kem_id & 0xff, 0, ietf.kdf_id, 0, ietf.aead_id]);
    const { key, baseNonce } = hpkeKeySchedule(concatenate(utf8('HPKE'), ids), sharedSecret, hexToBytes(ietf.info), 32);
    expect([key.toHex(), baseNonce.toHex()]).toEqual([ietf.key, ietf.base_nonce]);
  });
});

describe('share keys', () => {
  it('are one-time MLKEM768-X25519 keys sent with share suite 1', () => {
    const first = newShareKey();
    const second = newShareKey();
    expect([first.seed.length, first.publicKey.length]).toEqual([32, 1216]);
    expect(first.publicKey).not.toEqual(second.publicKey);
    expect(formatShareKey(first.publicKey)).toMatch(/^1\.[\w-]{1622}$/u);
  });
});
