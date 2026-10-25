import { describe, expect, it } from 'vitest';

import { bytesToHex, loadVectors } from '../../test/vectors.ts';
import {
  encodeBase64url,
  leadingZeroBits,
  MAX_POW_DIFFICULTY,
  parseId,
  parsePowChallenge,
  POW_CHALLENGE_LEN,
  powDigest,
  PowError,
  PowTimeoutError,
  powToken,
  solvePow,
} from '../index.ts';

const vectors = loadVectors().pow;

describe('proof of work', () => {
  it.each(vectors)('matches the vector $name', (vector) => {
    const digest = powDigest(parsePowChallenge(vector.challenge), parseId(vector.id), vector.counter);
    expect(bytesToHex(digest)).toBe(vector.digest_hex);
    expect(leadingZeroBits(digest)).toBe(vector.zero_bits);
    expect(powToken(vector.challenge, vector.counter)).toBe(vector.token);
  });

  it.each(vectors.filter((vector) => vector.difficulty !== undefined))(
    'solves the vector $name',
    { timeout: 60_000 },
    (vector) => {
      const attempts: number[] = [];
      const onAttempt = (counter: number): void => {
        attempts.push(counter);
      };
      expect(solvePow(vector.challenge, parseId(vector.id), vector.difficulty ?? 0, { onAttempt })).toBe(vector.token);
      expect(attempts.at(-1)).toBe(vector.counter);
    },
  );

  it('counts leading zero bits', () => {
    expect(leadingZeroBits(new Uint8Array([]))).toBe(0);
    expect(leadingZeroBits(new Uint8Array([0x80]))).toBe(0);
    expect(leadingZeroBits(new Uint8Array([0x01]))).toBe(7);
    expect(leadingZeroBits(new Uint8Array([0x00, 0x40]))).toBe(9);
    expect(leadingZeroBits(new Uint8Array([0x00, 0x00]))).toBe(16);
  });

  it('refuses a difficulty a server must not ask for', () => {
    const challenge = encodeBase64url(new Uint8Array(POW_CHALLENGE_LEN));
    const id = new Uint8Array(16);
    for (const difficulty of [-1, MAX_POW_DIFFICULTY + 1, 1.5, NaN]) {
      expect(() => solvePow(challenge, id, difficulty)).toThrow(PowError);
    }
    expect(() => solvePow(challenge.slice(1), id, 0)).toThrow();
    expect(() => powToken(challenge, -1)).toThrow(PowError);
    expect(() => powToken(challenge, 2 ** 53)).toThrow(PowError);
  });

  it('stops at the deadline instead of solving on past the challenge window', () => {
    const challenge = encodeBase64url(new Uint8Array(POW_CHALLENGE_LEN));
    let clock = 0;
    const attempts: number[] = [];
    expect(() =>
      solvePow(challenge, new Uint8Array(16), MAX_POW_DIFFICULTY, {
        deadline: 3,
        now: () => clock++,
        onAttempt: (counter) => {
          attempts.push(counter);
        },
      }),
    ).toThrow(PowTimeoutError);
    expect(attempts).toEqual([0, 1, 2]);
  });
});
