import { describe, expect, it } from 'vitest';

import { linkSegments } from './link-segments.ts';

describe('linkSegments', () => {
  it('marks the host and the id as seen by the server and the rest of the fragment as browser-only', () => {
    const key = 'k'.repeat(43);
    const hash = 'h'.repeat(43);
    const segments = linkSegments(`https://nylzo.example/r#2.3.AbCdEfGhIjKlMnOpQrStUv.${key}.${hash}`);
    expect(segments.map(({ id, server }) => [id, server])).toEqual([
      ['host', true],
      ['meta', false],
      ['id', true],
      ['key', false],
    ]);
    expect(segments.map(({ text }) => text)).toEqual([
      'https://nylzo.example/r',
      '#2.3.',
      'AbCdEfGhIjKlMnOpQrStUv',
      `.${'k'.repeat(13)}…${'h'.repeat(6)}`,
    ]);
    expect(segments.map(({ short }) => short)).toEqual([
      'nylzo.example/r',
      '#2.3.',
      'AbCdEf…',
      `.${'k'.repeat(7)}…${'h'.repeat(5)}`,
    ]);
  });

  it('keeps a link it cannot split in one server-side segment', () => {
    for (const link of [
      'https://nylzo.example/other#x',
      'https://nylzo.example/r',
      'https://nylzo.example/r#2.3.a.b',
      'https://nylzo.example/m/abc#1.0.x',
    ]) {
      expect(linkSegments(link)).toEqual([expect.objectContaining({ id: 'host', text: link, server: true })]);
    }
  });
});
