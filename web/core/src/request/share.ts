import { expand, extract } from '@noble/hashes/hkdf.js';
import { sha256 } from '@noble/hashes/sha2.js';
import { ml_kem768_x25519 } from '@noble/post-quantum/hybrid.js';

import { EncodingError, IntegrityError } from '../errors.ts';
import { SHARE_LENGTH } from '../limits.ts';
import { decodeBase64urlExact, encodeBase64url } from '../primitives/base64url.ts';
import { arrayBufferBacked, assertLength, concatenate, utf8 } from '../primitives/bytes.ts';

const SHARE_SUITE = '1';
const ENCAPSULATED_LENGTH = 1120;
const TAG_LENGTH = 16;
const WRAPPED_SHARE_LENGTH = ENCAPSULATED_LENGTH + SHARE_LENGTH + TAG_LENGTH;
const AES_KEY_LENGTH = 32;
const NONCE_LENGTH = 12;
const SHARE_INFO = utf8('nylzo/v2/share');
const HPKE_VERSION = utf8('HPKE-v1');
// RFC 9180 suite_id: KEM MLKEM768-X25519 0x647a, KDF HKDF-SHA256 0x0001, AEAD AES-256-GCM 0x0002.
const SHARE_HPKE_SUITE = concatenate(utf8('HPKE'), new Uint8Array([0x64, 0x7a, 0x00, 0x01, 0x00, 0x02]));

// A one-time MLKEM768-X25519 key pair (spec 5.6); the seed derives both halves and is erased after one use.
export interface ShareKey {
  readonly seed: Uint8Array<ArrayBuffer>;
  readonly publicKey: Uint8Array<ArrayBuffer>;
}

export function newShareKey(): ShareKey {
  const { secretKey, publicKey } = ml_kem768_x25519.keygen();
  return { seed: arrayBufferBacked(secretKey), publicKey: arrayBufferBacked(publicKey) };
}

export function formatShareKey(publicKey: Uint8Array): string {
  return `${SHARE_SUITE}.${encodeBase64url(publicKey)}`;
}

export function parseWrappedShare(text: string): Uint8Array<ArrayBuffer> {
  return decodeBase64urlExact(text, WRAPPED_SHARE_LENGTH);
}

function labeledExtract(suite: Uint8Array, salt: Uint8Array, label: string, ikm: Uint8Array): Uint8Array {
  return extract(sha256, concatenate(HPKE_VERSION, suite, utf8(label), ikm), salt);
}

function labeledExpand(suite: Uint8Array, prk: Uint8Array, label: string, info: Uint8Array, length: number) {
  const prefix = new Uint8Array([length >> 8, length & 0xff]);
  return expand(sha256, prk, concatenate(prefix, HPKE_VERSION, suite, utf8(label), info), length);
}

// The base mode KeySchedule of RFC 9180 section 5.1, with an empty PSK.
export function hpkeKeySchedule(
  suite: Uint8Array,
  sharedSecret: Uint8Array,
  info: Uint8Array,
  keyLength: number,
): { key: Uint8Array<ArrayBuffer>; baseNonce: Uint8Array<ArrayBuffer> } {
  const empty = new Uint8Array(0);
  const context = concatenate(
    new Uint8Array([0]),
    labeledExtract(suite, empty, 'psk_id_hash', empty),
    labeledExtract(suite, empty, 'info_hash', info),
  );
  const secret = labeledExtract(suite, sharedSecret, 'secret', empty);
  try {
    return {
      key: arrayBufferBacked(labeledExpand(suite, secret, 'key', context, keyLength)),
      baseNonce: arrayBufferBacked(labeledExpand(suite, secret, 'base_nonce', context, NONCE_LENGTH)),
    };
  } finally {
    secret.fill(0);
  }
}

async function shareCipher(sharedSecret: Uint8Array, usage: KeyUsage) {
  const { key, baseNonce } = hpkeKeySchedule(SHARE_HPKE_SUITE, sharedSecret, SHARE_INFO, AES_KEY_LENGTH);
  try {
    const aesKey = await crypto.subtle.importKey('raw', key, 'AES-GCM', false, [usage]);
    return { aesKey, parameters: { name: 'AES-GCM', iv: baseNonce, tagLength: TAG_LENGTH * 8 } };
  } finally {
    key.fill(0);
  }
}

// Opens the share with HPKE (spec 5.6). A share key opens one share, so this erases its seed.
export async function unwrapShare(key: ShareKey, wrapped: Uint8Array | undefined): Promise<Uint8Array<ArrayBuffer>> {
  if (key.seed.every((byte) => byte === 0)) {
    throw new Error('the share key was already used');
  }
  let sharedSecret: Uint8Array;
  try {
    if (wrapped === undefined) {
      throw new IntegrityError('the server sent no share');
    }
    assertLength(wrapped, WRAPPED_SHARE_LENGTH, 'wrapped share');
    sharedSecret = ml_kem768_x25519.decapsulate(wrapped.subarray(0, ENCAPSULATED_LENGTH), key.seed);
  } catch (error) {
    throw error instanceof EncodingError || error instanceof IntegrityError
      ? error
      : new IntegrityError('the server sent a share that cannot open');
  } finally {
    key.seed.fill(0);
  }
  let opened: ArrayBuffer;
  try {
    const { aesKey, parameters } = await shareCipher(sharedSecret, 'decrypt');
    opened = await crypto.subtle.decrypt(parameters, aesKey, arrayBufferBacked(wrapped.subarray(ENCAPSULATED_LENGTH)));
  } catch {
    throw new IntegrityError('the server sent a share that does not open');
  } finally {
    sharedSecret.fill(0);
  }
  return new Uint8Array(opened);
}

// Seals a share as the server does, for the fake server of the web client's tests.
export async function wrapShare(share: Uint8Array, publicKey: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  const { cipherText, sharedSecret } = ml_kem768_x25519.encapsulate(publicKey);
  try {
    const { aesKey, parameters } = await shareCipher(sharedSecret, 'encrypt');
    const sealed = await crypto.subtle.encrypt(parameters, aesKey, arrayBufferBacked(share));
    return concatenate(cipherText, new Uint8Array(sealed));
  } finally {
    sharedSecret.fill(0);
  }
}
