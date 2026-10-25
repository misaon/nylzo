import { describe, expect, it } from 'vitest';

import { ATTACKERS, countdown, judge, yearsAmount } from './brute-force.ts';
import { createI18n } from '../../../shared/i18n/i18n.svelte.ts';

function words(yearsLog10: number, language: 'cs' | 'en'): string {
  const { t } = createI18n(language);
  const { key, amount } = yearsAmount(yearsLog10, language);
  return t(key, { amount }).replaceAll(' ', ' ');
}

describe('brute force illustration', () => {
  it('puts every classical attacker past the age of the universe', () => {
    for (const { id } of ATTACKERS) {
      expect(judge(id, false)).toMatchObject({ verdict: 'beyond', bigBang: true });
    }
  });

  it('shortens the search with a quantum computer to the π/4·2^128 steps of Grover’s algorithm', () => {
    const world = judge('world', true);
    expect(world.bigBang).toBe(false);
    expect(world.verdict).toBe('earth');
    expect(10 ** world.yearsLog10).toBeCloseTo(8.46e9, -8);
    expect(words(world.yearsLog10, 'cs')).toBe('8,5 miliardy let');
    expect(words(world.yearsLog10, 'en')).toBe('8.5 billion years');
  });

  it('counts down from 2^256 and marks the digits that changed', () => {
    const start = countdown('laptop', false, 0);
    expect((start.head + start.tail).replaceAll(' ', '')).toBe((2n ** 256n).toString());
    expect(start.tail).toBe('');
    const later = countdown('laptop', false, 1000);
    expect(later.done.replaceAll(' ', '')).toBe('100000000');
    expect((later.head + later.tail).replaceAll(' ', '')).toBe((2n ** 256n - 100_000_000n).toString());
    expect(later.tail).toBe('029 639 936');
    expect(later.head.endsWith('913 ')).toBe(true);
  });

  it('words numbers of years with the plural forms of each language', () => {
    expect(words(0, 'cs')).toBe('1 rok');
    expect(words(Math.log10(3), 'cs')).toBe('3 roky');
    expect(words(Math.log10(12), 'cs')).toBe('12 let');
    expect(words(0, 'en')).toBe('1 year');
    expect(words(Math.log10(12), 'en')).toBe('12 years');
    expect(words(Math.log10(66e6), 'cs')).toBe('66 milionů let');
  });
});
