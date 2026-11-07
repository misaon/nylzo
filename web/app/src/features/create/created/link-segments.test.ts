import { describe, expect, it } from 'vitest';

import { linkSegments } from './link-segments.ts';

describe('linkSegments', () => {
  it('highlights only the key that the browser never sends', () => {
    const key = 'k'.repeat(43);
    const hash = 'h'.repeat(43);
    const segments = linkSegments(`https://nylzo.example/r#2.3.AbCdEfGhIjKlMnOpQrStUv.${key}.${hash}`);
    expect(segments.map(({ id, secret }) => [id, secret])).toEqual([
      ['host', false],
      ['meta', false],
      ['id', false],
      ['key', true],
    ]);
    expect(segments.map(({ text }) => text)).toEqual([
      'nylzo.example/r',
      '#2.3.',
      'AbCd…tUv',
      `.${'k'.repeat(13)}…${'h'.repeat(6)}`,
    ]);
    expect(segments.map(({ short }) => short)).toEqual([
      'nylzo.example/r',
      '#2.3.',
      'Ab…Uv',
      `.${'k'.repeat(4)}…${'h'.repeat(3)}`,
    ]);
  });

  it('keeps a link it cannot split in one plain segment', () => {
    for (const link of [
      'https://nylzo.example/other#x',
      'https://nylzo.example/r',
      'https://nylzo.example/r#2.3.a.b',
      'https://nylzo.example/m/abc#1.0.x',
    ]) {
      expect(linkSegments(link)).toEqual([expect.objectContaining({ id: 'host', text: link, secret: false })]);
    }
  });
});
