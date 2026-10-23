import { describe, expect, it } from 'vitest';

import { contactHref, verifyCommand } from '../src/lib/transparency.ts';

describe('transparency', () => {
  it('links only an email address or an https address', () => {
    expect(contactHref('abuse@example.org')).toBe('mailto:abuse@example.org');
    expect(contactHref('https://example.org/contact')).toBe('https://example.org/contact');
    for (const contact of [
      'javascript:alert(1)',
      'http://example.org',
      'mailto:abuse@example.org',
      'https://user:pass@example.org',
      'https://',
      'https://example.org/"onmouseover',
      'Abuse <abuse@example.org>',
      'abuse@example.org?subject=x',
      'data:text/html,x',
      '//example.org',
    ]) {
      expect(contactHref(contact), contact).toBeUndefined();
    }
  });

  it('builds the verify command for this origin', () => {
    expect(verifyCommand('https://nylzo.example')).toBe('nylzo verify-client --server https://nylzo.example');
  });
});
