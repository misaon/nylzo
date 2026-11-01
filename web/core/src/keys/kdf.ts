import { KdfError, MalformedPassphraseError } from '../errors.ts';
import { utf8 } from '../primitives/bytes.ts';

export const KDF_NONE = 0;
export const KDF_RETIRED = 2;
export const KDF_SCRYPT = 3;

export type KdfId = typeof KDF_NONE | typeof KDF_SCRYPT;

export const SCRYPT_N = 1 << 18;
export const SCRYPT_R = 8;
export const SCRYPT_P = 1;

export function isKdfId(value: number): value is KdfId {
  return value === KDF_NONE || value === KDF_SCRYPT;
}

export function parseKdfId(text: string): KdfId {
  const kdfId = Number(text);
  if (text !== String(kdfId) || !isKdfId(kdfId)) {
    throw new KdfError();
  }
  return kdfId;
}

export function normalizePassphrase(passphrase: string): Uint8Array<ArrayBuffer> {
  return utf8(passphrase.normalize('NFC'));
}

export function assertWellFormedPassphrase(kdfId: KdfId, passphrase: string): void {
  if (kdfId !== KDF_NONE && !passphrase.isWellFormed()) {
    throw new MalformedPassphraseError();
  }
}
