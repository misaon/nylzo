import { describe, expect, it } from 'vitest';

import type { Language } from '../../../../shared/language/language.ts';
import { Translator } from '../../../../shared/language/translator.svelte.ts';
import { ATTACKERS, countdown, judge, judgementOf, yearsText } from './brute-force.ts';

function words(yearsLog10: number, language: Language): string {
  return yearsText(yearsLog10, new Translator(language)).replaceAll('\u{A0}', ' ');
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
    expect(words(world.yearsLog10, 'ja')).toBe('85億年');
  });

  it.each([
    [-3, 'week', false],
    [0, 'ttl', false],
    [2, 'life', false],
    [8, 'dinosaurs', false],
    [9.8, 'earth', false],
    [11, 'universe', true],
    [14, 'beyond', true],
  ] as const)('names the milestone a search of 10^%d years passes', (yearsLog10, verdict, bigBang) => {
    expect(judgementOf(yearsLog10)).toEqual({ verdict, bigBang, yearsLog10 });
  });

  it('writes a span under a million years in whole years', () => {
    expect(words(Math.log10(250), 'en')).toBe('250 years');
  });

  it('counts the quantum search down from π/4·2^128 steps', () => {
    const start = countdown('world', true, 0);
    expect((start.head + start.tail).replaceAll(' ', '')).toBe(
      ((2n ** 128n * 785_398_163n) / 1_000_000_000n).toString(),
    );
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
});
