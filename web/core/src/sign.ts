import { ed25519 } from '@noble/curves/ed25519.js';

import { decodeBase64urlExact, encodeBase64url } from './base64url.ts';
import { arrayBufferBacked, assertLength, concat, utf8 } from './bytes.ts';
import { hash } from './ids.ts';
import { TOKEN_LEN } from './limits.ts';

export const PUBLIC_KEY_LEN = 32;
export const SIGNATURE_LEN = 64;
export const CHALLENGE_LEN = 36;
export const AUTH_SCHEME = 'Nylzo-Sig';
export const PURPOSE_READ = 'read';
export const PURPOSE_VERIFIER = 'verifier';
export const PURPOSE_MANAGE = 'manage';

export type Purpose = typeof PURPOSE_READ | typeof PURPOSE_VERIFIER | typeof PURPOSE_MANAGE;

const REQUEST_LABEL = 'nylzo/v1/request';
const PKCS8_ED25519_PREFIX = Uint8Array.from([
  0x30, 0x2e, 0x02, 0x01, 0x00, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x04, 0x22, 0x04, 0x20,
]);

const GENERATE_ATTEMPTS = 8;

export interface CryptoRequestKey {
  readonly privateKey: CryptoKey;
  readonly publicKey: Uint8Array<ArrayBuffer>;
}

export interface SeedRequestKey {
  readonly seed: Uint8Array<ArrayBuffer>;
  readonly publicKey: Uint8Array<ArrayBuffer>;
}

export type RequestKey = CryptoRequestKey | SeedRequestKey;

export interface RequestSignature {
  readonly publicKey: Uint8Array<ArrayBuffer>;
  readonly challenge: Uint8Array<ArrayBuffer>;
  readonly signature: Uint8Array<ArrayBuffer>;
}

const failedWith = (error: unknown, name: string) => error instanceof DOMException && error.name === name;

export async function requestKey(seed: Uint8Array): Promise<RequestKey> {
  assertLength(seed, TOKEN_LEN, 'seed');
  const pkcs8 = concat(PKCS8_ED25519_PREFIX, seed);
  try {
    const exportable = await crypto.subtle.importKey('pkcs8', pkcs8, { name: 'Ed25519' }, true, ['sign']);
    const jwk = await crypto.subtle.exportKey('jwk', exportable);
    const publicKey = decodeBase64urlExact(jwk.x ?? '', PUBLIC_KEY_LEN);
    const privateKey = await crypto.subtle.importKey('pkcs8', pkcs8, { name: 'Ed25519' }, false, ['sign']);
    return { privateKey, publicKey };
  } catch (error) {
    if (!failedWith(error, 'DataError')) {
      throw error;
    }
    return seedRequestKey(seed);
  } finally {
    pkcs8.fill(0);
  }
}

export function seedRequestKey(seed: Uint8Array): SeedRequestKey {
  assertLength(seed, TOKEN_LEN, 'seed');
  const copy = arrayBufferBacked(Uint8Array.from(seed));
  return { seed: copy, publicKey: arrayBufferBacked(ed25519.getPublicKey(copy)) };
}

export function wipeRequestKey(key: RequestKey | undefined): void {
  if (key && 'seed' in key) {
    key.seed.fill(0);
  }
}

export async function newRequestKey(): Promise<CryptoRequestKey> {
  for (let attempt = 1; ; attempt++) {
    try {
      const pair = (await crypto.subtle.generateKey({ name: 'Ed25519' }, false, ['sign', 'verify'])) as CryptoKeyPair;
      return {
        privateKey: pair.privateKey,
        publicKey: new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey)),
      };
    } catch (error) {
      if (attempt >= GENERATE_ATTEMPTS || !failedWith(error, 'OperationError')) {
        throw error;
      }
    }
  }
}

export async function requestMessage(
  purpose: Purpose,
  method: string,
  path: string,
  challenge: Uint8Array,
  body: Uint8Array,
): Promise<Uint8Array<ArrayBuffer>> {
  assertLength(challenge, CHALLENGE_LEN, 'challenge');
  const zero = new Uint8Array(1);
  const parts: Uint8Array[] = [];
  for (const part of [REQUEST_LABEL, purpose, method, path]) {
    parts.push(utf8(part), zero);
  }
  return concat(...parts, challenge, await hash(body));
}

export async function signRequest(
  key: RequestKey,
  purpose: Purpose,
  method: string,
  path: string,
  challenge: Uint8Array,
  body: Uint8Array = new Uint8Array(0),
): Promise<RequestSignature> {
  const message = await requestMessage(purpose, method, path, challenge, body);
  if ('seed' in key && key.seed.every((byte) => byte === 0)) {
    throw new Error('the request key was wiped');
  }
  const signature =
    'seed' in key
      ? arrayBufferBacked(ed25519.sign(message, key.seed))
      : new Uint8Array(await crypto.subtle.sign('Ed25519', key.privateKey, message));
  return { publicKey: key.publicKey, challenge: arrayBufferBacked(challenge), signature };
}

export function authorizationHeader(sig: RequestSignature): string {
  return `${AUTH_SCHEME} ${encodeBase64url(sig.publicKey)}.${encodeBase64url(sig.challenge)}.${encodeBase64url(sig.signature)}`;
}

export function verifierHeader(sig: RequestSignature): string {
  return `${encodeBase64url(sig.publicKey)}.${encodeBase64url(sig.signature)}`;
}

export function parseChallenge(text: string): Uint8Array<ArrayBuffer> {
  return decodeBase64urlExact(text, CHALLENGE_LEN);
}
