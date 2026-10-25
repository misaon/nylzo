import { scrypt } from '@noble/hashes/scrypt.js';

import { PowError, PowTimeoutError } from '../errors.ts';
import { ID_LENGTH, isUnsignedInteger, KEY_LENGTH } from '../limits.ts';
import { decodeBase64urlExact } from '../primitives/base64url.ts';
import { arrayBufferBacked, assertLength, concatenate } from '../primitives/bytes.ts';

const POW_N = 1 << 14;
const POW_R = 8;
const POW_P = 1;
export const POW_CHALLENGE_LENGTH = 36;
export const MAX_POW_DIFFICULTY = 8;

const NONCE_OFFSET = 4;
const NONCE_LENGTH = 16;
const COUNTER_HEX_DIGITS = 16;

export function parsePowChallenge(text: string): Uint8Array<ArrayBuffer> {
  return decodeBase64urlExact(text, POW_CHALLENGE_LENGTH);
}

export function powDigest(challenge: Uint8Array, id: Uint8Array, counter: number): Uint8Array<ArrayBuffer> {
  assertLength(challenge, POW_CHALLENGE_LENGTH, 'challenge');
  assertLength(id, ID_LENGTH, 'id');
  assertCounter(counter);
  const salt = new Uint8Array(8);
  new DataView(salt.buffer).setBigUint64(0, BigInt(counter));
  const password = concatenate(challenge.subarray(NONCE_OFFSET, NONCE_OFFSET + NONCE_LENGTH), id);
  return arrayBufferBacked(scrypt(password, salt, { N: POW_N, r: POW_R, p: POW_P, dkLen: KEY_LENGTH }));
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
  return `${challenge}.${counter.toString(16).padStart(COUNTER_HEX_DIGITS, '0')}`;
}

export interface SolveOptions {
  readonly onAttempt?: (counter: number) => void;
  readonly deadline?: number;
  readonly now?: () => number;
}

export function solvePow(challenge: string, id: Uint8Array, difficulty: number, options: SolveOptions = {}): string {
  if (!isUnsignedInteger(difficulty, MAX_POW_DIFFICULTY)) {
    throw new PowError();
  }
  const challengeBytes = parsePowChallenge(challenge);
  const now = options.now ?? Date.now;
  for (let counter = 0; ; counter++) {
    if (options.deadline !== undefined && now() >= options.deadline) {
      throw new PowTimeoutError();
    }
    options.onAttempt?.(counter);
    if (leadingZeroBits(powDigest(challengeBytes, id, counter)) >= difficulty) {
      return powToken(challenge, counter);
    }
  }
}

function assertCounter(counter: number): void {
  if (!isUnsignedInteger(counter, Number.MAX_SAFE_INTEGER)) {
    throw new PowError('proof-of-work counter out of range');
  }
}
