import { describe, expect, it } from 'vitest';

import { takeEnrollmentToken } from './enrollment-link.ts';

function address(hash: string) {
  const replaced: string[] = [];
  return {
    replaced,
    location: { hash, pathname: '/admin', search: '' },
    history: {
      replaceState: (_state: unknown, _unused: string, url?: string | URL | null) => {
        replaced.push(String(url));
      },
    },
  };
}

describe('takeEnrollmentToken', () => {
  it('reads the token of an enrollment link and removes it from the address', () => {
    const link = address(`#enroll=${'A'.repeat(31)}w`);
    expect(takeEnrollmentToken(link)).toBe(`${'A'.repeat(31)}w`);
    expect(link.replaced).toEqual(['/admin']);
  });

  it('drops any other fragment without taking a token', () => {
    const link = address('#enroll=short');
    expect(takeEnrollmentToken(link)).toBeUndefined();
    expect(link.replaced).toEqual(['/admin']);
  });
});
