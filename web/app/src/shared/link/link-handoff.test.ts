import { formatLink, KDF_NONE, newSecret } from '@nylzo/core';
import { describe, expect, it, vi } from 'vitest';

import { handOverLink, takeFragment } from './link-handoff.ts';

const ORIGIN = 'https://nylzo.test';

function newId(): Uint8Array {
  return crypto.getRandomValues(new Uint8Array(16));
}

describe('a pasted link (W2-02)', () => {
  it('hands the fragment over in memory once, only to the page of its message', () => {
    vi.stubGlobal('nylzoFragment', () => '');
    const link = formatLink(ORIGIN, newId(), KDF_NONE, newSecret());
    const path = new URL(link).pathname;
    expect(handOverLink(`  ${link}\n`, ORIGIN)).toEqual({ path });
    expect(takeFragment(path)).toEqual({ fragment: new URL(link).hash, pasted: true });
    handOverLink(link, ORIGIN);
    expect(takeFragment('/m/AAAAAAAAAAAAAAAAAAAAAA').pasted).toBe(false);
    expect(takeFragment(path).pasted).toBe(false);
  });

  it.each([
    ['damaged', `${ORIGIN}/m/AAAAAAAAAAAAAAAAAAAAAA#1.0.short`, 'invalid'],
    ['without a key', `${ORIGIN}/m/AAAAAAAAAAAAAAAAAAAAAA`, 'invalid'],
    ['from a newer version', `${ORIGIN}/m/AAAAAAAAAAAAAAAAAAAAAA#9.0.x`, 'newer'],
    ['for another instance', formatLink('https://other.test', newId(), KDF_NONE, newSecret()), 'foreign'],
  ])('refuses a link %s', (_, link, problem) => {
    expect(handOverLink(link, ORIGIN)).toEqual({ problem });
  });
});
