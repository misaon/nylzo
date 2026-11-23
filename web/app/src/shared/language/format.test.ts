import { describe, expect, it } from 'vitest';

import { formatMoment, formatSize } from './format.ts';

describe('Czech times', () => {
  it.each([
    [14, 41, 've\u{A0}14:41'],
    [9, 5, 'v\u{A0}9:05'],
  ])('put the preposition Czech grammar asks for before %i:%i', (hour, minute, ending) => {
    const moment = new Date(2026, 9, 23, hour, minute).getTime();
    expect(formatMoment(moment, 'cs').endsWith(ending)).toBe(true);
  });
});

describe('sizes', () => {
  it('count bytes below one kibibyte and switch to kilobytes above', () => {
    expect(formatSize(512, 'en')).toBe('512\u{A0}B');
    expect(formatSize(2048, 'en')).toBe('2 kB');
  });
});
