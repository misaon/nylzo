import { describe, expect, it } from 'vitest';

import { bytesToHex, vectors } from '../../test/vectors.ts';
import { EncodingError, PowError, PowTimeoutError } from '../errors.ts';
import { ID_LENGTH } from '../limits.ts';
import { encodeBase64url } from '../primitives/base64url.ts';
import { parseId } from '../primitives/identifiers.ts';
import {
  leadingZeroBits,
  MAX_POW_DIFFICULTY,
  parsePowChallenge,
  POW_CHALLENGE_LENGTH,
  powDigest,
  powToken,
  solvePow,
} from './proof-of-work.ts';

const challenge = encodeBase64url(new Uint8Array(POW_CHALLENGE_LENGTH));
const id = new Uint8Array(ID_LENGTH);

describe('AK9: proof-of-work vectors', () => {
  const solvable = vectors.pow.filter((vector) => vector.difficulty !== undefined);

  it.each(vectors.pow)('$name', (vector) => {
    const digest = powDigest(parsePowChallenge(vector.challenge), parseId(vector.id), vector.counter);
    expect(bytesToHex(digest)).toBe(vector.digest_hex);
    expect(leadingZeroBits(digest)).toBe(vector.zero_bits);
    expect(powToken(vector.challenge, vector.counter)).toBe(vector.token);
  });

  it.each(solvable)('$name is the first counter the solver accepts', { timeout: 60_000 }, (vector) => {
    const attempts: number[] = [];
    const onAttempt = (counter: number): void => {
      attempts.push(counter);
    };
    expect(solvePow(vector.challenge, parseId(vector.id), vector.difficulty ?? 0, { onAttempt })).toBe(vector.token);
    expect(attempts.at(-1)).toBe(vector.counter);
  });
});

describe('proof of work (spec 8.1)', () => {
  it.each([-1, MAX_POW_DIFFICULTY + 1, 1.5, NaN])(
    'refuses the difficulty %d, which no server may ask for',
    (difficulty) => {
      expect(() => solvePow(challenge, id, difficulty)).toThrow(PowError);
    },
  );

  it.each([-1, 2 ** 53])('refuses the counter %d', (counter) => {
    expect(() => powToken(challenge, counter)).toThrow(PowError);
  });

  it('refuses a challenge that is not 36 bytes', () => {
    expect(() => solvePow(challenge.slice(1), id, 0)).toThrow(EncodingError);
  });

  it('stops at the deadline instead of solving past the challenge window', () => {
    let clock = 0;
    const attempts: number[] = [];
    const options = {
      deadline: 3,
      now: () => clock++,
      onAttempt: (counter: number) => {
        attempts.push(counter);
      },
    };
    expect(() => solvePow(challenge, id, MAX_POW_DIFFICULTY, options)).toThrow(PowTimeoutError);
    expect(attempts).toEqual([0, 1, 2]);
  });
});
