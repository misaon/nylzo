import { scrypt } from '@noble/hashes/scrypt.js';

import { arrayBufferBacked } from './bytes.ts';
import { deriveContent, deriveLink, type DerivedKeys } from './derive.ts';
import { KdfError } from './errors.ts';
import { KDF_NONE, KDF_SCRYPT, SCRYPT_N, SCRYPT_P, SCRYPT_R, assertWellFormedPassphrase, normalizePassphrase, type KdfId } from './kdf.ts';
import { KEY_LEN } from './limits.ts';

export function scryptPassphraseKey(
  passphrase: string,
  saltPw: Uint8Array,
  onProgress?: (fraction: number) => void,
): Uint8Array<ArrayBuffer> {
  assertWellFormedPassphrase(KDF_SCRYPT, passphrase);
  const params = { N: SCRYPT_N, r: SCRYPT_R, p: SCRYPT_P, dkLen: KEY_LEN, ...(onProgress ? { onProgress } : {}) };
  return arrayBufferBacked(scrypt(normalizePassphrase(passphrase), saltPw, params));
}

export async function anyPassphraseKey(kdfId: KdfId, passphrase: string, saltPw: Uint8Array): Promise<Uint8Array<ArrayBuffer>> {
  if (kdfId === KDF_SCRYPT) {
    return scryptPassphraseKey(passphrase, saltPw);
  }
  if (kdfId === KDF_NONE) {
    return new Uint8Array(0);
  }
  throw new KdfError();
}

export async function deriveAll(
  id: Uint8Array,
  s: Uint8Array,
  kdfId: KdfId,
  passphrase: string,
): Promise<DerivedKeys> {
  assertWellFormedPassphrase(kdfId, passphrase);
  const link = await deriveLink(id, s);
  const p = await anyPassphraseKey(kdfId, passphrase, link.saltPw);
  const content = await deriveContent(id, s, p);
  return { link, content };
}
