import { scrypt } from '@noble/hashes/scrypt.js';

import { SCRYPT_N, SCRYPT_P, SCRYPT_R } from './keys/kdf.ts';
import { arrayBufferBacked } from './primitives/bytes.ts';

const STRETCH_LENGTH = 64;
const STRETCH_SALT = new Uint8Array(16);

// The key stretching function of profile 3: scrypt over the OPRF output with 16 zero bytes as salt (spec section 2.1).
export function stretchScrypt(message: Uint8Array, onProgress?: (fraction: number) => void): Uint8Array<ArrayBuffer> {
  const parameters = {
    N: SCRYPT_N,
    r: SCRYPT_R,
    p: SCRYPT_P,
    dkLen: STRETCH_LENGTH,
    ...(onProgress && { onProgress }),
  };
  return arrayBufferBacked(scrypt(message, STRETCH_SALT, parameters));
}
