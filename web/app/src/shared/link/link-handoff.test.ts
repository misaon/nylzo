import { encodeBase64url, formatLink, KDF_NONE, newSecret } from '@nylzo/core';
import { describe, expect, it, vi } from 'vitest';

import { handOverLink, takeFragment } from './link-handoff.ts';

const ORIGIN = 'https://nylzo.test';

function newId(): Uint8Array {
  return crypto.getRandomValues(new Uint8Array(16));
}

function newLink(origin = ORIGIN): string {
  return formatLink(origin, newId(), KDF_NONE, newSecret(), new Uint8Array(32));
}

describe('a pasted link (W2-02)', () => {
  it('hands the fragment over in memory once, only to the page of its message', () => {
    vi.stubGlobal('nylzoFragment', () => '');
    const link = newLink();
    const path = new URL(link).pathname;
    expect(handOverLink(`  ${link}\n`, ORIGIN)).toEqual({ path: '/r' });
    expect(takeFragment(path)).toEqual({ fragment: new URL(link).hash, pasted: true });
    handOverLink(link, ORIGIN);
    expect(takeFragment('/m/AAAAAAAAAAAAAAAAAAAAAA').pasted).toBe(false);
    expect(takeFragment(path).pasted).toBe(false);
  });

  it.each([
    ['damaged', `${ORIGIN}/r#2.0.short`, 'invalid'],
    ['without a key', `${ORIGIN}/r`, 'invalid'],
    ['from a newer version', `${ORIGIN}/r#9.0.x`, 'newer'],
    ['for another instance', newLink('https://other.test'), 'foreign'],
    [
      'from before the upgrade to protocol version 2',
      `${ORIGIN}/m/${encodeBase64url(newId())}#1.0.${encodeBase64url(newSecret())}`,
      'released',
    ],
  ])('refuses a link %s', (_, link, problem) => {
    expect(handOverLink(link, ORIGIN)).toEqual({ problem });
  });
});
