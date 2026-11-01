import { IntegrityError } from '../errors.ts';
import { SHARE_LENGTH } from '../limits.ts';
import { decodeBase64urlExact, encodeBase64url } from '../primitives/base64url.ts';
import { arrayBufferBacked, assertLength, concatenate, utf8 } from '../primitives/bytes.ts';

const SHARE_KEY_LENGTH = 32;
const WRAPPED_SHARE_LENGTH = SHARE_KEY_LENGTH + SHARE_LENGTH;
const SHARE_LABEL = 'nylzo/v2/share';

// A one-time X25519 key pair; the server wraps the expiry share to it (spec 5.6).
export interface ShareKey {
  readonly privateKey: CryptoKey;
  readonly publicKey: Uint8Array<ArrayBuffer>;
}

export async function newShareKey(): Promise<ShareKey> {
  const pair = await crypto.subtle.generateKey({ name: 'X25519' }, false, ['deriveBits']);
  return {
    privateKey: pair.privateKey,
    publicKey: new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey)),
  };
}

export function shareKeyText(key: ShareKey): string {
  return encodeBase64url(key.publicKey);
}

export function parseWrappedShare(text: string): Uint8Array<ArrayBuffer> {
  return decodeBase64urlExact(text, WRAPPED_SHARE_LENGTH);
}

export async function unwrapShare(key: ShareKey, wrapped: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  assertLength(wrapped, WRAPPED_SHARE_LENGTH, 'wrapped share');
  const serverPublic = wrapped.slice(0, SHARE_KEY_LENGTH);
  let secret: Uint8Array<ArrayBuffer>;
  try {
    const serverKey = await crypto.subtle.importKey('raw', serverPublic, { name: 'X25519' }, true, []);
    secret = new Uint8Array(
      await crypto.subtle.deriveBits({ name: 'X25519', public: serverKey }, key.privateKey, SHARE_KEY_LENGTH * 8),
    );
  } catch {
    throw new IntegrityError('the server sent a share key that cannot agree');
  }
  let mask: Uint8Array<ArrayBuffer>;
  try {
    if (secret.every((byte) => byte === 0)) {
      throw new IntegrityError('the server sent a share key of small order');
    }
    const keyMaterial = await crypto.subtle.importKey('raw', secret, 'HKDF', false, ['deriveBits']);
    const parameters: HkdfParams = {
      name: 'HKDF',
      hash: 'SHA-256',
      salt: concatenate(key.publicKey, serverPublic),
      info: utf8(SHARE_LABEL),
    };
    mask = new Uint8Array(await crypto.subtle.deriveBits(parameters, keyMaterial, SHARE_LENGTH * 8));
  } finally {
    secret.fill(0);
  }
  const masked = arrayBufferBacked(wrapped.subarray(SHARE_KEY_LENGTH));
  const share = new Uint8Array(SHARE_LENGTH);
  for (const [index, byte] of masked.entries()) {
    share[index] = byte ^ (mask[index] ?? 0);
  }
  mask.fill(0);
  return share;
}
