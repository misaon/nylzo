import { ed25519 } from '@noble/curves/ed25519.js';

import { SEED_LENGTH } from '../limits.ts';
import { decodeBase64urlExact, encodeBase64url } from '../primitives/base64url.ts';
import { arrayBufferBacked, assertLength, concatenate, utf8 } from '../primitives/bytes.ts';
import { hash } from '../primitives/identifiers.ts';

export const PUBLIC_KEY_LENGTH = 32;
export const CHALLENGE_LENGTH = 36;
export const PURPOSE_READ = 'read';
export const PURPOSE_VERIFIER = 'verifier';
export const PURPOSE_MANAGE = 'manage';

export type Purpose = typeof PURPOSE_READ | typeof PURPOSE_VERIFIER | typeof PURPOSE_MANAGE;

const AUTHORIZATION_SCHEME = 'Nylzo-Sig';
const REQUEST_LABEL = 'nylzo/v1/request';
const FIELD_SEPARATOR = new Uint8Array(1);
const PKCS8_ED25519_PREFIX = decodeBase64urlExact('MC4CAQAwBQYDK2VwBCIEIA', 16);
const GENERATE_ATTEMPTS = 8;

export interface CryptoRequestKey {
  readonly privateKey: CryptoKey;
  readonly publicKey: Uint8Array<ArrayBuffer>;
}

export type RequestKey = CryptoRequestKey;

const revokedKeys = new WeakSet<CryptoKey>();

export interface RequestSignature {
  readonly publicKey: Uint8Array<ArrayBuffer>;
  readonly challenge: Uint8Array<ArrayBuffer>;
  readonly signature: Uint8Array<ArrayBuffer>;
}

export async function requestKey(seed: Uint8Array): Promise<RequestKey> {
  assertLength(seed, SEED_LENGTH, 'seed');
  const pkcs8 = concatenate(PKCS8_ED25519_PREFIX, seed);
  try {
    // Web Crypto has no interoperable public-key derivation from an imported private key.
    const publicKey = arrayBufferBacked(ed25519.getPublicKey(pkcs8.subarray(PKCS8_ED25519_PREFIX.length)));
    const privateKey = await importSigningKey(pkcs8, publicKey);
    return { privateKey, publicKey };
  } finally {
    pkcs8.fill(0);
  }
}

async function importSigningKey(pkcs8: Uint8Array<ArrayBuffer>, publicKey: Uint8Array): Promise<CryptoKey> {
  try {
    return await crypto.subtle.importKey('pkcs8', pkcs8, { name: 'Ed25519' }, false, ['sign']);
  } catch (error) {
    if (!isDomException(error, 'DataError')) {
      throw error;
    }
  }
  // Some WebKit builds reject PKCS#8 seeds with leading zeroes. Keep signing native.
  const jwk: JsonWebKey = {
    kty: 'OKP',
    crv: 'Ed25519',
    d: encodeBase64url(pkcs8.subarray(PKCS8_ED25519_PREFIX.length)),
    x: encodeBase64url(publicKey),
    ext: false,
    key_ops: ['sign'],
  };
  try {
    return await crypto.subtle.importKey('jwk', jwk, { name: 'Ed25519' }, false, ['sign']);
  } finally {
    delete jwk.d;
  }
}

export function wipeRequestKey(key: RequestKey | undefined): void {
  // Revoke future application signatures; only the browser controls CryptoKey memory.
  if (key) {
    revokedKeys.add(key.privateKey);
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
  assertLength(challenge, CHALLENGE_LENGTH, 'challenge');
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
  if (revokedKeys.has(key.privateKey)) {
    throw new Error('the request key was released');
  }
  const message = await requestMessage(purpose, method, path, challenge, body);
  const signature = new Uint8Array(await crypto.subtle.sign('Ed25519', key.privateKey, message));
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
  return decodeBase64urlExact(text, CHALLENGE_LENGTH);
}

function isDomException(error: unknown, name: string): boolean {
  return error instanceof DOMException && error.name === name;
}
