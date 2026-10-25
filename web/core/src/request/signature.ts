import { ed25519 } from '@noble/curves/ed25519.js';

import { TOKEN_LEN } from '../limits.ts';
import { decodeBase64urlExact, encodeBase64url } from '../primitives/base64url.ts';
import { arrayBufferBacked, assertLength, concatenate, utf8 } from '../primitives/bytes.ts';
import { hash } from '../primitives/identifiers.ts';

export const PUBLIC_KEY_LEN = 32;
export const SIGNATURE_LEN = 64;
export const CHALLENGE_LEN = 36;
const AUTHORIZATION_SCHEME = 'Nylzo-Sig';
export const PURPOSE_READ = 'read';
export const PURPOSE_VERIFIER = 'verifier';
export const PURPOSE_MANAGE = 'manage';

export type Purpose = typeof PURPOSE_READ | typeof PURPOSE_VERIFIER | typeof PURPOSE_MANAGE;

const REQUEST_LABEL = 'nylzo/v1/request';
const FIELD_SEPARATOR = new Uint8Array(1);
const PKCS8_ED25519_PREFIX = decodeBase64urlExact('MC4CAQAwBQYDK2VwBCIEIA', 16);
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

export async function requestKey(seed: Uint8Array): Promise<RequestKey> {
  assertLength(seed, TOKEN_LEN, 'seed');
  const pkcs8 = concatenate(PKCS8_ED25519_PREFIX, seed);
  try {
    const exportableKey = await crypto.subtle.importKey('pkcs8', pkcs8, { name: 'Ed25519' }, true, ['sign']);
    const jwk = await crypto.subtle.exportKey('jwk', exportableKey);
    const publicKey = decodeBase64urlExact(jwk.x ?? '', PUBLIC_KEY_LEN);
    const privateKey = await crypto.subtle.importKey('pkcs8', pkcs8, { name: 'Ed25519' }, false, ['sign']);
    return { privateKey, publicKey };
  } catch (error) {
    if (!isDomException(error, 'DataError')) {
      throw error;
    }
    return seedRequestKey(seed);
  } finally {
    pkcs8.fill(0);
  }
}

export function seedRequestKey(seed: Uint8Array): SeedRequestKey {
  assertLength(seed, TOKEN_LEN, 'seed');
  const seedCopy = Uint8Array.from(seed);
  return { seed: seedCopy, publicKey: arrayBufferBacked(ed25519.getPublicKey(seedCopy)) };
}

export function wipeRequestKey(key: RequestKey | undefined): void {
  if (key && 'seed' in key) {
    key.seed.fill(0);
  }
}

export async function newRequestKey(): Promise<CryptoRequestKey> {
  for (let attempt = 1; ; attempt++) {
    try {
      const pair = await crypto.subtle.generateKey({ name: 'Ed25519' }, false, ['sign', 'verify']);
      return {
        privateKey: pair.privateKey,
        publicKey: new Uint8Array(await crypto.subtle.exportKey('raw', pair.publicKey)),
      };
    } catch (error) {
      if (attempt >= GENERATE_ATTEMPTS || !isDomException(error, 'OperationError')) {
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
  const fields = [REQUEST_LABEL, purpose, method, path].flatMap((field) => [utf8(field), FIELD_SEPARATOR]);
  return concatenate(...fields, challenge, await hash(body));
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
  const signature =
    'seed' in key
      ? signWithSeed(key.seed, message)
      : new Uint8Array(await crypto.subtle.sign('Ed25519', key.privateKey, message));
  return { publicKey: key.publicKey, challenge: arrayBufferBacked(challenge), signature };
}

export function authorizationHeader({ publicKey, challenge, signature }: RequestSignature): string {
  const credentials = [publicKey, challenge, signature].map((bytes) => encodeBase64url(bytes)).join('.');
  return `${AUTHORIZATION_SCHEME} ${credentials}`;
}

export function verifierHeader({ publicKey, signature }: RequestSignature): string {
  return `${encodeBase64url(publicKey)}.${encodeBase64url(signature)}`;
}

export function parseChallenge(text: string): Uint8Array<ArrayBuffer> {
  return decodeBase64urlExact(text, CHALLENGE_LEN);
}

function signWithSeed(seed: Uint8Array, message: Uint8Array): Uint8Array<ArrayBuffer> {
  if (seed.every((byte) => byte === 0)) {
    throw new Error('the request key was wiped');
  }
  return arrayBufferBacked(ed25519.sign(message, seed));
}

function isDomException(error: unknown, name: string): boolean {
  return error instanceof DOMException && error.name === name;
}
