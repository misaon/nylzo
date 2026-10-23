import { scrypt } from '@noble/hashes/scrypt.js';

import { decodeBase64urlExact } from './base64url.ts';
import { arrayBufferBacked, assertLength, concat } from './bytes.ts';
import { PowError } from './errors.ts';
import { ID_LEN, KEY_LEN } from './limits.ts';

export const POW_N = 1 << 14;
export const POW_R = 8;
export const POW_P = 1;
export const POW_CHALLENGE_LEN = 36;
export const MAX_POW_DIFFICULTY = 8;

const NONCE_OFFSET = 4;
const NONCE_LEN = 16;
const COUNTER_HEX_CHARS = 16;

export function parsePowChallenge(text: string): Uint8Array<ArrayBuffer> {
  return decodeBase64urlExact(text, POW_CHALLENGE_LEN);
}

export function powDigest(challenge: Uint8Array, id: Uint8Array, counter: number): Uint8Array<ArrayBuffer> {
  assertLength(challenge, POW_CHALLENGE_LEN, 'challenge');
  assertLength(id, ID_LEN, 'id');
  assertCounter(counter);
  const salt = new Uint8Array(8);
  new DataView(salt.buffer).setBigUint64(0, BigInt(counter));
  const password = concat(challenge.subarray(NONCE_OFFSET, NONCE_OFFSET + NONCE_LEN), id);
  return arrayBufferBacked(scrypt(password, salt, { N: POW_N, r: POW_R, p: POW_P, dkLen: KEY_LEN }));
}

export function leadingZeroBits(bytes: Uint8Array): number {
  let bits = 0;
  for (const byte of bytes) {
    if (byte !== 0) {
      return bits + Math.clz32(byte) - 24;
    }
    bits += 8;
  }
  return bits;
}

export function powToken(challenge: string, counter: number): string {
  assertCounter(counter);
  return `${challenge}.${counter.toString(16).padStart(COUNTER_HEX_CHARS, '0')}`;
}

export function solvePow(
  challenge: string,
  id: Uint8Array,
  difficulty: number,
  onAttempt?: (counter: number) => void,
): string {
  if (!Number.isInteger(difficulty) || difficulty < 0 || difficulty > MAX_POW_DIFFICULTY) {
    throw new PowError();
  }
  const raw = parsePowChallenge(challenge);
  for (let counter = 0; ; counter++) {
    onAttempt?.(counter);
    if (leadingZeroBits(powDigest(raw, id, counter)) >= difficulty) {
      return powToken(challenge, counter);
    }
  }
}

function assertCounter(counter: number): void {
  if (!Number.isSafeInteger(counter) || counter < 0) {
    throw new PowError('proof-of-work counter out of range');
  }
}
