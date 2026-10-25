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
import { KEY_LEN } from './limits.ts';
import { arrayBufferBacked } from './primitives/bytes.ts';

export function scryptPassphraseKey(
  passphrase: string,
  passphraseSalt: Uint8Array,
  onProgress?: (fraction: number) => void,
): Uint8Array<ArrayBuffer> {
  assertWellFormedPassphrase(KDF_SCRYPT, passphrase);
  const parameters = { N: SCRYPT_N, r: SCRYPT_R, p: SCRYPT_P, dkLen: KEY_LEN, ...(onProgress && { onProgress }) };
  return arrayBufferBacked(scrypt(normalizePassphrase(passphrase), passphraseSalt, parameters));
}

// eslint-disable-next-line @typescript-eslint/require-await -- callers await this Promise; scrypt itself runs synchronously
export async function anyPassphraseKey(
  kdfId: KdfId,
  passphrase: string,
  passphraseSalt: Uint8Array,
): Promise<Uint8Array<ArrayBuffer>> {
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
  const passphraseKey = await anyPassphraseKey(kdfId, passphrase, link.saltPw);
  const content = await deriveContent(id, secret, passphraseKey);
  return { link, content };
}
