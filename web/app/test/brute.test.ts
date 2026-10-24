import { describe, expect, it } from 'vitest';

import { countdown, judge, yearsInWords } from '../src/lib/brute.ts';

describe('brute force illustration', () => {
  it('puts every classical attacker past the age of the universe', () => {
    for (const attacker of ['laptop', 'gpu', 'farm', 'datacenter', 'world'] as const) {
      expect(judge(attacker, false)).toMatchObject({ verdict: 'beyond', bigBang: true });
    }
  });

  it('shortens the search with a quantum computer to Grover’s average', () => {
    const world = judge('world', true);
    expect(world.bigBang).toBe(false);
    expect(world.verdict).toBe('dinosaurs');
    expect(yearsInWords(world.yearsLog10, 'cs')).toMatch(/^\d+,\d miliardy let$/u);
    expect(yearsInWords(world.yearsLog10, 'en')).toMatch(/^\d+\.\d billion years$/u);
  });

  it('counts down from 2^256 and marks the digits that changed', () => {
    const start = countdown('laptop', false, 0);
    expect((start.head + start.tail).replaceAll(' ', '')).toBe((2n ** 256n).toString());
    expect(start.tail).toBe('');
    const later = countdown('laptop', false, 1000);
    expect(later.done.replaceAll(' ', '')).toBe('100000000');
    expect((later.head + later.tail).replaceAll(' ', '')).toBe((2n ** 256n - 100_000_000n).toString());
    expect(later.tail.replaceAll(' ', '').length).toBeGreaterThan(0);
  });

  it('words small numbers of years with Czech plural forms', () => {
    expect(yearsInWords(0, 'cs')).toBe('1 rok');
    expect(yearsInWords(Math.log10(3), 'cs')).toBe('3 roky');
    expect(yearsInWords(Math.log10(12), 'cs')).toBe('12 let');
    expect(yearsInWords(Math.log10(12), 'en')).toBe('12 years');
  });
});
