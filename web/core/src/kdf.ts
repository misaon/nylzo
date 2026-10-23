import { scryptAsync } from '@noble/hashes/scrypt.js';

import { arrayBufferBacked, utf8 } from './bytes.ts';
import { KdfError, MalformedPassphraseError, WeakPassphraseError } from './errors.ts';
import { KEY_LEN } from './limits.ts';

export const KDF_NONE = 0;
export const KDF_SCRYPT = 1;
export const KDF_PBKDF2 = 2;

export type KdfId = typeof KDF_NONE | typeof KDF_SCRYPT | typeof KDF_PBKDF2;

export const SCRYPT_N = 1 << 17;
export const SCRYPT_R = 8;
export const SCRYPT_P = 1;
export const PBKDF2_ITERATIONS = 600000;

export function isKdfId(value: number): value is KdfId {
  return value === KDF_NONE || value === KDF_SCRYPT || value === KDF_PBKDF2;
}

export function parseKdfId(text: string): KdfId {
  switch (text) {
    case '0':
      return KDF_NONE;
    case '1':
      return KDF_SCRYPT;
    case '2':
      return KDF_PBKDF2;
    default:
      throw new KdfError();
  }
}

export function kdfRequiresPassphrase(kdfId: KdfId): boolean {
  return kdfId !== KDF_NONE;
}

export function normalizePassphrase(passphrase: string): Uint8Array<ArrayBuffer> {
  return utf8(passphrase.normalize('NFC'));
}

export const MIN_PASSPHRASE_CHARS = 12;

export function assertStrongPassphrase(passphrase: string): void {
  if (!passphrase.isWellFormed()) {
    throw new MalformedPassphraseError();
  }
  if ([...passphrase.normalize('NFC')].length < MIN_PASSPHRASE_CHARS) {
    throw new WeakPassphraseError();
  }
}

export function assertWellFormedPassphrase(kdfId: KdfId, passphrase: string): void {
  if (kdfId !== KDF_NONE && !passphrase.isWellFormed()) {
    throw new MalformedPassphraseError();
  }
}

export async function derivePassphraseKey(
  kdfId: KdfId,
  passphrase: string,
  saltPw: Uint8Array,
): Promise<Uint8Array<ArrayBuffer>> {
  if (kdfId === KDF_NONE) {
    return new Uint8Array(0);
  }
  assertWellFormedPassphrase(kdfId, passphrase);
  if (kdfId === KDF_SCRYPT) {
    return scrypt(normalizePassphrase(passphrase), saltPw);
  }
  if (kdfId === KDF_PBKDF2) {
    return pbkdf2(normalizePassphrase(passphrase), saltPw);
  }
  throw new KdfError();
}

async function scrypt(password: Uint8Array, salt: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  const key = await scryptAsync(password, salt, { N: SCRYPT_N, r: SCRYPT_R, p: SCRYPT_P, dkLen: KEY_LEN });
  return arrayBufferBacked(key);
}

async function pbkdf2(password: Uint8Array<ArrayBuffer>, salt: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  const key = await crypto.subtle.importKey('raw', password, 'PBKDF2', false, ['deriveBits']);
  const params: Pbkdf2Params = {
    name: 'PBKDF2',
    hash: 'SHA-256',
    salt: arrayBufferBacked(salt),
    iterations: PBKDF2_ITERATIONS,
  };
  return new Uint8Array(await crypto.subtle.deriveBits(params, key, KEY_LEN * 8));
}
