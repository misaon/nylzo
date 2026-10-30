import { scrypt } from '@noble/hashes/scrypt.js';

import { KdfError } from './errors.ts';
import { deriveContent, type DerivedKeys, deriveLink } from './keys/derive.ts';
import {
  assertWellFormedPassphrase,
  KDF_NONE,
  KDF_SCRYPT,
  type KdfId,
  normalizePassphrase,
  SCRYPT_N,
  SCRYPT_P,
  SCRYPT_R,
} from './keys/kdf.ts';
import { KEY_LENGTH } from './limits.ts';
import { arrayBufferBacked } from './primitives/bytes.ts';

export function scryptPassphraseKey(
  passphrase: string,
  passphraseSalt: Uint8Array,
  onProgress?: (fraction: number) => void,
): Uint8Array<ArrayBuffer> {
  assertWellFormedPassphrase(KDF_SCRYPT, passphrase);
  const parameters = { N: SCRYPT_N, r: SCRYPT_R, p: SCRYPT_P, dkLen: KEY_LENGTH, ...(onProgress && { onProgress }) };
  const password = normalizePassphrase(passphrase);
  try {
    return arrayBufferBacked(scrypt(password, passphraseSalt, parameters));
  } finally {
    password.fill(0);
  }
}

export function derivePassphraseKey(
  kdfId: KdfId,
  passphrase: string,
  passphraseSalt: Uint8Array,
): Uint8Array<ArrayBuffer> {
  switch (kdfId) {
    case KDF_NONE: {
      return new Uint8Array(0);
    }
    case KDF_SCRYPT: {
      return scryptPassphraseKey(passphrase, passphraseSalt);
    }
    default: {
      throw new KdfError();
    }
  }
}

export async function deriveAll(
  id: Uint8Array,
  secret: Uint8Array,
  kdfId: KdfId,
  passphrase: string,
): Promise<DerivedKeys> {
  assertWellFormedPassphrase(kdfId, passphrase);
  const link = await deriveLink(id, secret);
  let passphraseKey: Uint8Array | undefined;
  try {
    passphraseKey = derivePassphraseKey(kdfId, passphrase, link.passphraseSalt);
    const content = await deriveContent(id, secret, passphraseKey);
    return { link, content };
  } catch (error) {
    link.readSeed.fill(0);
    link.passphraseSalt.fill(0);
    throw error;
  } finally {
    passphraseKey?.fill(0);
  }
}
