import { KdfError, LongPassphraseError, MalformedPassphraseError } from '../errors.ts';
import { MAX_PASSPHRASE_BYTES } from '../limits.ts';
import { concatenate, utf8 } from '../primitives/bytes.ts';

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

function normalizePassphrase(passphrase: string): Uint8Array<ArrayBuffer> {
  return utf8(passphrase.normalize('NFC'));
}

export function assertWellFormedPassphrase(kdfId: KdfId, passphrase: string): void {
  if (kdfId !== KDF_NONE && !passphrase.isWellFormed()) {
    throw new MalformedPassphraseError();
  }
}

const OPAQUE_CONTEXT = 'nylzo/v3/opaque';

// The OPRF input salt_pw || pw of spec section 2.1; pw is empty for a message without a passphrase.
export function opaquePassword(passphraseSalt: Uint8Array, kdfId: KdfId, passphrase: string): Uint8Array<ArrayBuffer> {
  assertWellFormedPassphrase(kdfId, passphrase);
  if (kdfId === KDF_NONE) {
    return concatenate(passphraseSalt);
  }
  const normalized = normalizePassphrase(passphrase);
  try {
    if (normalized.length > MAX_PASSPHRASE_BYTES) {
      throw new LongPassphraseError();
    }
    return concatenate(passphraseSalt, normalized);
  } finally {
    normalized.fill(0);
  }
}

export function opaqueContext(id: Uint8Array, kdfId: KdfId): Uint8Array<ArrayBuffer> {
  return concatenate(utf8(OPAQUE_CONTEXT), id, Uint8Array.of(kdfId));
}
