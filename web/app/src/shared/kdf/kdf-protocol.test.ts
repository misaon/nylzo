import { describe, expect, it } from 'vitest';

import { isKdfRequest, PROGRESS_HEARTBEAT_MS, throttledProgress } from './kdf-protocol.ts';

describe('progress from the key derivation worker', () => {
  it('posts at most one message per percent and always the end', () => {
    const posted: number[] = [];
    const report = throttledProgress(
      (fraction) => {
        posted.push(fraction);
      },
      () => 0,
    );
    for (let call = 0; call <= 10_083; call++) {
      report(call / 10_083);
    }
    expect(posted.length).toBeGreaterThan(50);
    expect(posted.length).toBeLessThanOrEqual(101);
    expect(posted[0]).toBe(0);
    expect(posted.at(-1)).toBe(1);
  });

  it('keeps posting once a second after the fraction stopped growing', () => {
    let now = 0;
    const posted: number[] = [];
    const report = throttledProgress(
      (fraction) => {
        posted.push(fraction);
      },
      () => now,
    );
    report(1);
    report(1);
    now += PROGRESS_HEARTBEAT_MS - 1;
    report(1);
    expect(posted).toEqual([1]);
    now += 1;
    report(1);
    expect(posted).toEqual([1, 1]);
  });
});

describe('requests the key derivation worker accepts', () => {
  const message = new Uint8Array(64);
  const id = new Uint8Array(16);

  it('accepts a well-formed stretch or proof-of-work request', () => {
    expect(isKdfRequest({ kind: 'stretch', message })).toBe(true);
    expect(isKdfRequest({ kind: 'pow', challenge: 'c', id, difficulty: 4, budgetMs: 1000 })).toBe(true);
  });

  it.each([
    null,
    'stretch',
    { kind: 'stretch', message: 'phrase' },
    { kind: 'stretch', message: new Uint8Array(32) },
    { kind: 'scrypt', passphrase: 'phrase', passphraseSalt: new Uint8Array(32) },
    { kind: 'pow', challenge: 'c', id: new Uint8Array(15), difficulty: 4, budgetMs: 1000 },
    { kind: 'pow', challenge: 'c', id, difficulty: 0.5, budgetMs: 1000 },
    { kind: 'pow', challenge: 'c', id, difficulty: 4, budgetMs: Number.POSITIVE_INFINITY },
    { kind: 'pow', challenge: 'c', id, difficulty: 4, budgetMs: 0 },
    { kind: 'eval', source: 'x' },
  ])('refuses %j', (request) => {
    expect(isKdfRequest(request)).toBe(false);
  });
});
