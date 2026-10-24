import { describe, expect, it } from 'vitest';

import { linkSegments } from '../src/lib/link-segments.ts';

describe('linkSegments', () => {
  it('marks the host and the id as seen by the server and the fragment as browser-only', () => {
    const key = 'k'.repeat(43);
    const segments = linkSegments(`https://nylzo.example/m/AbCdEfGhIjKlMnOpQrStUv#1.1.${key}`);
    expect(segments.map(({ id, server }) => [id, server])).toEqual([
      ['host', true],
      ['id', true],
      ['meta', false],
      ['key', false],
    ]);
    expect(segments.map(({ text }) => text)).toEqual(['https://nylzo.example/m/', 'AbCdEfGhIjKlMnOpQrStUv', '#1.1.', `${'k'.repeat(14)}…${'k'.repeat(6)}`]);
    expect(segments.map(({ short }) => short)).toEqual(['nylzo.example/m/', 'AbCdEf…', '#1.1.', `${'k'.repeat(8)}…${'k'.repeat(5)}`]);
  });

  it('keeps a link it cannot split in one server-side segment', () => {
    for (const link of ['https://nylzo.example/other#x', 'https://nylzo.example/m/abc', 'https://nylzo.example/#/m/abc']) {
      expect(linkSegments(link)).toEqual([expect.objectContaining({ id: 'host', text: link, server: true })]);
    }
  });

  it('treats a fragment with a single dot as metadata only', () => {
    const segments = linkSegments('https://nylzo.example/m/abc#1.0');
    expect(segments.find(({ id }) => id === 'meta')?.text).toBe('#1.0');
    expect(segments.find(({ id }) => id === 'key')?.text).toBe('');
  });
});
